// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/store"
)

// TestGetTaskDiffEndpoint 验证任务级 diff 端点（1.1.0-d）：
// 有基准 → unified diff；无生成配置 → 409；未知任务 → 404。
func TestGetTaskDiffEndpoint(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()
	now := time.Now().UTC()
	base := "receivers:\n  otlp:\n    protocols:\n      grpc:\n        endpoint: 0.0.0.0:4317\n"
	gen := "receivers:\n  otlp:\n    protocols:\n      grpc:\n        endpoint: 0.0.0.0:14317\n"
	seed := []*store.Task{
		{ID: "t-diff", Type: store.TaskTypeGenerate, Status: store.TaskStatusAwaitingApproval,
			GeneratedYAML: gen, BaseYAML: base, CreatedAt: now, UpdatedAt: now},
		{ID: "t-no-gen", Type: store.TaskTypeRollback, Status: store.TaskStatusAwaitingApproval,
			RollbackVersionID: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "t-no-base", Type: store.TaskTypeApply, Status: store.TaskStatusAwaitingApproval,
			GeneratedYAML: gen, CreatedAt: now, UpdatedAt: now},
	}
	for _, tk := range seed {
		if err := h.store.CreateTask(ctx, tk); err != nil {
			t.Fatalf("CreateTask(%s): %v", tk.ID, err)
		}
	}

	rec := doJSON(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.GetTaskDiff(w, r, "t-diff")
	}), http.MethodGet, "/api/v1/tasks/t-diff/diff", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		TaskID        string `json:"task_id"`
		BaseYAML      string `json:"base_yaml"`
		GeneratedYAML string `json:"generated_yaml"`
		Diff          string `json:"diff"`
		HasBase       bool   `json:"has_base"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !got.HasBase || got.BaseYAML != base {
		t.Errorf("has_base=%v base 不符", got.HasBase)
	}
	if !strings.Contains(got.Diff, "@@") || !strings.Contains(got.Diff, "-        endpoint: 0.0.0.0:4317") ||
		!strings.Contains(got.Diff, "+        endpoint: 0.0.0.0:14317") {
		t.Errorf("diff 内容不符:\n%s", got.Diff)
	}

	// 无基准：has_base=false，但 diff 仍给出全部新增行（前端提示"无基准"）。
	rec = doJSON(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.GetTaskDiff(w, r, "t-no-base")
	}), http.MethodGet, "/api/v1/tasks/t-no-base/diff", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("无基准 status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"has_base":false`) {
		t.Errorf("无基准应标记 has_base=false: %s", rec.Body.String())
	}

	// 无生成配置 → 409。
	rec = doJSON(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.GetTaskDiff(w, r, "t-no-gen")
	}), http.MethodGet, "/api/v1/tasks/t-no-gen/diff", nil)
	if rec.Code != http.StatusConflict {
		t.Errorf("无生成配置 status = %d, want 409", rec.Code)
	}
	// 未知任务 → 404。
	rec = doJSON(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.GetTaskDiff(w, r, "nope")
	}), http.MethodGet, "/api/v1/tasks/nope/diff", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("未知任务 status = %d, want 404", rec.Code)
	}
}

// TestTaskDiffRouterPath 验证路由把 /tasks/{id}/diff 分发到 diff 处理器。
func TestTaskDiffRouterPath(t *testing.T) {
	parts := splitPath("/api/v1/tasks/abc/diff")
	if len(parts) != 5 || parts[3] != "abc" || parts[4] != "diff" {
		t.Fatalf("splitPath 结果不符: %v", parts)
	}
}
