// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package api

import (
	"log/slog"
	"net/http"

	"github.com/chengyifei1991-ai/cadenza/internal/mcp"
	"github.com/chengyifei1991-ai/cadenza/internal/opampserver"
	"github.com/open-telemetry/opamp-go/server"
)

// RouterOptions 是 Router 装配的全部依赖。
type RouterOptions struct {
	// Auth 是 Web 管理面鉴权管理器（必填；off 模式下中间件为 no-op）。
	Auth *AuthManager
	// WebHandler 是 Web 静态资源处理器（含 SPA fallback）；nil 表示纯后端部署。
	WebHandler http.Handler
	// CORSOrigins 是允许的跨域 Origin 列表（默认空 = 同源部署）。
	CORSOrigins []string
	// DemoMode 标记演示模式（随 /api/v1/system/info 下发，供前端横幅展示）。
	DemoMode bool
	// MCPAuthToken 是 MCP 端点（/mcp）的 Bearer token；为空表示不启用鉴权。
	MCPAuthToken string
	// Logger 是请求日志（可 nil）。
	Logger *slog.Logger
}

// Router 装配全部 HTTP 路由。
type Router struct {
	opampHandler http.HandlerFunc
	connCtx      server.ConnContext
	mcpHandler   http.Handler
	handlers     *Handlers
	opts         RouterOptions
}

// NewRouter 创建路由：opamp 提供 OpAMP handler 与 ConnContext。
func NewRouter(opamp *opampserver.Server, mcpSrv *mcp.Server, handlers *Handlers, opts RouterOptions) (*Router, error) {
	handler, connCtx := opamp.Handler()
	return &Router{
		opampHandler: handler,
		connCtx:      connCtx,
		mcpHandler:   mcpSrv.Handler(),
		handlers:     handlers,
		opts:         opts,
	}, nil
}

// ConnContext 返回 OpAMP 连接上下文钩子，由 http.Server 装配。
func (r *Router) ConnContext() server.ConnContext { return r.connCtx }

// Handler 返回装配好的根 http.Handler。
func (r *Router) Handler() http.Handler {
	mux := http.NewServeMux()
	// 协议与公开端点（不走 Web 鉴权）。
	mux.HandleFunc("/v1/opamp", r.opampHandler)
	mux.Handle("/mcp", MCPAuth(r.opts.MCPAuthToken, r.mcpHandler))
	mux.HandleFunc("/healthz", r.handlers.Healthz)

	// Web 管理面 REST：整体挂鉴权中间件（登录等免登路径在中间件内放行）。
	mux.Handle("/api/", r.opts.Auth.Middleware(r.apiMux()))

	// Web 静态资源（含 SPA fallback）；纯后端部署时 opts.WebHandler 为 nil。
	if r.opts.WebHandler != nil {
		mux.Handle("/", r.opts.WebHandler)
	}

	var root http.Handler = logMiddleware(r.opts.Logger, mux)
	if len(r.opts.CORSOrigins) > 0 {
		root = corsMiddleware(r.opts.CORSOrigins, root)
	}
	return root
}

// apiMux 构建 /api 前缀下的全部 REST 路由。
func (r *Router) apiMux() *http.ServeMux {
	h := r.handlers
	mux := http.NewServeMux()

	// 认证与系统信息（登录前可访问，鉴权中间件内放行）。
	mux.HandleFunc("/api/v1/auth/login", r.opts.Auth.HandleLogin)
	mux.HandleFunc("/api/v1/auth/logout", r.opts.Auth.HandleLogout)
	mux.HandleFunc("/api/v1/auth/me", r.opts.Auth.HandleMe)
	mux.HandleFunc("/api/v1/system/info", SystemInfo(r.opts.Auth, r.opts.DemoMode, r.opts.MCPAuthToken != ""))

	// 会话（GET=列表 / POST=创建）与会话详情。
	mux.HandleFunc("/api/v1/sessions", h.HandleSessions)
	mux.HandleFunc("/api/v1/sessions/", func(w http.ResponseWriter, req *http.Request) {
		parts := splitPath(req.URL.Path)
		// /api/v1/sessions/{id}/tasks：会话发起的任务（任务↔会话绑定）。
		if len(parts) == 5 && parts[4] == "tasks" {
			h.ListSessionTasks(w, req, parts[3])
			return
		}
		if len(parts) != 4 {
			writeError(w, http.StatusNotFound, "未知路径")
			return
		}
		h.GetSessionByID(w, req, parts[3])
	})

	// 对话。
	mux.HandleFunc("/api/v1/chat", h.Chat)

	// 任务：apply/rollback 精确优先，其余走 {id}[/approve|reject] 分发。
	mux.HandleFunc("/api/v1/tasks/apply", h.ApplyTask)
	mux.HandleFunc("/api/v1/tasks/rollback", h.RollbackTask)
	mux.HandleFunc("/api/v1/tasks", h.ListTasks)
	mux.HandleFunc("/api/v1/tasks/", r.taskAction)

	// Collector 与审计。
	mux.HandleFunc("/api/v1/collectors", h.ListCollectors)
	mux.HandleFunc("/api/v1/collectors/", r.collectorAction)
	mux.HandleFunc("/api/v1/audit", h.ListAudit)
	mux.HandleFunc("/api/v1/stats", h.Stats)

	// 未注册的 /api/* 返回 JSON 404（而非 SPA fallback）。
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		writeError(w, http.StatusNotFound, "接口不存在")
	})
	return mux
}

// taskAction 分发 /api/v1/tasks/{id}[/approve|reject]。
func (r *Router) taskAction(w http.ResponseWriter, req *http.Request) {
	parts := splitPath(req.URL.Path)
	if len(parts) < 4 {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	id := parts[3]
	action := ""
	if len(parts) >= 5 {
		action = parts[4]
	}
	switch action {
	case "":
		r.handlers.GetTask(w, req, id)
	case "approve":
		r.handlers.ApproveTask(w, req, id)
	case "reject":
		r.handlers.RejectTask(w, req, id)
	default:
		writeError(w, http.StatusNotFound, "未知操作")
	}
}

// collectorAction 分发 /api/v1/collectors/{uid}[/versions]。
func (r *Router) collectorAction(w http.ResponseWriter, req *http.Request) {
	parts := splitPath(req.URL.Path)
	if len(parts) == 5 && parts[4] == "versions" {
		r.handlers.ListVersions(w, req, parts[3])
		return
	}
	if len(parts) == 4 {
		r.handlers.GetCollectorJSON(w, req, parts[3])
		return
	}
	writeError(w, http.StatusNotFound, "未知路径")
}

// logMiddleware 记录请求日志。
func logMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if logger != nil {
			logger.Info("http", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr)
		}
		next.ServeHTTP(w, r)
	})
}

// corsMiddleware 仅对白名单内的 Origin 回写 CORS 头（同源部署默认无需启用）。
// 显式 "*" 表示任意来源（此时不回写凭证头）。
func corsMiddleware(allowed []string, next http.Handler) http.Handler {
	wildcard := false
	set := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		if o == "*" {
			wildcard = true
			continue
		}
		set[o] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		_, match := set[origin]
		if origin != "" && (wildcard || match) {
			h := w.Header()
			if wildcard {
				h.Set("Access-Control-Allow-Origin", "*")
			} else {
				h.Set("Access-Control-Allow-Origin", origin)
				h.Add("Vary", "Origin")
				h.Set("Access-Control-Allow-Credentials", "true")
			}
			h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// splitPath 将路径按 '/' 拆分为非空段。
func splitPath(p string) []string {
	var parts []string
	start := 0
	for i := 0; i <= len(p); i++ {
		if i == len(p) || p[i] == '/' {
			if i > start {
				parts = append(parts, p[start:i])
			}
			start = i + 1
		}
	}
	return parts
}
