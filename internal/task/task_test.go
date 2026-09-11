// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package task

import (
	"context"
	"testing"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/store"
)

// TestCanTransition 表驱动测试状态迁移合法性。
func TestCanTransition(t *testing.T) {
	tests := []struct {
		name string
		from store.TaskStatus
		to   store.TaskStatus
		want bool
	}{
		{"pending→generating 合法", store.TaskStatusPending, store.TaskStatusGenerating, true},
		{"failed→generating 可重试", store.TaskStatusFailed, store.TaskStatusGenerating, true},
		{"generating→validating 合法", store.TaskStatusGenerating, store.TaskStatusValidating, true},
		{"validating→awaiting_approval 合法", store.TaskStatusValidating, store.TaskStatusAwaitingApproval, true},
		{"awaiting_approval→applying 合法", store.TaskStatusAwaitingApproval, store.TaskStatusApplying, true},
		{"applying→done 合法", store.TaskStatusApplying, store.TaskStatusDone, true},
		{"awaiting_approval→rejected 合法", store.TaskStatusAwaitingApproval, store.TaskStatusRejected, true},
		{"pending→failed 合法", store.TaskStatusPending, store.TaskStatusFailed, true},
		{"generating→failed 合法", store.TaskStatusGenerating, store.TaskStatusFailed, true},
		{"validating→failed 合法", store.TaskStatusValidating, store.TaskStatusFailed, true},
		{"applying→failed 合法", store.TaskStatusApplying, store.TaskStatusFailed, true},
		{"done→failed 非法（终态）", store.TaskStatusDone, store.TaskStatusFailed, false},
		{"rejected→failed 非法（终态）", store.TaskStatusRejected, store.TaskStatusFailed, false},
		{"done→generating 非法（终态）", store.TaskStatusDone, store.TaskStatusGenerating, false},
		{"pending→applying 非法（跳级）", store.TaskStatusPending, store.TaskStatusApplying, false},
		{"generating→done 非法（跳级）", store.TaskStatusGenerating, store.TaskStatusDone, false},
		{"awaiting_approval→generating 非法", store.TaskStatusAwaitingApproval, store.TaskStatusGenerating, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanTransition(tt.from, tt.to); got != tt.want {
				t.Errorf("CanTransition(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

// fakeStore 是最小 Store 实现，仅支撑 Service 测试。
type fakeStore struct {
	tasks map[string]*store.Task
}

func newFakeStore() *fakeStore { return &fakeStore{tasks: map[string]*store.Task{}} }

func (f *fakeStore) CreateTask(_ context.Context, t *store.Task) error {
	f.tasks[t.ID] = t
	return nil
}
func (f *fakeStore) UpdateTask(_ context.Context, t *store.Task) error {
	f.tasks[t.ID] = t
	return nil
}
func (f *fakeStore) GetTask(_ context.Context, id string) (*store.Task, error) {
	t, ok := f.tasks[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return t, nil
}
func (f *fakeStore) ListTasks(_ context.Context, _ store.TaskFilter, _, _ int) ([]store.Task, int64, error) {
	return nil, 0, nil
}
func (f *fakeStore) Close() error { return nil }
func (f *fakeStore) UpsertCollector(context.Context, *store.Collector) error {
	return nil
}
func (f *fakeStore) GetCollector(context.Context, string) (*store.Collector, error) {
	return nil, store.ErrNotFound
}
func (f *fakeStore) ListCollectors(context.Context, int, int) ([]store.Collector, int64, error) {
	return nil, 0, nil
}
func (f *fakeStore) UpsertGroup(context.Context, *store.CollectorGroup) error { return nil }
func (f *fakeStore) GetGroup(context.Context, string) (*store.CollectorGroup, error) {
	return nil, store.ErrNotFound
}
func (f *fakeStore) ListGroups(context.Context) ([]store.CollectorGroup, error) {
	return nil, nil
}
func (f *fakeStore) CreateConfigVersion(context.Context, *store.ConfigVersion) error { return nil }
func (f *fakeStore) ListConfigVersions(context.Context, string, int, int) ([]store.ConfigVersion, int64, error) {
	return nil, 0, nil
}
func (f *fakeStore) CreateSession(context.Context, *store.ChatSession) error { return nil }
func (f *fakeStore) GetSession(context.Context, string) (*store.ChatSession, error) {
	return nil, store.ErrNotFound
}
func (f *fakeStore) AppendMessage(context.Context, string, store.ChatMessage) error { return nil }
func (f *fakeStore) ListSessions(context.Context, int, int) ([]store.SessionSummary, int64, error) {
	return nil, 0, nil
}
func (f *fakeStore) Ping(context.Context) error                   { return nil }
func (f *fakeStore) CountSessions(context.Context) (int64, error) { return 0, nil }
func (f *fakeStore) CountCollectorsByStatus(context.Context) (map[store.CollectorStatus]int64, error) {
	return nil, nil
}
func (f *fakeStore) CountTasksByStatus(context.Context) (map[store.TaskStatus]int64, error) {
	return nil, nil
}
func (f *fakeStore) CreateAgentRun(context.Context, *store.AgentRun) error { return nil }
func (f *fakeStore) UpdateAgentRun(context.Context, *store.AgentRun) error { return nil }
func (f *fakeStore) AppendAudit(context.Context, *store.AuditLog) error    { return nil }
func (f *fakeStore) ListAudit(context.Context, int64, int, int) ([]store.AuditLog, int64, error) {
	return nil, 0, nil
}

// TestServiceApproveReject 表驱动测试审批/拒绝的完整链路。
func TestServiceApproveReject(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name       string
		initial    store.TaskStatus
		action     string // approve | reject
		wantErr    bool
		wantStatus store.TaskStatus
	}{
		{"审批待审批任务成功", store.TaskStatusAwaitingApproval, "approve", false, store.TaskStatusApplying},
		{"拒绝待审批任务成功", store.TaskStatusAwaitingApproval, "reject", false, store.TaskStatusRejected},
		{"审批非待审批任务失败", store.TaskStatusPending, "approve", true, store.TaskStatusPending},
		{"拒绝已完成任务失败", store.TaskStatusDone, "reject", true, store.TaskStatusDone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeStore()
			svc := NewService(f)
			id := "task-" + tt.name
			now := time.Now().UTC()
			task := &store.Task{
				ID: id, Type: store.TaskTypeGenerate, Status: tt.initial,
				GeneratedYAML: "receivers: {}", CreatedAt: now, UpdatedAt: now,
			}
			if err := svc.Create(ctx, task); err != nil {
				t.Fatalf("Create 失败: %v", err)
			}
			var err error
			switch tt.action {
			case "approve":
				_, err = svc.Approve(ctx, id, "alice")
			case "reject":
				_, err = svc.Reject(ctx, id, "alice", "方案不符")
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			got, _ := f.GetTask(ctx, id)
			if got.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, tt.wantStatus)
			}
			if tt.action == "approve" && got.Approver != "alice" {
				t.Errorf("approver = %q, want alice", got.Approver)
			}
		})
	}
}
