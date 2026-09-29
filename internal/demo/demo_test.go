// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package demo

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/store"
)

// newEmptyStore 创建内存 SQLite 空库。
func newEmptyStore(t *testing.T) store.Store {
	t.Helper()
	st, err := store.New("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("store.New 失败: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// discardLogger 丢弃日志输出。
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestSeedEmptyAndIdempotent 验证演示数据注入与幂等（重复 Seed 不重复注入）。
func TestSeedEmptyAndIdempotent(t *testing.T) {
	ctx := context.Background()
	st := newEmptyStore(t)
	if err := Seed(ctx, st, discardLogger()); err != nil {
		t.Fatalf("Seed 失败: %v", err)
	}

	assertCounts := func(stage string) {
		t.Helper()
		collectors, _, err := st.ListCollectors(ctx, 0, 0)
		if err != nil {
			t.Fatalf("%s: ListCollectors 失败: %v", stage, err)
		}
		if len(collectors) != 5 {
			t.Errorf("%s: collectors = %d, want 5", stage, len(collectors))
		}
		tasks, _, err := st.ListTasks(ctx, store.TaskFilter{}, 0, 0)
		if err != nil {
			t.Fatalf("%s: ListTasks 失败: %v", stage, err)
		}
		if len(tasks) != 3 {
			t.Errorf("%s: tasks = %d, want 3", stage, len(tasks))
		}
		versions, _, err := st.ListConfigVersions(ctx, "demo-gateway-1", 0, 0)
		if err != nil {
			t.Fatalf("%s: ListConfigVersions 失败: %v", stage, err)
		}
		if len(versions) != 2 {
			t.Errorf("%s: versions = %d, want 2", stage, len(versions))
		}
		sessions, total, err := st.ListSessions(ctx, 0, 0)
		if err != nil || total != 1 || len(sessions) != 1 {
			t.Errorf("%s: sessions = %d/%d, err = %v, want 1", stage, len(sessions), total, err)
		}
		// 1.1.0：演示任务应与演示会话绑定（AI 助手"本会话任务"可见）。
		sessionsAll, _, err := st.ListSessions(ctx, 0, 0)
		if err != nil || len(sessionsAll) == 0 {
			t.Fatalf("%s: 无演示会话: %v", stage, err)
		}
		bound, _, err := st.ListTasks(ctx, store.TaskFilter{SessionID: sessionsAll[0].ID}, 0, 0)
		if err != nil || len(bound) < 2 {
			t.Errorf("%s: 会话绑定任务 = %d（err=%v），want >= 2", stage, len(bound), err)
		}

		// 至少有一条待审批任务可供演示审批流。
		awaiting, _, err := st.ListTasks(ctx, store.TaskFilter{Status: store.TaskStatusAwaitingApproval}, 0, 0)
		if err != nil || len(awaiting) != 1 {
			t.Errorf("%s: awaiting_approval = %d, err = %v, want 1", stage, len(awaiting), err)
		}
	}

	assertCounts("首次注入")
	// 二次注入：库非空应跳过。
	if err := Seed(ctx, st, discardLogger()); err != nil {
		t.Fatalf("二次 Seed 失败: %v", err)
	}
	assertCounts("二次注入（应跳过）")
}

// TestSeedNonEmptyStore 验证非空库时 Seed 直接跳过（不污染真实数据）。
func TestSeedNonEmptyStore(t *testing.T) {
	ctx := context.Background()
	st := newEmptyStore(t)
	if err := st.UpsertCollector(ctx, &store.Collector{
		InstanceUID: "real-1", Hostname: "real", LastSeenAt: time.Now().UTC(),
		Status: store.CollectorStatusHealthy,
	}); err != nil {
		t.Fatalf("UpsertCollector 失败: %v", err)
	}
	if err := Seed(ctx, st, discardLogger()); err != nil {
		t.Fatalf("Seed 失败: %v", err)
	}
	collectors, _, err := st.ListCollectors(ctx, 0, 0)
	if err != nil || len(collectors) != 1 || collectors[0].InstanceUID != "real-1" {
		t.Fatalf("Seed 应跳过非空库: collectors = %+v, err = %v", collectors, err)
	}
}
