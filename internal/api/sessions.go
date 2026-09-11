// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package api

import (
	"net/http"

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
	if _, err := h.store.GetSession(r.Context(), id); err != nil {
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
