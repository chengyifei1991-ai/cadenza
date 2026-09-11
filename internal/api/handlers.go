// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

// Package api 实现 REST API（Web 前端用）与主 HTTP 路由装配：
// /v1/opamp（OpAMP 协议）、/mcp（MCP Server）、/api/v1/*（REST）。
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/agent"
	"github.com/chengyifei1991-ai/cadenza/internal/store"
	"github.com/chengyifei1991-ai/cadenza/internal/task"
	"github.com/chengyifei1991-ai/cadenza/internal/validator"
)

// maxPageSize 是分页 page_size 的上限，防止一次拉取全表。
const maxPageSize = 100

// defaultPageSize 是分页 page_size 的默认值。
const defaultPageSize = 20

// pageResponse 是分页模式下的统一响应结构。
type pageResponse[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// pageParams 解析分页参数。
//
// 返回 (page, pageSize, enabled, err)：
//   - enabled=false 表示请求未携带任何分页参数（调用方应返回裸数组，保持向后兼容）；
//   - enabled=true 时 page/pageSize 为合法解析值，err 非 nil 表示参数非法（调用方返回 400）。
func pageParams(r *http.Request) (page, pageSize int, enabled bool, err error) {
	q := r.URL.Query()
	_, hasPage := q["page"]
	_, hasSize := q["page_size"]
	if !hasPage && !hasSize {
		return 0, 0, false, nil
	}
	page = 1
	if v := q.Get("page"); v != "" {
		n, perr := strconv.Atoi(v)
		if perr != nil || n < 1 {
			return 0, 0, true, fmt.Errorf("page 必须为正整数")
		}
		page = n
	}
	pageSize = defaultPageSize
	if v := q.Get("page_size"); v != "" {
		n, perr := strconv.Atoi(v)
		if perr != nil || n < 1 || n > maxPageSize {
			return 0, 0, true, fmt.Errorf("page_size 必须在 1~%d 之间", maxPageSize)
		}
		pageSize = n
	}
	return page, pageSize, true, nil
}

// Handlers 聚合 REST handler 依赖。
type Handlers struct {
	store  store.Store
	tasks  *task.Service
	orch   *agent.Orchestrator
	deps   *agent.Deps
	logger *slog.Logger
}

// NewHandlers 创建 REST handlers。
func NewHandlers(st store.Store, tasks *task.Service, orch *agent.Orchestrator, deps *agent.Deps, logger *slog.Logger) *Handlers {
	return &Handlers{store: st, tasks: tasks, orch: orch, deps: deps, logger: logger}
}

// --- 会话 ---

// CreateSessionRequest 是创建会话的请求体。
type CreateSessionRequest struct{}

// CreateSession 处理 POST /api/v1/sessions。
func (h *Handlers) CreateSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	sess := &store.ChatSession{ID: agent.NewULID(), CreatedAt: time.Now().UTC()}
	if err := h.store.CreateSession(r.Context(), sess); err != nil {
		writeError(w, http.StatusInternalServerError, "创建会话失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"session_id": sess.ID})
}

// ChatRequest 是对话请求体。
type ChatRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

// Chat 处理 POST /api/v1/chat。
func (h *Handlers) Chat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "message 不能为空")
		return
	}
	sess, err := h.resolveSession(r.Context(), req.SessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	// 追加用户消息。
	if err := h.store.AppendMessage(r.Context(), sess.ID, store.ChatMessage{Role: "user", Content: req.Message, CreatedAt: time.Now().UTC()}); err != nil {
		writeError(w, http.StatusInternalServerError, "保存消息失败")
		return
	}
	reply, err := h.orch.Chat(r.Context(), sess, req.Message)
	if err != nil {
		// LLM 链路故障：告知可重试（设计方案 v3：LLM_UNAVAILABLE 语义）。
		h.logger.Error("chat 调用失败", "session_id", sess.ID, "error", err)
		writeError(w, http.StatusServiceUnavailable, fmt.Sprintf("智能体调用失败（可重试）：%v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"session_id": sess.ID, "reply": reply})
}

// resolveSession 加载指定会话，ID 为空时创建新会话。
func (h *Handlers) resolveSession(ctx context.Context, sessionID string) (*store.ChatSession, error) {
	if sessionID == "" {
		sess := &store.ChatSession{ID: agent.NewULID(), CreatedAt: time.Now().UTC()}
		if err := h.store.CreateSession(ctx, sess); err != nil {
			return nil, fmt.Errorf("创建会话失败")
		}
		return sess, nil
	}
	sess, err := h.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("会话 %s 不存在", sessionID)
	}
	return sess, nil
}

// --- 任务 ---

// ListTasks 处理 GET /api/v1/tasks?status=&type=&target=&session_id=&since=&until=&page=&page_size=。
// 携带分页参数时返回 {items,total,page,page_size}；否则返回裸数组（向后兼容）。
func (h *Handlers) ListTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	filter, err := taskFilterFromQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, pageSize, enabled, err := pageParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if enabled {
		items, total, perr := h.store.ListTasks(r.Context(), filter, page, pageSize)
		if perr != nil {
			writeError(w, http.StatusInternalServerError, "查询任务失败")
			return
		}
		writeJSON(w, http.StatusOK, pageResponse[store.Task]{Items: items, Total: total, Page: page, PageSize: pageSize})
		return
	}
	tasks, _, err := h.store.ListTasks(r.Context(), filter, 0, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询任务失败")
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

// taskFilterFromQuery 解析任务列表过滤参数（全部可选，零值=不过滤）。
func taskFilterFromQuery(r *http.Request) (store.TaskFilter, error) {
	q := r.URL.Query()
	status := strings.TrimSpace(q.Get("status"))
	taskType := strings.TrimSpace(q.Get("type"))
	if !store.IsValidTaskStatus(status) {
		return store.TaskFilter{}, fmt.Errorf("status 取值非法（合法值：%s）", strings.Join(store.TaskStatuses(), "/"))
	}
	if !store.IsValidTaskType(taskType) {
		return store.TaskFilter{}, fmt.Errorf("type 取值非法（合法值：%s）", strings.Join(store.TaskTypes(), "/"))
	}
	f := store.TaskFilter{
		Status:    store.TaskStatus(status),
		Type:      store.TaskType(taskType),
		Target:    strings.TrimSpace(q.Get("target")),
		SessionID: strings.TrimSpace(q.Get("session_id")),
	}
	var err error
	if f.Since, err = parseTimeParam(q.Get("since")); err != nil {
		return f, fmt.Errorf("since 参数非法（需 RFC3339 或 Unix 秒）")
	}
	if f.Until, err = parseTimeParam(q.Get("until")); err != nil {
		return f, fmt.Errorf("until 参数非法（需 RFC3339 或 Unix 秒）")
	}
	return f, nil
}

// parseTimeParam 解析时间过滤参数：接受 RFC3339 或 Unix 秒（纯数字）；空串为零值。
func parseTimeParam(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, nil
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return time.Unix(n, 0).UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

// GetTask 处理 GET /api/v1/tasks/{id}。
func (h *Handlers) GetTask(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	t, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// ApproveRequest 是审批请求体。
type ApproveRequest struct {
	Approver string `json:"approver"`
}

// ApproveTask 处理 POST /api/v1/tasks/{id}/approve。
func (h *Handlers) ApproveTask(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	var req ApproveRequest
	approver := "user"
	if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.Approver != "" {
		approver = req.Approver
	}
	// Web 登录态下审批人绑定当前登录用户；off 模式退化为请求体/默认值。
	approver = approverOf(r, approver)
	result, err := h.deps.DispatchApprove(r.Context(), id, approver)
	if err != nil {
		// 任务不存在 → 404（避免泄漏英文内部错误，如 store: record not found）。
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "任务不存在")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// RejectRequest 是拒绝请求体。
type RejectRequest struct {
	Approver string `json:"approver"`
	Reason   string `json:"reason"`
}

// RejectTask 处理 POST /api/v1/tasks/{id}/reject。
func (h *Handlers) RejectTask(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	var req RejectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	// Web 登录态下审批人绑定当前登录用户；off 模式退化为请求体/默认值。
	approver := approverOf(r, req.Approver)
	t, err := h.tasks.Reject(r.Context(), id, approver, req.Reason)
	if err != nil {
		// 任务不存在 → 404（避免泄漏英文内部错误）。
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "任务不存在")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// --- Collector 与审计 ---

// GetCollectorJSON 处理 GET /api/v1/collectors/{uid}：返回单个 Collector 详情
// （含当前生效配置全文，配置编辑器/详情页使用）。
func (h *Handlers) GetCollectorJSON(w http.ResponseWriter, r *http.Request, uid string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	c, err := h.store.GetCollector(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusNotFound, "Collector 不存在")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// ListCollectors 处理 GET /api/v1/collectors?page=&page_size=。
// 携带分页参数时返回 {items,total,page,page_size}；否则返回裸数组（向后兼容）。
func (h *Handlers) ListCollectors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	page, pageSize, enabled, err := pageParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if enabled {
		items, total, perr := h.store.ListCollectors(r.Context(), page, pageSize)
		if perr != nil {
			writeError(w, http.StatusInternalServerError, "查询 Collector 失败")
			return
		}
		writeJSON(w, http.StatusOK, pageResponse[store.Collector]{Items: items, Total: total, Page: page, PageSize: pageSize})
		return
	}
	collectors, _, err := h.store.ListCollectors(r.Context(), 0, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询 Collector 失败")
		return
	}
	writeJSON(w, http.StatusOK, collectors)
}

// ListAudit 处理 GET /api/v1/audit?since=&page=&page_size=。
// 携带分页参数时返回 {items,total,page,page_size}；否则返回裸数组（向后兼容）。
func (h *Handlers) ListAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	var since int64
	fmt.Sscanf(r.URL.Query().Get("since"), "%d", &since)
	page, pageSize, enabled, err := pageParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if enabled {
		items, total, perr := h.store.ListAudit(r.Context(), since, page, pageSize)
		if perr != nil {
			writeError(w, http.StatusInternalServerError, "查询审计失败")
			return
		}
		writeJSON(w, http.StatusOK, pageResponse[store.AuditLog]{Items: items, Total: total, Page: page, PageSize: pageSize})
		return
	}
	logs, _, err := h.store.ListAudit(r.Context(), since, 0, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询审计失败")
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

// --- 回滚与版本历史 ---

// RollbackRequest 是回滚任务创建请求体。
type RollbackRequest struct {
	CollectorInstanceUID string `json:"collector_instance_uid"`
	VersionID            int64  `json:"version_id"`
	// SessionID 是发起该回滚的 AI 会话（可选；会话内发起时回传）。
	SessionID string `json:"session_id,omitempty"`
}

// RollbackTask 处理 POST /api/v1/tasks/rollback：
// 取目标版本 YAML → 两级校验 → 创建回滚任务（awaiting_approval）。
func (h *Handlers) RollbackTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	var req RollbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if req.CollectorInstanceUID == "" || req.VersionID <= 0 {
		writeError(w, http.StatusBadRequest, "collector_instance_uid 与 version_id 均为必填")
		return
	}
	versions, _, err := h.store.ListConfigVersions(r.Context(), req.CollectorInstanceUID, 0, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询版本历史失败")
		return
	}
	var target *store.ConfigVersion
	for i := range versions {
		if versions[i].ID == req.VersionID {
			target = &versions[i]
			break
		}
	}
	if target == nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("版本 %d 不存在或不属于该 Collector", req.VersionID))
		return
	}
	// 复用两级配置校验（yaml.v3 + otelcol-contrib v0.156.0）。
	res := validator.Validate(target.YAML, h.deps.Config.OtelcolBin, h.deps.Config.StrictValidate)
	if !res.Valid {
		writeError(w, http.StatusBadRequest, "回滚版本校验未通过: "+strings.Join(res.Errors, "; "))
		return
	}
	t := &store.Task{
		ID:                agent.NewULID(),
		Type:              store.TaskTypeRollback,
		Status:            store.TaskStatusAwaitingApproval,
		RequireApproval:   true,
		Input:             fmt.Sprintf("回滚 %s 到版本 %d", req.CollectorInstanceUID, req.VersionID),
		SessionID:         req.SessionID,
		TargetInstanceUID: req.CollectorInstanceUID,
		RollbackVersionID: req.VersionID,
	}
	if err := h.tasks.Create(r.Context(), t); err != nil {
		writeError(w, http.StatusInternalServerError, "创建回滚任务失败")
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// ListVersions 处理 GET /api/v1/collectors/{uid}/versions：
// 分页返回该 Collector 的版本历史；携带分页参数返回信封，否则裸数组。
func (h *Handlers) ListVersions(w http.ResponseWriter, r *http.Request, uid string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	page, pageSize, enabled, err := pageParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if enabled {
		items, total, perr := h.store.ListConfigVersions(r.Context(), uid, page, pageSize)
		if perr != nil {
			writeError(w, http.StatusInternalServerError, "查询版本历史失败")
			return
		}
		writeJSON(w, http.StatusOK, pageResponse[store.ConfigVersion]{Items: items, Total: total, Page: page, PageSize: pageSize})
		return
	}
	versions, _, err := h.store.ListConfigVersions(r.Context(), uid, 0, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询版本历史失败")
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

// --- helpers ---

// approverOf 决定任务操作者：优先 request context 中的登录用户（Web 鉴权
// simple 模式），其次请求体传入值，最后兜底 "user"（兼容 off 模式与既有调用方）。
func approverOf(r *http.Request, body string) string {
	if u := usernameFrom(r); u != "" {
		return u
	}
	if body != "" {
		return body
	}
	return "user"
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
