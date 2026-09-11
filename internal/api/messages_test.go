// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/store"
)

// TestSessionMessagesEndpoint 验证会话消息 keyset 分页端点与参数校验。
func TestSessionMessagesEndpoint(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := h.store.CreateSession(ctx, &store.ChatSession{ID: "s-msg", CreatedAt: now}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	for i := 1; i <= 5; i++ {
		if err := h.store.AppendMessage(ctx, "s-msg", store.ChatMessage{
			Role: "user", Content: string(rune('a' + i - 1)), CreatedAt: now.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	type envelope struct {
		Items []store.ChatMessage `json:"items"`
		Total int64               `json:"total"`
	}
	fetch := func(query string) (envelope, int) {
		rec := doJSON(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.ListSessionMessages(w, r, "s-msg")
		}), http.MethodGet, "/api/v1/sessions/s-msg/messages"+query, nil)
		var env envelope
		if rec.Code == http.StatusOK {
			_ = json.Unmarshal(rec.Body.Bytes(), &env)
		}
		return env, rec.Code
	}
	contents := func(env envelope) []string {
		out := make([]string, 0, len(env.Items))
		for _, m := range env.Items {
			out = append(out, m.Content)
		}
		return out
	}
	equal := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	// 尾部窗口。
	env, code := fetch("?limit=2")
	if code != http.StatusOK || env.Total != 5 || !equal(contents(env), []string{"d", "e"}) {
		t.Errorf("尾部窗口 = %v (total=%d, code=%d), want [d e]", contents(env), env.Total, code)
	}
	// 向前翻历史。
	if env, _ = fetch("?before_id=4&limit=2"); !equal(contents(env), []string{"b", "c"}) {
		t.Errorf("before_id = %v, want [b c]", contents(env))
	}
	// 增量刷新。
	if env, _ = fetch("?after_id=3"); !equal(contents(env), []string{"d", "e"}) {
		t.Errorf("after_id = %v, want [d e]", contents(env))
	}
	// 默认 limit（不带参数）返回全部 5 条以内的尾部窗口。
	if env, _ = fetch(""); len(env.Items) != 5 || env.Total != 5 {
		t.Errorf("默认窗口 = %d 条 (total=%d), want 5", len(env.Items), env.Total)
	}

	// 参数校验：limit 越界 / 非数字游标 → 400；未知会话 → 404。
	for _, tc := range []struct {
		name  string
		query string
		want  int
	}{
		{name: "limit=0 越界", query: "?limit=0", want: http.StatusBadRequest},
		{name: "limit=101 越界", query: "?limit=101", want: http.StatusBadRequest},
		{name: "before_id 非数字", query: "?before_id=abc", want: http.StatusBadRequest},
		{name: "after_id 负数", query: "?after_id=-1", want: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.ListSessionMessages(w, r, "s-msg")
			}), http.MethodGet, "/api/v1/sessions/s-msg/messages"+tc.query, nil)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d (body=%s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
	rec := doJSON(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ListSessionMessages(w, r, "nope")
	}), http.MethodGet, "/api/v1/sessions/nope/messages", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("未知会话 status = %d, want 404", rec.Code)
	}
}

// TestAuditFiltersEndpoint 验证审计列表过滤参数（含 since=id 游标语义与新时间参数）。
func TestAuditFiltersEndpoint(t *testing.T) {
	h := newTestHandlers(t)
	ctx := context.Background()
	now := time.Now().UTC()
	seed := []store.AuditLog{
		{Actor: "admin", Action: store.AuditActionApply, Subject: "t-1", Detail: "d1", CreatedAt: now.Add(-2 * time.Hour)},
		{Actor: "agent", Action: store.AuditActionGenerate, Subject: "t-2", Detail: "d2", CreatedAt: now.Add(-time.Hour)},
		{Actor: "admin", Action: store.AuditActionApprove, Subject: "t-2", Detail: "d3", CreatedAt: now},
	}
	for i := range seed {
		if err := h.store.AppendAudit(ctx, &seed[i]); err != nil {
			t.Fatalf("AppendAudit: %v", err)
		}
	}

	details := func(query string) ([]string, int) {
		rec := doJSON(t, http.HandlerFunc(h.ListAudit), http.MethodGet, "/api/v1/audit"+query, nil)
		if rec.Code != http.StatusOK {
			return nil, rec.Code
		}
		var items []store.AuditLog
		if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		out := make([]string, 0, len(items))
		for _, it := range items {
			out = append(out, it.Detail)
		}
		return out, rec.Code
	}

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "按 actor", query: "?actor=admin", want: []string{"d3", "d1"}},
		{name: "按 action", query: "?action=generate", want: []string{"d2"}},
		{name: "按 subject", query: "?subject=t-2", want: []string{"d3", "d2"}},
		{name: "时间区间 from", query: "?from=" + now.Add(-90*time.Minute).Format(time.RFC3339), want: []string{"d3", "d2"}},
		{name: "since 作为 id 游标", query: "?since=2", want: []string{"d3"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, code := details(tc.query)
			if code != http.StatusOK {
				t.Fatalf("status = %d", code)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("命中 %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("命中 %v, want %v", got, tc.want)
				}
			}
		})
	}

	// 非法参数 → 400：action 枚举、since 非数字、from 非法时间。
	for _, tc := range []struct{ name, query string }{
		{name: "action 枚举非法", query: "?action=bogus"},
		{name: "since 非数字", query: "?since=abc"},
		{name: "from 非法时间", query: "?from=not-a-time"},
		{name: "to 非法时间", query: "?to=13月"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, http.HandlerFunc(h.ListAudit), http.MethodGet, "/api/v1/audit"+tc.query, nil)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
			}
		})
	}
}
