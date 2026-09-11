// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package agent

import "context"

// sessionCtxKey 是会话上下文在 ctx 中的私有键类型（避免与其他包冲突）。
type sessionCtxKey struct{}

// withSessionID 把会话 ID 注入 ctx（Orchestrator 在 ReAct 循环前调用）。
func withSessionID(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionCtxKey{}, sessionID)
}

// sessionIDFromCtx 读取 ctx 中的会话 ID；非会话调用（MCP 直调）返回空串。
func sessionIDFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(sessionCtxKey{}).(string); ok {
		return v
	}
	return ""
}
