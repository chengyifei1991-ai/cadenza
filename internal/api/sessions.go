// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/chengyifei1991-ai/cadenza/internal/store"
)

// HandleSessions 分发 /api/v1/sessions：GET=列表（分页），POST=创建。
func (h *Handlers) HandleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.ListSessions(w, r)
	case http.MethodPost:
		h.CreateSession(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET/POST")
	}
}

// ListSessions 处理 GET /api/v1/sessions?page=&page_size=。
// 携带分页参数时返回 {items,total,page,page_size}；否则返回裸数组（向后兼容约定）。
func (h *Handlers) ListSessions(w http.ResponseWriter, r *http.Request) {
	page, pageSize, enabled, err := pageParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if enabled {
		items, total, perr := h.store.ListSessions(r.Context(), page, pageSize)
		if perr != nil {
			writeError(w, http.StatusInternalServerError, "查询会话失败")
			return
		}
		writeJSON(w, http.StatusOK, pageResponse[store.SessionSummary]{Items: items, Total: total, Page: page, PageSize: pageSize})
		return
	}
	items, _, perr := h.store.ListSessions(r.Context(), 0, 0)
	if perr != nil {
		writeError(w, http.StatusInternalServerError, "查询会话失败")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// ListSessionTasks 处理 GET /api/v1/sessions/{id}/tasks：
// 返回**该会话发起**的任务（任务↔会话硬绑定，替代前端文本正则联动）。
// 携带分页参数时返回 {items,total,page,page_size}；否则返回裸数组。
func (h *Handlers) ListSessionTasks(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	// 轻量存在性校验：避免为列表校验而全量加载该会话消息（长会话开销大）。
	exists, err := h.store.SessionExists(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询会话失败")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "会话不存在")
		return
	}
	page, pageSize, enabled, err := pageParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter := store.TaskFilter{SessionID: id}
	if enabled {
		items, total, perr := h.store.ListTasks(r.Context(), filter, page, pageSize)
		if perr != nil {
			writeError(w, http.StatusInternalServerError, "查询任务失败")
			return
		}
		writeJSON(w, http.StatusOK, pageResponse[store.Task]{Items: items, Total: total, Page: page, PageSize: pageSize})
		return
	}
	items, _, perr := h.store.ListTasks(r.Context(), filter, 0, 0)
	if perr != nil {
		writeError(w, http.StatusInternalServerError, "查询任务失败")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// ListSessionMessages 处理 GET /api/v1/sessions/{id}/messages?before_id=&after_id=&limit=：
// keyset 分页读取会话消息（长会话不必全量拉取）。返回 {items,total}；items 按 id 升序。
func (h *Handlers) ListSessionMessages(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	exists, err := h.store.SessionExists(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询会话失败")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "会话不存在")
		return
	}
	q := r.URL.Query()
	beforeID, err := nonNegativeInt64(q.Get("before_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "before_id 必须为非负整数")
		return
	}
	afterID, err := nonNegativeInt64(q.Get("after_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "after_id 必须为非负整数")
		return
	}
	limit, err := intParamInRange(q.Get("limit"), 1, maxPageSize)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("limit 必须在 1~%d 之间", maxPageSize))
		return
	}
	items, total, err := h.store.ListMessages(r.Context(), id, beforeID, afterID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询消息失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

// nonNegativeInt64 解析可选的非负整数参数（空串返回 0）。
func nonNegativeInt64(v string) (int64, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("非法整数: %s", v)
	}
	return n, nil
}

// intParamInRange 解析可选的区间整数参数（空串返回 0 = 由调用方/存储层取默认）。
func intParamInRange(v string, min, max int) (int, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min || n > max {
		return 0, fmt.Errorf("非法整数: %s", v)
	}
	return n, nil
}

// GetSessionByID 处理 GET /api/v1/sessions/{id}：返回会话（含全部消息）。
func (h *Handlers) GetSessionByID(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	sess, err := h.store.GetSession(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "会话不存在")
		return
	}
	writeJSON(w, http.StatusOK, sess)
}
