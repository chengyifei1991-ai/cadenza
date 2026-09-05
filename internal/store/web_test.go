// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// seedSession 创建一个会话并追加消息，返回会话 ID。
func seedSession(t *testing.T, st Store, id string, createdAt time.Time, msgs ...ChatMessage) string {
	t.Helper()
	ctx := context.Background()
	sess := &ChatSession{ID: id, CreatedAt: createdAt}
	if err := st.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession 失败: %v", err)
	}
	for i := range msgs {
		msgs[i].CreatedAt = createdAt.Add(time.Duration(i) * time.Second)
		if err := st.AppendMessage(ctx, sess.ID, msgs[i]); err != nil {
			t.Fatalf("AppendMessage 失败: %v", err)
		}
	}
	return sess.ID
}

// TestListSessionsSummary 验证会话列表的聚合摘要与排序。
func TestListSessionsSummary(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)

	older := seedSession(t, st, "sess-older", base.Add(-10*time.Minute),
		ChatMessage{Role: "user", Content: "给网关加 tail sampling"},
		ChatMessage{Role: "assistant", Content: "已生成配置（旧会话）"},
	)
	_ = seedSession(t, st, "sess-empty", base) // 空会话（应置底）
	newer := seedSession(t, st, "sess-newer", base.Add(-5*time.Minute),
		ChatMessage{Role: "user", Content: "优化 demo 的内存占用，这是一条用于验证预览截断长度的超长消息：" + repeatRune('长', 100)},
		ChatMessage{Role: "assistant", Content: "已优化"},
	)

	items, total, err := st.ListSessions(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListSessions 失败: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(items) != 3 {
		t.Fatalf("len(items) = %d, want 3", len(items))
	}
	// 排序：有消息的会话按最近消息倒序，空会话置底。
	if items[0].ID != newer || items[1].ID != older {
		t.Errorf("排序不符: got %s, %s, %s", items[0].ID, items[1].ID, items[2].ID)
	}
	if items[2].MessageCount != 0 {
		t.Errorf("空会话 message_count = %d, want 0", items[2].MessageCount)
	}
	if items[0].MessageCount != 2 || items[0].FirstMessage == "" || items[0].LastMessage != "已优化" {
		t.Errorf("会话摘要不符: %+v", items[0])
	}
	// 预览截断：超长消息应截断为"79 字符 + …"共 80 个字符（rune 计数）。
	if got := len([]rune(items[0].FirstMessage)); got != 80 {
		t.Errorf("first_message 长度 = %d, want 80: %q", got, items[0].FirstMessage)
	}
}

func repeatRune(r rune, n int) string {
	out := make([]rune, n)
	for i := range out {
		out[i] = r
	}
	return string(out)
}

// TestListSessionsPagination 验证分页信封与空库行为。
func TestListSessionsPagination(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	base := time.Now().UTC()

	// 空库：全量返回空数组而非 nil。
	items, total, err := st.ListSessions(ctx, 0, 0)
	if err != nil || total != 0 || items == nil {
		t.Fatalf("空库返回 items=%v total=%d err=%v", items, total, err)
	}

	seedSession(t, st, "p-1", base.Add(-3*time.Minute), ChatMessage{Role: "user", Content: "a"})
	seedSession(t, st, "p-2", base.Add(-2*time.Minute), ChatMessage{Role: "user", Content: "b"})
	seedSession(t, st, "p-3", base.Add(-time.Minute), ChatMessage{Role: "user", Content: "c"})

	page1, total, err := st.ListSessions(ctx, 1, 2)
	if err != nil || total != 3 || len(page1) != 2 {
		t.Fatalf("第 1 页 items=%d total=%d err=%v", len(page1), total, err)
	}
	page2, _, err := st.ListSessions(ctx, 2, 2)
	if err != nil || len(page2) != 1 {
		t.Fatalf("第 2 页 items=%d err=%v", len(page2), err)
	}
}

// TestSessionNotFound 验证查询不存在会话返回 ErrNotFound。
func TestSessionNotFound(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.GetSession(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSession(不存在) err = %v, want ErrNotFound", err)
	}
}

// TestCountGrouped 验证三组聚合统计（collectors/tasks/sessions）。
func TestCountGrouped(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for _, tc := range []struct {
		uid    string
		status CollectorStatus
	}{
		{"c1", CollectorStatusHealthy}, {"c2", CollectorStatusHealthy}, {"c3", CollectorStatusOffline},
	} {
		c := &Collector{InstanceUID: tc.uid, Hostname: tc.uid, LastSeenAt: now, Status: tc.status}
		if err := st.UpsertCollector(ctx, c); err != nil {
			t.Fatalf("UpsertCollector 失败: %v", err)
		}
	}
	byStatus, err := st.CountCollectorsByStatus(ctx)
	if err != nil {
		t.Fatalf("CountCollectorsByStatus 失败: %v", err)
	}
	if byStatus[CollectorStatusHealthy] != 2 || byStatus[CollectorStatusOffline] != 1 {
		t.Errorf("collectors 统计不符: %v", byStatus)
	}

	for _, tc := range []struct {
		id     string
		status TaskStatus
	}{
		{"t1", TaskStatusDone}, {"t2", TaskStatusDone}, {"t3", TaskStatusAwaitingApproval}, {"t4", TaskStatusFailed},
	} {
		tk := &Task{ID: tc.id, Type: TaskTypeApply, Status: tc.status,
			CreatedAt: now, UpdatedAt: now}
		if err := st.CreateTask(ctx, tk); err != nil {
			t.Fatalf("CreateTask 失败: %v", err)
		}
	}
	tasksByStatus, err := st.CountTasksByStatus(ctx)
	if err != nil {
		t.Fatalf("CountTasksByStatus 失败: %v", err)
	}
	if tasksByStatus[TaskStatusDone] != 2 || tasksByStatus[TaskStatusAwaitingApproval] != 1 ||
		tasksByStatus[TaskStatusFailed] != 1 {
		t.Errorf("tasks 统计不符: %v", tasksByStatus)
	}
	// 空状态分组不出现。
	if _, ok := tasksByStatus[TaskStatusPending]; ok {
		t.Errorf("pending 不应出现在统计中: %v", tasksByStatus)
	}

	seedSession(t, st, "count-1", now, ChatMessage{Role: "user", Content: "hi"})
	if n, err := st.CountSessions(ctx); err != nil || n != 1 {
		t.Errorf("CountSessions = %d, err = %v, want 1", n, err)
	}
}

// TestPing 验证数据库连通性探测。
func TestPing(t *testing.T) {
	st := newTestStore(t)
	if err := st.Ping(context.Background()); err != nil {
		t.Errorf("Ping 失败: %v", err)
	}
}
