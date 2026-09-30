// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package task

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/store"
)

// newSweepStore 用真实 SQLite（临时文件）建库：巡检依赖条件更新与秒级时间比较，
// 用桩存无法证明这些语义。
func newSweepStore(t *testing.T) store.Store {
	t.Helper()
	st, err := store.New("sqlite", t.TempDir()+"/sweep.db")
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// seedTask 落一个指定状态、指定 updated_at 的任务。
func seedTask(t *testing.T, st store.Store, id string, status store.TaskStatus, typ store.TaskType, updatedAt time.Time) {
	t.Helper()
	if err := st.CreateTask(context.Background(), &store.Task{
		ID: id, Type: typ, Status: status, CreatedAt: updatedAt, UpdatedAt: updatedAt,
	}); err != nil {
		t.Fatalf("CreateTask(%s): %v", id, err)
	}
}

// TestSweepStuck 验证中间态超时巡检（F-18）：
// 超时的 applying/validating 收口为 failed（含事件与审计），未超时与终态任务不被动。
func TestSweepStuck(t *testing.T) {
	ctx := context.Background()
	st := newSweepStore(t)
	svc := NewService(st)
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)

	seedTask(t, st, "stuck-applying", store.TaskStatusApplying, store.TaskTypeApply, now.Add(-10*time.Minute))
	seedTask(t, st, "stuck-validating", store.TaskStatusValidating, store.TaskTypeGenerate, now.Add(-8*time.Minute))
	seedTask(t, st, "fresh-applying", store.TaskStatusApplying, store.TaskTypeApply, now.Add(-30*time.Second))
	seedTask(t, st, "old-done", store.TaskStatusDone, store.TaskTypeApply, now.Add(-24*time.Hour))
	seedTask(t, st, "old-awaiting", store.TaskStatusAwaitingApproval, store.TaskTypeApply, now.Add(-24*time.Hour))

	swept, err := svc.SweepStuck(ctx, 5*time.Minute, now)
	if err != nil {
		t.Fatalf("SweepStuck: %v", err)
	}
	if len(swept) != 2 {
		t.Fatalf("收口数量 = %d, want 2（只有停留超阈值的中间态）", len(swept))
	}
	got := map[string]store.TaskStatus{}
	for _, s := range swept {
		got[s.TaskID] = s.From
	}
	if got["stuck-applying"] != store.TaskStatusApplying || got["stuck-validating"] != store.TaskStatusValidating {
		t.Errorf("收口的任务/前态不符: %+v", got)
	}

	for _, id := range []string{"stuck-applying", "stuck-validating"} {
		tk, err := st.GetTask(ctx, id)
		if err != nil {
			t.Fatalf("GetTask(%s): %v", id, err)
		}
		if tk.Status != store.TaskStatusFailed {
			t.Errorf("%s status = %s, want failed", id, tk.Status)
		}
		if !strings.Contains(tk.Error, "巡检收口") {
			t.Errorf("%s error = %q, want 含巡检来源（可区分系统收口与人为失败）", id, tk.Error)
		}
		// 状态迁移事件：时间线与效率指标都要能看到这次收口
		events, err := st.ListTaskEventsByTask(ctx, id, 0)
		if err != nil {
			t.Fatalf("ListTaskEventsByTask(%s): %v", id, err)
		}
		if len(events) != 1 || events[0].ToStatus != string(store.TaskStatusFailed) {
			t.Errorf("%s 事件 = %+v, want 一条 → failed", id, events)
		}
	}

	// 未超时/终态/非中间态任务不被动
	for id, want := range map[string]store.TaskStatus{
		"fresh-applying": store.TaskStatusApplying,
		"old-done":       store.TaskStatusDone,
		"old-awaiting":   store.TaskStatusAwaitingApproval,
	} {
		tk, err := st.GetTask(ctx, id)
		if err != nil {
			t.Fatalf("GetTask(%s): %v", id, err)
		}
		if tk.Status != want {
			t.Errorf("%s status = %s, want %s（不该被巡检改动）", id, tk.Status, want)
		}
	}

	// 审计留痕（actor=system, action=sweep）
	logs, total, err := st.ListAudit(ctx, store.AuditFilter{Action: store.AuditActionSweep}, 0, 0)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if total != 2 || len(logs) != 2 {
		t.Fatalf("巡检审计 = %d 条, want 2", total)
	}
	for _, l := range logs {
		if l.Actor != "system" {
			t.Errorf("审计 actor = %q, want system", l.Actor)
		}
		if !strings.Contains(l.Detail, "巡检收口") {
			t.Errorf("审计 detail = %q, want 写明收口来源", l.Detail)
		}
	}
}

// TestSweepStuckDoesNotOverrideConcurrentTerminal 验证并发保护：
// 任务在巡检判定后被并发推进到终态（生效确认到达）时，巡检不得把终态覆盖成 failed。
func TestSweepStuckDoesNotOverrideConcurrentTerminal(t *testing.T) {
	ctx := context.Background()
	st := newSweepStore(t)
	svc := NewService(st)
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	seedTask(t, st, "race", store.TaskStatusApplying, store.TaskTypeApply, now.Add(-30*time.Minute))

	// 模拟：巡检读到"卡住"之后、写回之前，生效确认把任务推到了 done。
	done := &store.Task{ID: "race", Type: store.TaskTypeApply, Status: store.TaskStatusDone,
		CreatedAt: now.Add(-30 * time.Minute), UpdatedAt: now}
	if err := st.UpdateTask(ctx, done); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}

	// 直接验证条件更新：以"仍停留在 applying"为条件 → 不应命中
	ok, err := st.FailStuckTask(ctx, "race", store.TaskStatusApplying, now.Add(-5*time.Minute), "巡检收口")
	if err != nil {
		t.Fatalf("FailStuckTask: %v", err)
	}
	if ok {
		t.Fatal("任务已进入终态，条件更新不应命中（否则会覆盖 done）")
	}
	tk, err := st.GetTask(ctx, "race")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if tk.Status != store.TaskStatusDone {
		t.Fatalf("status = %s, want done（终态不可被巡检覆盖）", tk.Status)
	}

	// 巡检整体跑一遍也不该产生任何收口/审计
	swept, err := svc.SweepStuck(ctx, 5*time.Minute, now)
	if err != nil {
		t.Fatalf("SweepStuck: %v", err)
	}
	if len(swept) != 0 {
		t.Fatalf("收口数量 = %d, want 0", len(swept))
	}
	if _, total, _ := st.ListAudit(ctx, store.AuditFilter{Action: store.AuditActionSweep}, 0, 0); total != 0 {
		t.Errorf("不应写入巡检审计, got %d 条", total)
	}
}

// TestSweepStuckBoundary 验证秒级边界与关闭语义：
// 停留时长恰好等于阈值（同一整秒）不收口（半开区间）；timeout<=0 表示关闭。
func TestSweepStuckBoundary(t *testing.T) {
	ctx := context.Background()
	st := newSweepStore(t)
	svc := NewService(st)
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)

	// updated_at 恰好等于 cutoff（now-timeout）→ substr(updated_at,1,19) < cutoff 为假 → 不收口
	seedTask(t, st, "exactly-cutoff", store.TaskStatusApplying, store.TaskTypeApply, now.Add(-5*time.Minute))
	// 早一秒 → 收口
	seedTask(t, st, "one-second-more", store.TaskStatusApplying, store.TaskTypeApply, now.Add(-5*time.Minute-time.Second))

	swept, err := svc.SweepStuck(ctx, 5*time.Minute, now)
	if err != nil {
		t.Fatalf("SweepStuck: %v", err)
	}
	if len(swept) != 1 || swept[0].TaskID != "one-second-more" {
		t.Fatalf("收口 = %+v, want 仅 one-second-more（边界整秒不收口）", swept)
	}
	if swept[0].StuckFor < 5*time.Minute {
		t.Errorf("StuckFor = %s, want ≥ 5m", swept[0].StuckFor)
	}

	// timeout<=0：关闭语义，不做任何事
	if got, err := svc.SweepStuck(ctx, 0, now); err != nil || len(got) != 0 {
		t.Errorf("timeout=0 应收敛为不动作, got %+v err=%v", got, err)
	}
}
