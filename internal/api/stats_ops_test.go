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

// TestComputeOpsStats 表驱动验证效率指标聚合（纯函数）。
func TestComputeOpsStats(t *testing.T) {
	base := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	ev := func(taskID, from, to string, at time.Time) store.TaskEvent {
		return store.TaskEvent{TaskID: taskID, FromStatus: from, ToStatus: to, CreatedAt: at}
	}
	tests := []struct {
		name           string
		tasks          []store.Task
		events         []store.TaskEvent
		wantTotal      int
		wantDone       int
		wantFailed     int
		wantRate       float64
		wantWaitCount  int
		wantWaitAvgMs  int64
		wantDispatchN  int
		wantDispatchMs int64
		wantRbCount    int
		wantRbAvgMs    int64
	}{
		{
			name: "空窗口全零",
		},
		{
			name: "审批等待与下发时长各一段",
			tasks: []store.Task{
				{ID: "t1", Type: store.TaskTypeGenerate, Status: store.TaskStatusDone, CreatedAt: base, UpdatedAt: base},
			},
			events: []store.TaskEvent{
				ev("t1", "", "pending", base),
				ev("t1", "pending", "awaiting_approval", base),
				ev("t1", "awaiting_approval", "applying", base.Add(60*time.Second)), // 审批等待 60s
				ev("t1", "applying", "done", base.Add(90*time.Second)),              // 下发 30s
			},
			wantTotal: 1, wantDone: 1, wantRate: 1,
			wantWaitCount: 1, wantWaitAvgMs: 60000,
			wantDispatchN: 1, wantDispatchMs: 30000,
		},
		{
			name: "成功率为 done/(done+failed)",
			tasks: []store.Task{
				{ID: "t1", Type: store.TaskTypeApply, Status: store.TaskStatusDone, CreatedAt: base, UpdatedAt: base},
				{ID: "t2", Type: store.TaskTypeApply, Status: store.TaskStatusFailed, CreatedAt: base, UpdatedAt: base},
				{ID: "t3", Type: store.TaskTypeApply, Status: store.TaskStatusDone, CreatedAt: base, UpdatedAt: base},
				{ID: "t4", Type: store.TaskTypeApply, Status: store.TaskStatusDone, CreatedAt: base, UpdatedAt: base},
			},
			events: []store.TaskEvent{
				ev("t1", "awaiting_approval", "applying", base),
				ev("t1", "applying", "done", base.Add(10*time.Second)),
				ev("t2", "awaiting_approval", "applying", base),
				ev("t2", "applying", "failed", base.Add(20*time.Second)),
				ev("t3", "awaiting_approval", "applying", base),
				ev("t3", "applying", "done", base.Add(30*time.Second)),
				ev("t4", "awaiting_approval", "applying", base),
				ev("t4", "applying", "done", base.Add(40*time.Second)),
			},
			wantTotal: 4, wantDone: 3, wantFailed: 1, wantRate: 0.75,
			wantDispatchN: 4, wantDispatchMs: 25000,
		},
		{
			name: "回滚任务计入次数与平均时长",
			tasks: []store.Task{
				{ID: "rb1", Type: store.TaskTypeRollback, Status: store.TaskStatusDone, CreatedAt: base, UpdatedAt: base},
				{ID: "rb2", Type: store.TaskTypeRollback, Status: store.TaskStatusDone, CreatedAt: base, UpdatedAt: base},
			},
			events: []store.TaskEvent{
				ev("rb1", "", "pending", base),
				ev("rb1", "applying", "done", base.Add(40*time.Second)),
				ev("rb2", "", "pending", base),
				ev("rb2", "applying", "done", base.Add(60*time.Second)),
			},
			wantTotal: 2, wantDone: 2, wantRate: 1,
			wantDispatchN: 2, wantDispatchMs: 50000,
			wantRbCount: 2, wantRbAvgMs: 50000,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computeOpsStats(7, tc.tasks, tc.events)
			if got.WindowDays != 7 {
				t.Errorf("window_days = %d, want 7", got.WindowDays)
			}
			if got.Tasks.Total != tc.wantTotal || got.Tasks.Done != tc.wantDone || got.Tasks.Failed != tc.wantFailed {
				t.Errorf("tasks = %+v, want total=%d done=%d failed=%d", got.Tasks, tc.wantTotal, tc.wantDone, tc.wantFailed)
			}
			if got.Tasks.SuccessRate != tc.wantRate {
				t.Errorf("success_rate = %v, want %v", got.Tasks.SuccessRate, tc.wantRate)
			}
			if got.ApprovalWaitMs.Count != tc.wantWaitCount || got.ApprovalWaitMs.Avg != tc.wantWaitAvgMs {
				t.Errorf("approval_wait_ms = %+v, want count=%d avg=%d", got.ApprovalWaitMs, tc.wantWaitCount, tc.wantWaitAvgMs)
			}
			if got.DispatchMs.Count != tc.wantDispatchN || got.DispatchMs.Avg != tc.wantDispatchMs {
				t.Errorf("dispatch_ms = %+v, want count=%d avg=%d", got.DispatchMs, tc.wantDispatchN, tc.wantDispatchMs)
			}
			if got.Rollback.Count != tc.wantRbCount || got.Rollback.AvgDurationMs != tc.wantRbAvgMs {
				t.Errorf("rollback = %+v, want count=%d avg=%d", got.Rollback, tc.wantRbCount, tc.wantRbAvgMs)
			}
		})
	}
}

// TestSummarizePercentiles 验证分位取整规则（最近秩法）。
func TestSummarizePercentiles(t *testing.T) {
	var ds []time.Duration
	for i := 1; i <= 10; i++ {
		ds = append(ds, time.Duration(i)*time.Second)
	}
	got := summarize(ds)
	if got.Count != 10 || got.Avg != 5500 {
		t.Errorf("count/avg = %d/%d, want 10/5500", got.Count, got.Avg)
	}
	if got.P50 != 5000 || got.P90 != 9000 {
		t.Errorf("p50/p90 = %d/%d, want 5000/9000", got.P50, got.P90)
	}
	if empty := summarize(nil); empty.Count != 0 || empty.Avg != 0 || empty.P50 != 0 {
		t.Errorf("空集合应全零: %+v", empty)
	}
}

// TestStatsOpsEndpoint 验证端点装配与参数校验（含窗口边界）。
func TestStatsOpsEndpoint(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := h.store.CreateTask(ctx, &store.Task{
		ID: "ops-1", Type: store.TaskTypeApply, Status: store.TaskStatusDone,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	for _, e := range []store.TaskEvent{
		{TaskID: "ops-1", FromStatus: "awaiting_approval", ToStatus: "applying", CreatedAt: now.Add(-30 * time.Minute)},
		{TaskID: "ops-1", FromStatus: "applying", ToStatus: "done", CreatedAt: now.Add(-29 * time.Minute)},
	} {
		ev := e
		if err := h.store.AppendTaskEvent(ctx, &ev); err != nil {
			t.Fatalf("AppendTaskEvent: %v", err)
		}
	}

	rec := doJSON(t, http.HandlerFunc(h.StatsOps), http.MethodGet, "/api/v1/stats/ops", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got opsStatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got.WindowDays != 7 {
		t.Errorf("默认窗口 = %d, want 7", got.WindowDays)
	}
	if got.Tasks.Done != 1 || got.Tasks.SuccessRate != 1 {
		t.Errorf("tasks = %+v", got.Tasks)
	}
	if got.DispatchMs.Count != 1 || got.DispatchMs.Avg != 60000 {
		t.Errorf("dispatch_ms = %+v, want count=1 avg=60000", got.DispatchMs)
	}

	// 窗口参数边界：1 与 90 合法，0/91 非法。
	for _, q := range []string{"?window_days=1", "?window_days=90"} {
		if rec := doJSON(t, http.HandlerFunc(h.StatsOps), http.MethodGet, "/api/v1/stats/ops"+q, nil); rec.Code != http.StatusOK {
			t.Errorf("%s status = %d, want 200", q, rec.Code)
		}
	}
	for _, q := range []string{"?window_days=0", "?window_days=91", "?window_days=abc"} {
		if rec := doJSON(t, http.HandlerFunc(h.StatsOps), http.MethodGet, "/api/v1/stats/ops"+q, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d, want 400", q, rec.Code)
		}
	}
}
