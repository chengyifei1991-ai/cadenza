// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/config"
	"github.com/chengyifei1991-ai/cadenza/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// newTestAuth 构造 simple 模式鉴权管理器（口令固定为 "secret"）。
func newTestAuth(t *testing.T) *AuthManager {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt 生成失败: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(nopWriter{}, nil))
	am, err := NewAuthManager(&config.Config{Web: config.WebConfig{
		AuthMode: config.WebAuthModeSimple, AdminUser: "admin", AdminPasswordHash: string(hash),
	}}, logger)
	if err != nil {
		t.Fatalf("NewAuthManager 失败: %v", err)
	}
	return am
}

// TestAuthLoginGate 验证登录保护的完整流程：未登录拦截 → 错误口令拒绝 → 登录放行 → 登出失效。
func TestAuthLoginGate(t *testing.T) {
	am := newTestAuth(t)
	echoUser := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"user": usernameFrom(r)})
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", am.HandleLogin)
	mux.HandleFunc("/api/v1/auth/logout", am.HandleLogout)
	mux.HandleFunc("/api/v1/collectors", echoUser)
	root := am.Middleware(mux)

	// 1. 未登录访问受保护端点 → 401。
	rec := doJSON(t, root, http.MethodGet, "/api/v1/collectors", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录 status = %d, want 401", rec.Code)
	}
	// 2. 错误口令登录 → 401。
	rec = doJSON(t, root, http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": "admin", "password": "wrong"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("错误口令 status = %d, want 401, body=%s", rec.Code, rec.Body.String())
	}
	// 3. 正确登录 → 200 + 会话 Cookie。
	rec = doJSON(t, root, http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": "admin", "password": "secret"})
	if rec.Code != http.StatusOK {
		t.Fatalf("登录 status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value == "" {
		t.Fatal("未返回会话 Cookie")
	}
	// 4. 携带 Cookie 访问受保护端点 → 200 且注入用户名。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/collectors", nil)
	req.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	root.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK || !strings.Contains(rec2.Body.String(), `"user":"admin"`) {
		t.Fatalf("登录后访问 status = %d, body = %s", rec2.Code, rec2.Body.String())
	}
	// 5. 携带 Cookie 登出后令牌失效。
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutReq.AddCookie(cookie)
	rec4 := httptest.NewRecorder()
	root.ServeHTTP(rec4, logoutReq)
	if rec4.Code != http.StatusOK {
		t.Fatalf("登出 status = %d", rec4.Code)
	}
	loggedOut := httptest.NewRequest(http.MethodGet, "/api/v1/collectors", nil)
	loggedOut.AddCookie(cookie)
	rec3 := httptest.NewRecorder()
	root.ServeHTTP(rec3, loggedOut)
	if rec3.Code != http.StatusUnauthorized {
		t.Fatalf("登出后访问 status = %d, want 401", rec3.Code)
	}
}

// TestAuthMe 验证免登录会话自检端点语义。
func TestAuthMe(t *testing.T) {
	am := newTestAuth(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", am.HandleLogin)
	mux.HandleFunc("/api/v1/auth/me", am.HandleMe)
	root := am.Middleware(mux)

	// 直接访问（无会话）→ 401。
	rec := doJSON(t, root, http.MethodGet, "/api/v1/auth/me", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me(未登录) status = %d, want 401", rec.Code)
	}
	// 登录后访问 me → 200。
	login := doJSON(t, root, http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": "admin", "password": "secret"})
	var cookie *http.Cookie
	for _, c := range login.Result().Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("登录未返回会话 Cookie")
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	root.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK || !strings.Contains(rec2.Body.String(), `"username":"admin"`) {
		t.Fatalf("me(已登录) status = %d, body = %s", rec2.Code, rec2.Body.String())
	}
}

// sampleYAML 是测试用最小合法 Collector 配置（通过第一级结构校验）。
const sampleYAML = `receivers:
  otlp:
    protocols:
      grpc:
exporters:
  debug:
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [debug]
`

// TestApplyTask 表驱动验证配置编辑器"保存并下发"接口。
func TestApplyTask(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()
	if err := h.store.UpsertCollector(ctx, &store.Collector{
		InstanceUID: "col-1", Hostname: "node-1", LastSeenAt: time.Now().UTC(),
		Status: store.CollectorStatusHealthy, EffectiveConfig: sampleYAML,
	}); err != nil {
		t.Fatalf("UpsertCollector 失败: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tasks/apply", h.ApplyTask)

	valid := func() map[string]any {
		return map[string]any{"collector_instance_uid": "col-1", "yaml": sampleYAML, "note": "测试变更"}
	}
	cases := []struct {
		name   string
		body   map[string]any
		want   int
		substr string
	}{
		{name: "合法提交 → 201 待审批", body: valid(), want: http.StatusCreated, substr: `"type":"apply"`},
		{name: "缺 yaml → 400", body: map[string]any{"collector_instance_uid": "col-1"}, want: http.StatusBadRequest, substr: "yaml"},
		{name: "Collector 不存在 → 404", body: map[string]any{"collector_instance_uid": "nope", "yaml": sampleYAML},
			want: http.StatusNotFound, substr: "不存在"},
		{name: "非法 YAML → 400", body: map[string]any{"collector_instance_uid": "col-1", "yaml": "foo: bar"},
			want: http.StatusBadRequest, substr: "校验未通过"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, mux, http.MethodPost, "/api/v1/tasks/apply", tc.body)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.substr != "" && !strings.Contains(rec.Body.String(), tc.substr) {
				t.Errorf("body 缺少 %q: %s", tc.substr, rec.Body.String())
			}
		})
	}
	// 合法提交应产生一条 awaiting_approval 任务。
	tasks, _, err := h.store.ListTasks(ctx, store.TaskStatusAwaitingApproval, 0, 0)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("待审批任务数 = %d, err = %v, want 1", len(tasks), err)
	}
	if tasks[0].Type != store.TaskTypeApply || tasks[0].GeneratedYAML != sampleYAML {
		t.Errorf("apply 任务字段不符: %+v", tasks[0])
	}
}

// TestStatsAndSessions 验证统计端点与会话列表/详情端点。
func TestStatsAndSessions(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := h.store.UpsertCollector(ctx, &store.Collector{
		InstanceUID: "s-1", Hostname: "n1", LastSeenAt: now, Status: store.CollectorStatusHealthy,
	}); err != nil {
		t.Fatalf("UpsertCollector 失败: %v", err)
	}
	if err := h.store.UpsertCollector(ctx, &store.Collector{
		InstanceUID: "s-2", Hostname: "n2", LastSeenAt: now.Add(-2 * time.Hour), Status: store.CollectorStatusOffline,
	}); err != nil {
		t.Fatalf("UpsertCollector 失败: %v", err)
	}
	tk := &store.Task{ID: "tk-1", Type: store.TaskTypeApply, Status: store.TaskStatusAwaitingApproval,
		RequireApproval: true, Input: "x", CreatedAt: now, UpdatedAt: now}
	if err := h.tasks.Create(ctx, tk); err != nil {
		t.Fatalf("创建任务失败: %v", err)
	}
	sess := &store.ChatSession{ID: "chat-1", CreatedAt: now.Add(-time.Minute)}
	if err := h.store.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession 失败: %v", err)
	}
	if err := h.store.AppendMessage(ctx, sess.ID, store.ChatMessage{
		Role: "user", Content: "加 tail sampling", CreatedAt: now,
	}); err != nil {
		t.Fatalf("AppendMessage 失败: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/stats", h.Stats)
	mux.HandleFunc("/api/v1/sessions", h.HandleSessions)

	// stats：Collector 2（1 健康 1 离线）、任务 1 待审批、会话 1。
	rec := doJSON(t, mux, http.MethodGet, "/api/v1/stats", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("stats status = %d", rec.Code)
	}
	var st statsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("解析 stats 失败: %v, body=%s", err, rec.Body.String())
	}
	if st.Collectors.Total != 2 || st.Collectors.Healthy != 1 || st.Collectors.Offline != 1 {
		t.Errorf("collectors 统计不符: %+v", st.Collectors)
	}
	if st.Tasks.AwaitingApproval != 1 || st.Tasks.Total != 1 {
		t.Errorf("tasks 统计不符: %+v", st.Tasks)
	}
	if st.SessionsTotal != 1 {
		t.Errorf("sessions_total = %d, want 1", st.SessionsTotal)
	}

	// sessions 列表：1 条摘要，含首条消息与条数。
	rec = doJSON(t, mux, http.MethodGet, "/api/v1/sessions?page=1&page_size=10", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("sessions status = %d", rec.Code)
	}
	var page struct {
		Items    []store.SessionSummary `json:"items"`
		Total    int64                  `json:"total"`
		PageSize int                    `json:"page_size"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("解析 sessions 失败: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].MessageCount != 1 ||
		page.Items[0].FirstMessage != "加 tail sampling" {
		t.Errorf("sessions 摘要不符: %+v", page)
	}
}

// TestSystemInfo 验证公开系统信息端点内容。
func TestSystemInfo(t *testing.T) {
	am := newTestAuth(t)
	h := SystemInfo(am, true)
	rec := doJSON(t, h, http.MethodGet, "/api/v1/system/info", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("system/info status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`"auth_mode":"simple"`, `"demo_mode":true`, `"version":"1.0.0-rc.1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("system/info 缺少 %s: %s", want, body)
		}
	}
}

// TestGetCollectorDetail 验证 GET /api/v1/collectors/{uid} 详情端点。
func TestGetCollectorDetail(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()
	if err := h.store.UpsertCollector(ctx, &store.Collector{
		InstanceUID: "col-det-1", Hostname: "det-node", Version: "0.156.0",
		LastSeenAt: time.Now().UTC(), Status: store.CollectorStatusHealthy,
		EffectiveConfig: sampleYAML, GroupID: "grp-prod",
	}); err != nil {
		t.Fatalf("UpsertCollector 失败: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/collectors/", func(w http.ResponseWriter, r *http.Request) {
		parts := splitPath(r.URL.Path)
		if len(parts) == 4 {
			h.GetCollectorJSON(w, r, parts[3])
			return
		}
		writeError(w, http.StatusNotFound, "未知路径")
	})

	rec := doJSON(t, mux, http.MethodGet, "/api/v1/collectors/col-det-1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("详情 status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var c store.Collector
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatalf("解析详情失败: %v", err)
	}
	if c.InstanceUID != "col-det-1" || c.Hostname != "det-node" || c.EffectiveConfig != sampleYAML {
		t.Errorf("详情字段不符: %+v", c)
	}
	// 不存在 → 404。
	rec = doJSON(t, mux, http.MethodGet, "/api/v1/collectors/nope", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("不存在 uid status = %d, want 404", rec.Code)
	}
	// 方法不符 → 405。
	rec = doJSON(t, mux, http.MethodPost, "/api/v1/collectors/col-det-1", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", rec.Code)
	}
}

// TestLoginEmptyCredentials 验证空凭据登录返回 400（参数错误而非认证失败）。
func TestLoginEmptyCredentials(t *testing.T) {
	am := newTestAuth(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", am.HandleLogin)
	root := am.Middleware(mux)

	for _, tc := range []struct {
		name string
		body map[string]string
	}{
		{name: "全空", body: map[string]string{}},
		{name: "缺密码", body: map[string]string{"username": "admin"}},
		{name: "缺用户名", body: map[string]string{"password": "secret"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, root, http.MethodPost, "/api/v1/auth/login", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "不能为空") {
				t.Errorf("应提示字段缺失: %s", rec.Body.String())
			}
		})
	}
}

// TestApproveRejectNotFound 验证审批/拒绝不存在的任务返回 404 中文（不泄漏英文内部错误）。
func TestApproveRejectNotFound(t *testing.T) {
	h := newTestHandlers(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tasks/", func(w http.ResponseWriter, r *http.Request) {
		parts := splitPath(r.URL.Path)
		if len(parts) == 5 {
			switch parts[4] {
			case "approve":
				h.ApproveTask(w, r, parts[3])
			case "reject":
				h.RejectTask(w, r, parts[3])
			}
			return
		}
		writeError(w, http.StatusNotFound, "未知路径")
	})
	for _, action := range []string{"approve", "reject"} {
		t.Run(action, func(t *testing.T) {
			body := map[string]any{"reason": "x"}
			rec := doJSON(t, mux, http.MethodPost, "/api/v1/tasks/does-not-exist/"+action, body)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s 不存在任务 status = %d, want 404", action, rec.Code)
			}
			if strings.Contains(rec.Body.String(), "record not found") {
				t.Errorf("%s 不应泄漏英文内部错误: %s", action, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "任务不存在") {
				t.Errorf("%s 应返回中文 404: %s", action, rec.Body.String())
			}
		})
	}
}
