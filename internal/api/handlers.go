// Package api 实现 REST API（Web 前端用）与主 HTTP 路由装配：
// /v1/opamp（OpAMP 协议）、/mcp（MCP Server）、/api/v1/*（REST）。
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/chengyifei1991-ai/opamp-backend/internal/agent"
	"github.com/chengyifei1991-ai/opamp-backend/internal/store"
	"github.com/chengyifei1991-ai/opamp-backend/internal/task"
)

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

// ListTasks 处理 GET /api/v1/tasks?status=。
func (h *Handlers) ListTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	status := store.TaskStatus(r.URL.Query().Get("status"))
	tasks, err := h.store.ListTasks(r.Context(), status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询任务失败")
		return
	}
	writeJSON(w, http.StatusOK, tasks)
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
	result, err := h.deps.DispatchApprove(r.Context(), id, approver)
	if err != nil {
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
	approver := req.Approver
	if approver == "" {
		approver = "user"
	}
	t, err := h.tasks.Reject(r.Context(), id, approver, req.Reason)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// --- Collector 与审计 ---

// ListCollectors 处理 GET /api/v1/collectors。
func (h *Handlers) ListCollectors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	collectors, err := h.store.ListCollectors(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询 Collector 失败")
		return
	}
	writeJSON(w, http.StatusOK, collectors)
}

// ListAudit 处理 GET /api/v1/audit?since=。
func (h *Handlers) ListAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	var since int64
	fmt.Sscanf(r.URL.Query().Get("since"), "%d", &since)
	logs, err := h.store.ListAudit(r.Context(), since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询审计失败")
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
