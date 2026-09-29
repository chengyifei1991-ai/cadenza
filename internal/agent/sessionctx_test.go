// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package agent

import (
	"context"
	"testing"
)

// TestSessionIDContext 表驱动验证会话上下文注入/读取（任务↔会话绑定地基）。
func TestSessionIDContext(t *testing.T) {
	tests := []struct {
		name      string
		inject    string
		wantInCtx string
	}{
		{name: "注入会话 ID", inject: "sess-1", wantInCtx: "sess-1"},
		{name: "空会话 ID 不注入", inject: "", wantInCtx: ""},
		{name: "覆盖既有会话 ID", inject: "sess-2", wantInCtx: "sess-2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := context.Background()
			if tc.name == "覆盖既有会话 ID" {
				base = withSessionID(base, "old-sess")
			}
			ctx := withSessionID(base, tc.inject)
			if got := sessionIDFromCtx(ctx); got != tc.wantInCtx {
				t.Errorf("sessionIDFromCtx = %q, want %q", got, tc.wantInCtx)
			}
			// 非会话调用（如 MCP 直调）读取空串，任务 SessionID 为空。
			if tc.inject == "" {
				if got := sessionIDFromCtx(context.Background()); got != "" {
					t.Errorf("空白 ctx 应为空串，got %q", got)
				}
			}
		})
	}
}
