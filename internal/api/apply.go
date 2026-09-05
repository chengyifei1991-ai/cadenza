// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/store"
	"github.com/chengyifei1991-ai/cadenza/internal/ulid"
	"github.com/chengyifei1991-ai/cadenza/internal/validator"
)

// applyRequest 是 POST /api/v1/tasks/apply 的请求体（配置编辑器"保存并下发"）。
type applyRequest struct {
	CollectorInstanceUID string `json:"collector_instance_uid"`
	YAML                 string `json:"yaml"`
	// Note 是可选的变更说明（写入任务 Input，便于审计与前端展示）。
	Note string `json:"note,omitempty"`
}

// ApplyTask 处理 POST /api/v1/tasks/apply：
// 校验目标 Collector 存在 → 两级配置校验 → 创建 apply 任务。
// 全局审批开启时进入 awaiting_approval；关闭时立即触发下发（走完整状态机）。
func (h *Handlers) ApplyTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	var req applyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if req.CollectorInstanceUID == "" || req.YAML == "" {
		writeError(w, http.StatusBadRequest, "collector_instance_uid 与 yaml 均为必填")
		return
	}
	if _, err := h.store.GetCollector(r.Context(), req.CollectorInstanceUID); err != nil {
		writeError(w, http.StatusNotFound, "目标 Collector 不存在")
		return
	}
	res := validator.Validate(req.YAML, h.deps.Config.OtelcolBin, h.deps.Config.StrictValidate)
	if !res.Valid {
		writeError(w, http.StatusBadRequest, "配置校验未通过: "+strings.Join(res.Errors, "; "))
		return
	}

	input := req.Note
	if input == "" {
		input = fmt.Sprintf("直接下发配置到 %s", req.CollectorInstanceUID)
	}
	approver := approverOf(r, "")
	t := &store.Task{
		ID:                ulid.New(),
		Type:              store.TaskTypeApply,
		Status:            store.TaskStatusAwaitingApproval,
		RequireApproval:   h.deps.Config.RequireApproval,
		Input:             input,
		GeneratedYAML:     req.YAML,
		TargetInstanceUID: req.CollectorInstanceUID,
		TargetGroupID:     req.CollectorInstanceUID,
	}
	if err := h.tasks.Create(r.Context(), t); err != nil {
		writeError(w, http.StatusInternalServerError, "创建任务失败")
		return
	}
	// 变更提交即留痕；审批/拒绝与下发另有 audit 记录。
	h.auditApply(r.Context(), approver, t.ID, req.CollectorInstanceUID, req.YAML)

	if h.deps.Config.RequireApproval {
		writeJSON(w, http.StatusCreated, t)
		return
	}
	// 审批关闭：立即下发（复用 DispatchApprove 状态机，保证审计与版本快照一致）。
	if _, err := h.deps.DispatchApprove(r.Context(), t.ID, approver); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	updated, err := h.store.GetTask(r.Context(), t.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询任务状态失败")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// auditApply 记录"编辑器提交配置"审计（best effort）。
func (h *Handlers) auditApply(ctx context.Context, actor, taskID, instanceUID, yamlContent string) {
	_ = h.store.AppendAudit(ctx, &store.AuditLog{
		Actor:     actor,
		Action:    store.AuditActionApply,
		Subject:   taskID,
		Detail:    fmt.Sprintf("编辑器提交配置 uid=%s hash=%s", instanceUID, validator.Hash(yamlContent)),
		CreatedAt: time.Now().UTC(),
	})
}
