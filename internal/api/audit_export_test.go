// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/store"
)

// seedAuditExport 造 3 条审计（含逗号/引号/换行的 detail，验证 CSV 转义）。
func seedAuditExport(t *testing.T, h *Handlers) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	rows := []store.AuditLog{
		{ID: 1, Actor: "admin", Action: store.AuditActionApply, Subject: "t-1",
			Detail: `端口 4317, batch 5s`, CreatedAt: now},
		{ID: 2, Actor: "mcp", Action: store.AuditActionGenerate, Subject: "t-2",
			Detail: "含\"引号\"与换行\n第二行", CreatedAt: now.Add(time.Minute)},
		{ID: 3, Actor: "admin", Action: store.AuditActionApprove, Subject: "t-2",
			Detail: "审批通过", CreatedAt: now.Add(2 * time.Minute)},
	}
	for i := range rows {
		if err := h.store.AppendAudit(ctx, &rows[i]); err != nil {
			t.Fatalf("AppendAudit: %v", err)
		}
	}
}

// TestExportAuditCSV 验证 CSV 导出：BOM + 表头 + 行数 + 转义 + 附件头。
func TestExportAuditCSV(t *testing.T) {
	h := newTestHandlers(t)
	seedAuditExport(t, h)

	rec := doJSON(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ExportAudit(w, r)
	}), http.MethodGet, "/api/v1/audit/export", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, ".csv") {
		t.Errorf("Content-Disposition = %q, want attachment 且 .csv 文件名", cd)
	}
	body := rec.Body.Bytes()
	head := body
	if len(head) > 3 {
		head = head[:3]
	}
	if len(body) < 3 || body[0] != 0xEF || body[1] != 0xBB || body[2] != 0xBF {
		t.Fatalf("CSV 应以 UTF-8 BOM 开头（Excel 中文兼容），got %q", head)
	}
	rows, err := csv.NewReader(strings.NewReader(string(body[3:]))).ReadAll()
	if err != nil {
		t.Fatalf("CSV 解析失败: %v", err)
	}
	if len(rows) != 4 { // 表头 + 3 行
		t.Fatalf("CSV 行数 = %d, want 4（含表头）", len(rows))
	}
	wantHeader := []string{"id", "created_at", "actor", "action", "subject", "detail"}
	for i, w := range wantHeader {
		if rows[0][i] != w {
			t.Errorf("表头[%d] = %q, want %q", i, rows[0][i], w)
		}
	}
	// id 降序（与列表同序）：最新一条（approve/t-3）在前
	if rows[1][3] != "approve" {
		t.Errorf("首行 action = %q, want approve（列表同序：id 降序）", rows[1][3])
	}
	// 逗号 / 引号 / 换行必须被正确转义（csv 解析回原值即证明未被截断）
	joined := rec.Body.String()
	if !strings.Contains(joined, `端口 4317, batch 5s`) {
		t.Errorf("含逗号的 detail 未按原文导出：%s", joined)
	}
	if !strings.Contains(joined, "第二行") {
		t.Errorf("含换行的 detail 未完整导出（可能被截断）")
	}
}

// TestExportAuditJSONAndAuditTrail 验证 JSON 导出 + 导出自审计留痕。
func TestExportAuditJSONAndAuditTrail(t *testing.T) {
	h := newTestHandlers(t)
	seedAuditExport(t, h)
	ctx := context.Background()

	rec := doJSON(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ExportAudit(w, r)
	}), http.MethodGet, "/api/v1/audit/export?format=json&actor=admin", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Items []store.AuditLog `json:"items"`
		Total int64            `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// 过滤生效：只有 admin 的 2 条
	if got.Total != 2 || len(got.Items) != 2 {
		t.Fatalf("total/items = %d/%d, want 2/2（actor=admin 过滤）", got.Total, len(got.Items))
	}
	for _, it := range got.Items {
		if it.Actor != "admin" {
			t.Errorf("导出了非目标 actor 的记录：%s", it.Actor)
		}
	}

	// 导出动作本身留痕（action=export，subject 记录筛选摘要）
	logs, total, err := h.store.ListAudit(ctx, store.AuditFilter{Action: store.AuditActionExport}, 0, 0)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if total != 1 || len(logs) != 1 {
		t.Fatalf("导出审计记录数 = %d, want 1", total)
	}
	if !strings.Contains(logs[0].Subject, "actor=admin") {
		t.Errorf("导出审计 subject = %q, want 含筛选摘要 actor=admin", logs[0].Subject)
	}
	if !strings.Contains(logs[0].Detail, "2 行") {
		t.Errorf("导出审计 detail = %q, want 含导出行数", logs[0].Detail)
	}
}

// TestExportAuditLimits 验证上限与参数校验：超限 400、非法 format 400、非法 limit 400、非 GET 405。
func TestExportAuditLimits(t *testing.T) {
	h := newTestHandlers(t)
	seedAuditExport(t, h)

	tests := []struct {
		name   string
		method string
		query  string
		want   int
		substr string
	}{
		{name: "超限拒绝而非静默截断", method: http.MethodGet, query: "?limit=2", want: 400, substr: "导出范围过大"},
		{name: "limit 在范围内可导出", method: http.MethodGet, query: "?limit=3", want: 200},
		{name: "非法 format", method: http.MethodGet, query: "?format=xml", want: 400, substr: "format 取值非法"},
		{name: "limit 越界", method: http.MethodGet, query: "?limit=1000001", want: 400, substr: "limit 必须在"},
		{name: "limit 非数字", method: http.MethodGet, query: "?limit=abc", want: 400, substr: "limit 必须在"},
		{name: "非法 action 参数", method: http.MethodGet, query: "?action=nope", want: 400, substr: "action 取值非法"},
		{name: "非法时间参数", method: http.MethodGet, query: "?from=not-a-time", want: 400, substr: "from 参数非法"},
		{name: "仅支持 GET", method: http.MethodPost, query: "", want: 405},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.ExportAudit(w, r)
			}), tc.method, "/api/v1/audit/export"+tc.query, nil)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d（body=%s）", rec.Code, tc.want, rec.Body.String())
			}
			if tc.substr != "" && !strings.Contains(rec.Body.String(), tc.substr) {
				t.Errorf("错误信息应含 %q，got %s", tc.substr, rec.Body.String())
			}
		})
	}
}

// TestExportAuditActionEnum 验证新增的 export 动作进入合法枚举（列表筛选可用）。
func TestExportAuditActionEnum(t *testing.T) {
	if !store.IsValidAuditAction("export") {
		t.Fatal("export 应成为合法审计动作（导出留痕与筛选都需要）")
	}
	if store.IsValidAuditAction("nope") {
		t.Fatal("非法动作不应通过校验")
	}
}
