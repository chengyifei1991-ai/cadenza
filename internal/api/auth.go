// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/config"
	"golang.org/x/crypto/bcrypt"
)

// sessionCookie 是登录会话 Cookie 名。
const sessionCookie = "cadenza_session"

// sessionTTL 是登录会话有效期。
const sessionTTL = 24 * time.Hour

// ctxKey 是 request context 中用户名的键类型（避免与库冲突）。
type ctxKey struct{}

// authSession 是一条已登录会话。
type authSession struct {
	username  string
	expiresAt time.Time
}

// AuthManager 管理 Web 管理面登录（单管理员）。
//
// simple 模式：口令 bcrypt 校验 + 随机 token 会话（内存存储，重启即失效，
// V1 单实例可接受；多实例联邦时改为 DB 存储）。
// off 模式：全部放行（auth 中间件为 no-op）。
type AuthManager struct {
	mode     config.WebAuthMode
	username string
	hash     []byte

	mu       sync.RWMutex
	sessions map[string]authSession
	logger   *slog.Logger
}

// NewAuthManager 按配置创建鉴权管理器。simple 模式必须携带已校验的口令哈希
// （config.Load 已保证），此处仅为防御性解析。
func NewAuthManager(cfg *config.Config, logger *slog.Logger) (*AuthManager, error) {
	m := &AuthManager{
		mode:     cfg.Web.AuthMode,
		username: cfg.Web.AdminUser,
		logger:   logger,
		sessions: make(map[string]authSession),
	}
	if cfg.Web.AuthMode == config.WebAuthModeSimple && cfg.Web.AdminPasswordHash != "" {
		// Cost 对非 bcrypt 格式返回错误，此处仅作格式校验。
		if _, err := bcrypt.Cost([]byte(cfg.Web.AdminPasswordHash)); err != nil {
			return nil, errors.New("api: WEB_ADMIN_PASSWORD_HASH is not a valid bcrypt hash")
		}
		m.hash = []byte(cfg.Web.AdminPasswordHash)
	}
	return m, nil
}

// Enabled 返回当前是否为登录保护模式。
func (m *AuthManager) Enabled() bool { return m.mode == config.WebAuthModeSimple }

// exemptPath 判断路径是否免登录（登录/登出/会话自检/系统信息）。
func exemptPath(p string) bool {
	switch p {
	case "/api/v1/auth/login", "/api/v1/auth/logout", "/api/v1/auth/me", "/api/v1/system/info":
		return true
	}
	return false
}

// Middleware 返回鉴权中间件：simple 模式校验 Cookie 会话并注入用户名到 context；
// off 模式直接放行。免登录路径（login/logout/me/system/info）在未登录时放行，
// 但携带有效会话时仍注入用户名（供 me 与日志使用）。
func (m *AuthManager) Middleware(next http.Handler) http.Handler {
	if !m.Enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exempt := exemptPath(r.URL.Path)
		if token, err := r.Cookie(sessionCookie); err == nil && token.Value != "" {
			if sess, ok := m.lookup(token.Value); ok {
				ctx := context.WithValue(r.Context(), ctxKey{}, sess.username)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}
		if exempt {
			next.ServeHTTP(w, r)
			return
		}
		writeAuthRequired(w)
	})
}

// lookup 返回有效会话；过期会话惰性清理。
func (m *AuthManager) lookup(token string) (authSession, bool) {
	m.mu.RLock()
	sess, ok := m.sessions[token]
	m.mu.RUnlock()
	if !ok {
		return authSession{}, false
	}
	if time.Now().After(sess.expiresAt) {
		m.mu.Lock()
		delete(m.sessions, token)
		m.mu.Unlock()
		return authSession{}, false
	}
	return sess, true
}

// usernameFrom 从请求 context 读取当前登录用户（未登录/off 模式返回空）。
func usernameFrom(r *http.Request) string {
	if v, ok := r.Context().Value(ctxKey{}).(string); ok {
		return v
	}
	return ""
}

// loginRequest 是登录请求体。
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// HandleLogin 处理 POST /api/v1/auth/login。
func (m *AuthManager) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	if !m.Enabled() {
		writeError(w, http.StatusConflict, "免登模式（WEB_AUTH_MODE=off）无需登录")
		return
	}
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Username), []byte(m.username)) != 1 ||
		bcrypt.CompareHashAndPassword(m.hash, []byte(req.Password)) != nil {
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	token, err := newToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "登录失败，请重试")
		return
	}
	m.mu.Lock()
	m.sessions[token] = authSession{username: req.Username, expiresAt: time.Now().Add(sessionTTL)}
	m.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]string{"username": req.Username})
}

// HandleLogout 处理 POST /api/v1/auth/logout：删除会话并清 Cookie。
func (m *AuthManager) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	if token, err := r.Cookie(sessionCookie); err == nil && token.Value != "" {
		m.mu.Lock()
		delete(m.sessions, token.Value)
		m.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		MaxAge: -1, Expires: time.Unix(1, 0),
	})
	writeJSON(w, http.StatusOK, map[string]string{"message": "已退出登录"})
}

// HandleMe 处理 GET /api/v1/auth/me：返回当前登录态（供前端启动时校验会话）。
func (m *AuthManager) HandleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	if !m.Enabled() {
		writeJSON(w, http.StatusOK, map[string]string{
			"username": "user", "auth_mode": string(config.WebAuthModeOff),
		})
		return
	}
	if u := usernameFrom(r); u != "" {
		writeJSON(w, http.StatusOK, map[string]string{"username": u, "auth_mode": string(config.WebAuthModeSimple)})
		return
	}
	writeAuthRequired(w)
}

// writeAuthRequired 输出统一的未认证响应。
func writeAuthRequired(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录或会话已过期"})
}

// newToken 生成 32 字节随机会话 token（hex 编码）。
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
