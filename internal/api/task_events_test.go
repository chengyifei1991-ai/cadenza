// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/store"
)

// TestGetTaskEventsEndpoint 验证任务状态流转时间线端点（1.2.0 C-a）：
// 按 id 升序回放该任务的事件、隔离其他任务的事件；未知任务 404；非 GET 405。
func TestGetTaskEventsEndpoint(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := h.store.CreateTask(ctx, &store.Task{
		ID: "t-tl", Type: store.TaskTypeApply, Status: store.TaskStatusDone,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// 乱序插入（时间戳倒挂），端点必须按 id 升序回放，而不是按时间排序
	events := []*store.TaskEvent{
		{TaskID: "t-tl", FromStatus: "", ToStatus: string(store.TaskStatusAwaitingApproval), CreatedAt: now},
		{TaskID: "t-tl", FromStatus: string(store.TaskStatusAwaitingApproval), ToStatus: string(store.TaskStatusApplying), CreatedAt: now.Add(2 * time.Second)},
		{TaskID: "t-tl", FromStatus: string(store.TaskStatusApplying), ToStatus: string(store.TaskStatusDone), CreatedAt: now.Add(3 * time.Second)},
		// 其他任务的事件不得混入
		{TaskID: "t-other", FromStatus: "", ToStatus: string(store.TaskStatusPending), CreatedAt: now},
	}
	for _, e := range events {
		if err := h.store.AppendTaskEvent(ctx, e); err != nil {
			t.Fatalf("AppendTaskEvent: %v", err)
		}
	}

	var got struct {
		TaskID string            `json:"task_id"`
		Items  []store.TaskEvent `json:"items"`
		Total  int               `json:"total"`
	}
	rec := doJSON(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.GetTaskEvents(w, r, "t-tl")
	}), http.MethodGet, "/api/v1/tasks/t-tl/events", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.TaskID != "t-tl" {
		t.Errorf("task_id = %q, want t-tl", got.TaskID)
	}
	if got.Total != 3 || len(got.Items) != 3 {
		t.Fatalf("total/items = %d/%d, want 3/3（body=%s）", got.Total, len(got.Items), rec.Body.String())
	}
	wantTo := []string{
		string(store.TaskStatusAwaitingApproval),
		string(store.TaskStatusApplying),
		string(store.TaskStatusDone),
	}
	for i, want := range wantTo {
		if got.Items[i].ToStatus != want {
			t.Errorf("items[%d].to_status = %q, want %q（应按 id 升序回放）", i, got.Items[i].ToStatus, want)
		}
		if got.Items[i].FromStatus != "" && i > 0 && got.Items[i].FromStatus != wantTo[i-1] {
			t.Errorf("items[%d].from_status = %q, want %q（迁移应首尾相接）", i, got.Items[i].FromStatus, wantTo[i-1])
		}
		for _, it := range got.Items {
			if it.TaskID != "t-tl" {
				t.Errorf("混入了其他任务的事件：%s", it.TaskID)
			}
		}
	}

	// 无事件的任务 → 200 + 空数组（前端可用空态而非报错）
	if err := h.store.CreateTask(ctx, &store.Task{
		ID: "t-none", Type: store.TaskTypeApply, Status: store.TaskStatusPending,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateTask(t-none): %v", err)
	}
	rec = doJSON(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.GetTaskEvents(w, r, "t-none")
	}), http.MethodGet, "/api/v1/tasks/t-none/events", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("无事件任务 status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Total != 0 || len(got.Items) != 0 {
		t.Errorf("无事件任务应为空时间线，got total=%d items=%d", got.Total, len(got.Items))
	}

	// 未知任务 → 404；非 GET → 405
	rec = doJSON(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.GetTaskEvents(w, r, "nope")
	}), http.MethodGet, "/api/v1/tasks/nope/events", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("未知任务 status = %d, want 404", rec.Code)
	}
	rec = doJSON(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.GetTaskEvents(w, r, "t-tl")
	}), http.MethodPost, "/api/v1/tasks/t-tl/events", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", rec.Code)
	}
}
