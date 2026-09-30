// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/store"
)

// defaultAuditExportLimit 是审计导出的默认行数上限；超限返回 400 而不是静默截断
// （导出语义是"全量快照"，截断后不给提示会得到一份看起来完整的假数据）。
const defaultAuditExportLimit = 100000

// maxAuditExportLimit 是 limit 参数上限（防止单请求拉爆内存）。
const maxAuditExportLimit = 1000000

// ExportAudit 处理 GET /api/v1/audit/export：
// 按与列表一致的筛选条件（action/actor/subject/since/from/to）导出审计，
// format=csv（默认）或 json；导出动作本身也写入审计（谁在什么时候导出了什么范围）。
func (h *Handlers) ExportAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	filter, err := auditFilterFromQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	q := r.URL.Query()
	format := strings.ToLower(strings.TrimSpace(q.Get("format")))
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "json" {
		writeError(w, http.StatusBadRequest, "format 取值非法（合法值：csv/json）")
		return
	}
	limit, err := intParamInRange(q.Get("limit"), 1, maxAuditExportLimit)
	if err != nil {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("limit 必须在 1~%d 之间", maxAuditExportLimit))
		return
	}
	if limit == 0 {
		limit = defaultAuditExportLimit
	}

	// 一次取回（id 降序，与列表同序）+ 总数：超限直接拒绝，避免"导出到一半发现太大"。
	items, total, err := h.store.ListAudit(r.Context(), filter, 0, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询审计失败")
		return
	}
	if total > int64(limit) {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("导出范围过大（%d 行，上限 %d）：请收窄筛选（action/actor/from/to）或调小窗口",
				total, limit))
		return
	}

	// 导出自审计：动作与被导出的筛选范围留痕（放在写出之前，失败也不影响导出结果）。
	if err := h.store.AppendAudit(r.Context(), &store.AuditLog{
		Actor:     approverOf(r, ""),
		Action:    store.AuditActionExport,
		Subject:   auditExportSubject(filter),
		Detail:    fmt.Sprintf("导出审计 %d 行（format=%s, limit=%d）", total, format, limit),
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		// 审计写入失败不阻断导出，但要如实告警（不静默）。
		h.logger.Warn("审计导出留痕失败", "error", err)
	}

	stamp := time.Now().Format("20060102-150405")
	if format == "json" {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="cadenza-audit-%s.json"`, stamp))
		writeJSON(w, http.StatusOK, map[string]any{
			"items":       items,
			"total":       total,
			"exported_at": time.Now().UTC(),
		})
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="cadenza-audit-%s.csv"`, stamp))
	w.WriteHeader(http.StatusOK)
	// UTF-8 BOM：让 Excel 直接识别编码（否则中文列会乱码）。
	if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return
	}
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "created_at", "actor", "action", "subject", "detail"})
	for _, a := range items {
		_ = cw.Write([]string{
			strconv.FormatInt(a.ID, 10),
			a.CreatedAt.UTC().Format(time.RFC3339),
			a.Actor,
			string(a.Action),
			a.Subject,
			a.Detail,
		})
	}
	cw.Flush()
}

// auditExportSubject 把筛选条件摘要成审计 subject（便于事后查"这份导出对应什么范围"）。
func auditExportSubject(f store.AuditFilter) string {
	parts := make([]string, 0, 4)
	if f.Action != "" {
		parts = append(parts, "action="+string(f.Action))
	}
	if f.Actor != "" {
		parts = append(parts, "actor="+f.Actor)
	}
	if f.Subject != "" {
		parts = append(parts, "subject="+f.Subject)
	}
	if !f.From.IsZero() {
		parts = append(parts, "from="+f.From.UTC().Format(time.RFC3339))
	}
	if !f.To.IsZero() {
		parts = append(parts, "to="+f.To.UTC().Format(time.RFC3339))
	}
	if len(parts) == 0 {
		return "全部审计"
	}
	return strings.Join(parts, " ")
}
