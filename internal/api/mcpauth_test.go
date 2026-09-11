// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMCPAuth 表驱动验证 MCP 端点鉴权中间件。
func TestMCPAuth(t *testing.T) {
	const token = "mcp-secret-token"
	tests := []struct {
		name       string
		configured string
		header     string
		wantStatus int
	}{
		{name: "未配置 token：直通", configured: "", header: "", wantStatus: http.StatusOK},
		{name: "未配置 token：带任意头也直通", configured: "", header: "Bearer whatever", wantStatus: http.StatusOK},
		{name: "已配置：缺失 Authorization → 401", configured: token, header: "", wantStatus: http.StatusUnauthorized},
		{name: "已配置：错误 token → 401", configured: token, header: "Bearer wrong-token", wantStatus: http.StatusUnauthorized},
		{name: "已配置：正确 token → 200", configured: token, header: "Bearer " + token, wantStatus: http.StatusOK},
		{name: "已配置：scheme 大小写不敏感（bearer）", configured: token, header: "bearer " + token, wantStatus: http.StatusOK},
		{name: "已配置：缺少 Bearer 前缀 → 401", configured: token, header: token, wantStatus: http.StatusUnauthorized},
		{name: "已配置：空 token 头 → 401", configured: token, header: "Bearer ", wantStatus: http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reached := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				reached = true
				w.WriteHeader(http.StatusOK)
			})
			h := MCPAuth(tc.configured, next)
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			wantReached := tc.wantStatus == http.StatusOK
			if reached != wantReached {
				t.Errorf("透传到 MCP handler = %v, want %v", reached, wantReached)
			}
			if tc.wantStatus == http.StatusUnauthorized {
				if got := rec.Body.String(); got == "" || got[0] != '{' {
					t.Errorf("401 应返回 JSON 错误体，实际: %q", got)
				}
				// MCP 客户端依赖 WWW-Authenticate 做鉴权发现（F-2 回归）。
				if www := rec.Header().Get("WWW-Authenticate"); !strings.Contains(www, "Bearer") {
					t.Errorf("401 缺少 WWW-Authenticate: Bearer 头，实际: %q", www)
				}
			}
		})
	}
}

// TestSystemInfoMCPAuth 验证 /system/info 暴露 mcp_auth 配置状态。
func TestSystemInfoMCPAuth(t *testing.T) {
	am := newTestAuth(t)
	for _, tc := range []struct {
		name    string
		enabled bool
		want    string
	}{
		{name: "未启用", enabled: false, want: `"mcp_auth":false`},
		{name: "已启用", enabled: true, want: `"mcp_auth":true`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, SystemInfo(am, false, tc.enabled), http.MethodGet, "/api/v1/system/info", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			if body := rec.Body.String(); !strings.Contains(body, tc.want) {
				t.Errorf("system/info 缺少 %s: %s", tc.want, body)
			}
		})
	}
}
