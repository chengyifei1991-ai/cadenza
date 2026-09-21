// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/store"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestGetTaskDiffToolRegistered 验证 get_task_diff 已注册为可调用工具（MCP 暴露同一实现）。
func TestGetTaskDiffToolRegistered(t *testing.T) {
	deps := &Deps{Store: newDiffTestStore(t)}
	var found bool
	for _, tl := range NewTools(deps) {
		if tl.Declaration().Name != "get_task_diff" {
			continue
		}
		found = true
		if _, ok := tl.(tool.CallableTool); !ok {
			t.Fatal("get_task_diff 未实现 CallableTool（MCP 无法调用）")
		}
		if len(tl.Declaration().InputSchema.Required) == 0 {
			t.Error("get_task_diff 应要求 task_id")
		}
	}
	if !found {
		t.Fatal("NewTools 未注册 get_task_diff")
	}
}

// TestHandleGetTaskDiff 表驱动验证 diff 工具的返回与错误路径。
func TestHandleGetTaskDiff(t *testing.T) {
	ctx := context.Background()
	st := newDiffTestStore(t)
	now := time.Now().UTC()
	if err := st.CreateTask(ctx, &store.Task{
		ID: "t-1", Type: store.TaskTypeGenerate, Status: store.TaskStatusAwaitingApproval,
		BaseYAML: "a: 1\n", GeneratedYAML: "a: 2\n", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := st.CreateTask(ctx, &store.Task{
		ID: "t-empty", Type: store.TaskTypeRollback, Status: store.TaskStatusAwaitingApproval,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateTask(empty): %v", err)
	}
	deps := &Deps{Store: st}

	t.Run("返回基准/生成/统一 diff", func(t *testing.T) {
		got, err := deps.handleGetTaskDiff(ctx, map[string]any{"task_id": "t-1"})
		if err != nil {
			t.Fatalf("handleGetTaskDiff: %v", err)
		}
		m, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("返回类型 = %T", got)
		}
		if m["has_base"] != true {
			t.Errorf("has_base = %v, want true", m["has_base"])
		}
		d, _ := m["diff"].(string)
		if !strings.Contains(d, "-a: 1") || !strings.Contains(d, "+a: 2") {
			t.Errorf("diff 不符:\n%s", d)
		}
	})

	t.Run("task_id 缺失报错", func(t *testing.T) {
		if _, err := deps.handleGetTaskDiff(ctx, map[string]any{}); err == nil {
			t.Error("缺 task_id 应报错")
		}
	})

	t.Run("无生成配置报错", func(t *testing.T) {
		if _, err := deps.handleGetTaskDiff(ctx, map[string]any{"task_id": "t-empty"}); err == nil {
			t.Error("无生成配置应报错")
		}
	})

	t.Run("任务不存在报错", func(t *testing.T) {
		if _, err := deps.handleGetTaskDiff(ctx, map[string]any{"task_id": "nope"}); err == nil {
			t.Error("未知任务应报错")
		}
	})
}

// newDiffTestStore 创建内存 SQLite store。
func newDiffTestStore(t *testing.T) store.Store {
	t.Helper()
	st, err := store.New("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("store.New 失败: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}
