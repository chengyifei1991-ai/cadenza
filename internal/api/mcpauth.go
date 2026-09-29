// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// MCPAuth 包装 MCP handler（/mcp）：
//
//   - token 为空：直通（未启用鉴权，启动时已告警；保持 1.0 的开箱即用行为）；
//   - token 非空：要求请求携带 `Authorization: Bearer <token>`，常量时间比较，
//     不匹配返回 401 JSON。
//
// 目的：消除"MCP 端点无鉴权、外部 LLM/IDE 可触发审批"的公开敞口（1.1.0 MCP token）。
func MCPAuth(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := bearerTokenOf(r.Header.Get("Authorization"))
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			// MCP 客户端依赖 WWW-Authenticate 做鉴权发现（RFC 6750）；缺失时
			// 只能笼统失败，无法提示如何携带凭证。
			w.Header().Set("WWW-Authenticate", `Bearer realm="cadenza-mcp"`)
			writeError(w, http.StatusUnauthorized, "未认证的 MCP 请求")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// bearerTokenOf 解析 `Bearer <token>` 头，格式不符返回空串。
func bearerTokenOf(header string) string {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}
