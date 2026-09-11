// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/store"
)

// TestSessionTasksBinding 验证任务↔会话硬绑定：
//   - REST 提交带 session_id 的任务会落库绑定；
//   - GET /api/v1/sessions/{id}/tasks 只返回该会话发起的任务；
//   - 会话不存在返回 404；会话列表接口不受影响。
func TestSessionTasksBinding(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()

	// 目标 Collector 与会话。
	if err := h.store.UpsertCollector(ctx, &store.Collector{
		InstanceUID: "sess-col-1", Hostname: "n1", Version: "0.156.0",
		LastSeenAt: time.Now().UTC(), Status: store.CollectorStatusHealthy,
		EffectiveConfig: "receivers:\n", GroupID: "g1",
	}); err != nil {
		t.Fatalf("UpsertCollector: %v", err)
	}
	sess := &store.ChatSession{ID: "sess-1", CreatedAt: time.Now().UTC()}
	if err := h.store.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// 另一会话的任务（不应出现在 sess-1 结果里）。
	if err := h.store.CreateTask(ctx, &store.Task{
		ID: "task-other", Type: store.TaskTypeGenerate, Status: store.TaskStatusPending,
		SessionID: "sess-other", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateTask(other): %v", err)
	}

	// 1. REST 提交带 session_id 的 apply 任务。
	yamlCfg := sampleYAML
	rec := doJSON(t, http.HandlerFunc(h.ApplyTask), http.MethodPost, "/api/v1/tasks/apply",
		map[string]string{"collector_instance_uid": "sess-col-1", "yaml": yamlCfg, "session_id": "sess-1"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("apply status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created store.Task
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析任务失败: %v", err)
	}
	if created.SessionID != "sess-1" {
		t.Errorf("apply 任务 SessionID = %q, want sess-1", created.SessionID)
	}

	// 2. 会话任务列表（裸数组）。
	rec = doJSON(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ListSessionTasks(w, r, "sess-1")
	}), http.MethodGet, "/api/v1/sessions/sess-1/tasks", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("session tasks status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var items []store.Task
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("解析列表失败: %v", err)
	}
	if len(items) != 1 || items[0].ID != created.ID {
		t.Fatalf("会话任务列表不符: %+v", items)
	}

	// 3. 分页信封模式。
	rec = doJSON(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ListSessionTasks(w, r, "sess-1")
	}), http.MethodGet, "/api/v1/sessions/sess-1/tasks?page=1&page_size=10", nil)
	var page struct {
		Items []store.Task `json:"items"`
		Total int64        `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("解析分页失败: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Errorf("分页信封不符: total=%d items=%d", page.Total, len(page.Items))
	}

	// 4. 不存在的会话 → 404。
	rec = doJSON(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ListSessionTasks(w, r, "nope")
	}), http.MethodGet, "/api/v1/sessions/nope/tasks", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("未知会话 status = %d, want 404", rec.Code)
	}
}

// TestSessionTasksRouterPath 验证路由分发：/sessions/{id}/tasks 与会话详情并存。
func TestSessionTasksRouterPath(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()
	if err := h.store.CreateSession(ctx, &store.ChatSession{ID: "sess-r", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sessions/", func(w http.ResponseWriter, r *http.Request) {
		parts := splitPath(r.URL.Path)
		if len(parts) == 5 && parts[4] == "tasks" {
			h.ListSessionTasks(w, r, parts[3])
			return
		}
		if len(parts) != 4 {
			writeError(w, http.StatusNotFound, "未知路径")
			return
		}
		h.GetSessionByID(w, r, parts[3])
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-r/tasks", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("/tasks 路由 status = %d, want 200", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-r", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("会话详情路由 status = %d, want 200", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-r/bogus", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("未知子路径 status = %d, want 404", rec.Code)
	}
}

// TestTaskFilters 验证任务列表过滤参数（status/type/target/session_id/since/until）。
func TestTaskFilters(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()
	now := time.Now().UTC()
	seed := []*store.Task{
		{ID: "t-generate-1", Type: store.TaskTypeGenerate, Status: store.TaskStatusPending,
			TargetGroupID: "grp-a", SessionID: "s1", CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now},
		{ID: "t-apply-1", Type: store.TaskTypeApply, Status: store.TaskStatusDone,
			TargetInstanceUID: "col-a", TargetGroupID: "col-a", CreatedAt: now.Add(-time.Hour), UpdatedAt: now},
		{ID: "t-apply-2", Type: store.TaskTypeApply, Status: store.TaskStatusDone,
			TargetInstanceUID: "col-b", TargetGroupID: "col-b", SessionID: "s2", CreatedAt: now, UpdatedAt: now},
	}
	for _, tk := range seed {
		if err := h.store.CreateTask(ctx, tk); err != nil {
			t.Fatalf("CreateTask(%s): %v", tk.ID, err)
		}
	}

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "按状态", query: "?status=pending", want: []string{"t-generate-1"}},
		{name: "按类型", query: "?type=apply", want: []string{"t-apply-2", "t-apply-1"}},
		{name: "按目标实例", query: "?target=col-b", want: []string{"t-apply-2"}},
		{name: "按会话", query: "?session_id=s1", want: []string{"t-generate-1"}},
		{name: "组合（类型+状态）", query: "?type=apply&status=done", want: []string{"t-apply-2", "t-apply-1"}},
		{name: "时间下界（近 90 分钟）", query: "?since=" + now.Add(-90*time.Minute).Format(time.RFC3339), want: []string{"t-apply-2", "t-apply-1"}},
		{name: "时间上界（Unix 秒）", query: "?until=" + strconv.FormatInt(now.Add(-90*time.Minute).Unix(), 10), want: []string{"t-generate-1"}},
		{name: "无匹配", query: "?session_id=none", want: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, http.HandlerFunc(h.ListTasks), http.MethodGet, "/api/v1/tasks"+tc.query, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
			}
			var items []store.Task
			if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			got := make([]string, 0, len(items))
			for _, it := range items {
				got = append(got, it.ID)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("命中 %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("命中顺序 %v, want %v", got, tc.want)
				}
			}
		})
	}

	// 非法参数 → 400（时间与枚举一致，避免"筛无结果"被误读为"确无数据"）。
	for _, tc := range []struct{ name, query string }{
		{name: "非法时间参数", query: "?since=not-a-time"},
		{name: "非法状态枚举", query: "?status=bogus"},
		{name: "非法类型枚举", query: "?type=bogus"},
		{name: "非法 until 参数", query: "?until=13月"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, http.HandlerFunc(h.ListTasks), http.MethodGet, "/api/v1/tasks"+tc.query, nil)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
			}
		})
	}
}
