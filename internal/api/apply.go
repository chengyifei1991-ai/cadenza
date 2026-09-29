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

	"github.com/chengyifei1991-ai/cadenza/internal/gitsource"
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
	// SessionID 是发起该变更的 AI 会话（可选；会话内发起时回传，实现任务↔会话绑定）。
	SessionID string `json:"session_id,omitempty"`
	// GitRef 是 GitOps 模式下的来源 ref（分支/tag/commit）；提供时忽略 YAML 字段，
	// 改为从 GIT_CONFIG_PATHSPEC 展开的文件读取内容（版本权威在 git）。
	GitRef string `json:"git_ref,omitempty"`
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
	if req.CollectorInstanceUID == "" {
		writeError(w, http.StatusBadRequest, "collector_instance_uid 为必填")
		return
	}
	// GitOps 模式：允许只给 git_ref，由服务端从仓库读取配置内容。
	gitCommit, gitPath := "", ""
	if req.GitRef != "" {
		repo, ok := h.gitRepo()
		if !ok {
			writeError(w, http.StatusConflict, gitDisabledMsg)
			return
		}
		path, err := gitsource.ExpandPathspec(h.deps.Config.GitConfigPathspec, req.CollectorInstanceUID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		content, err := repo.ShowFile(r.Context(), req.GitRef, path)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		sha, err := repo.Resolve(r.Context(), req.GitRef)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		req.YAML, gitCommit, gitPath = content, sha, path
	}
	if req.YAML == "" {
		writeError(w, http.StatusBadRequest, "yaml 必填（或提供 git_ref 从 git 读取）")
		return
	}
	target, err := h.store.GetCollector(r.Context(), req.CollectorInstanceUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "目标 Collector 不存在")
		return
	}
	baseYAML, baseSource := baseForCollector(r.Context(), target, h.deps.Registry)
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
		ID:              ulid.New(),
		Type:            store.TaskTypeApply,
		Status:          store.TaskStatusAwaitingApproval,
		RequireApproval: h.deps.Config.RequireApproval,
		Input:           input,
		SessionID:       req.SessionID,
		GeneratedYAML:   req.YAML,
		// 基准快照（F-9）：Agent 上报值优先、回退服务端记录，并标注来源，
		// 保证任务级 diff 跨时间可复现且语义明确。
		BaseYAML:   baseYAML,
		BaseSource: baseSource,
		// GitOps 溯源（内置模式下为空）。
		GitCommit:         gitCommit,
		GitPath:           gitPath,
		GitRef:            req.GitRef,
		TargetInstanceUID: req.CollectorInstanceUID,
		TargetGroupID:     req.CollectorInstanceUID,
	}
	if err := h.tasks.Create(r.Context(), t); err != nil {
		writeError(w, http.StatusInternalServerError, "创建任务失败")
		return
	}
	// 变更提交即留痕；审批/拒绝与下发另有 audit 记录。
	h.auditApply(r.Context(), approver, t.ID, req.CollectorInstanceUID, req.YAML)
	if gitCommit != "" {
		_ = h.store.AppendAudit(r.Context(), &store.AuditLog{
			Actor: approver, Action: store.AuditActionApply, Subject: t.ID,
			Detail:    "git 溯源 commit=" + gitCommit + " path=" + gitPath + " ref=" + req.GitRef,
			CreatedAt: time.Now().UTC(),
		})
	}

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
