// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package api

import (
	"net/http"

	"github.com/chengyifei1991-ai/cadenza/internal/version"
)

// SystemInfo 返回面向前端引导的系统信息（公开端点，不含敏感信息）。
// 前端据此决定登录页展示与演示模式横幅。
func SystemInfo(am *AuthManager, demoMode bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
			return
		}
		mode := "off"
		if am.Enabled() {
			mode = "simple"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"version":   version.Version,
			"auth_mode": mode,
			"demo_mode": demoMode,
		})
	}
}

// Healthz 处理 GET /healthz（公开探活端点）。
// 数据库连通性以字段呈现而非错误码，保证 LB/容器探活语义稳定。
func (h *Handlers) Healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	dbStatus := "up"
	if err := h.store.Ping(r.Context()); err != nil {
		dbStatus = "down"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "db": dbStatus})
}
