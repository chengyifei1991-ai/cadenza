// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package api

import (
	"net/http"

	"github.com/chengyifei1991-ai/cadenza/internal/store"
)

// collectorsStats 是 Collector 健康分布聚合。
type collectorsStats struct {
	Total     int64 `json:"total"`
	Healthy   int64 `json:"healthy"`
	Unhealthy int64 `json:"unhealthy"`
	Offline   int64 `json:"offline"`
	Unknown   int64 `json:"unknown"`
}

// tasksStats 是任务按状态聚合。
type tasksStats struct {
	Total            int64 `json:"total"`
	Pending          int64 `json:"pending"`
	Generating       int64 `json:"generating"`
	Validating       int64 `json:"validating"`
	AwaitingApproval int64 `json:"awaiting_approval"`
	Applying         int64 `json:"applying"`
	Done             int64 `json:"done"`
	Rejected         int64 `json:"rejected"`
	Failed           int64 `json:"failed"`
}

// statsResponse 是 /api/v1/stats 的稳定响应结构（所有字段恒存在，便于前端消费）。
type statsResponse struct {
	Collectors    collectorsStats `json:"collectors"`
	Tasks         tasksStats      `json:"tasks"`
	SessionsTotal int64           `json:"sessions_total"`
}

// Stats 处理 GET /api/v1/stats：返回仪表盘所需的聚合计数。
func (h *Handlers) Stats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	byCollectorStatus, err := h.store.CountCollectorsByStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "统计 Collector 失败")
		return
	}
	byTaskStatus, err := h.store.CountTasksByStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "统计任务失败")
		return
	}
	sessions, err := h.store.CountSessions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "统计会话失败")
		return
	}
	resp := statsResponse{SessionsTotal: sessions}
	sum := func(m map[store.CollectorStatus]int64) int64 {
		var n int64
		for _, v := range m {
			n += v
		}
		return n
	}
	resp.Collectors = collectorsStats{
		Total:     sum(byCollectorStatus),
		Healthy:   byCollectorStatus[store.CollectorStatusHealthy],
		Unhealthy: byCollectorStatus[store.CollectorStatusUnhealthy],
		Offline:   byCollectorStatus[store.CollectorStatusOffline],
		Unknown:   byCollectorStatus[store.CollectorStatusUnknown],
	}
	taskSum := func(m map[store.TaskStatus]int64) int64 {
		var n int64
		for _, v := range m {
			n += v
		}
		return n
	}
	resp.Tasks = tasksStats{
		Total:            taskSum(byTaskStatus),
		Pending:          byTaskStatus[store.TaskStatusPending],
		Generating:       byTaskStatus[store.TaskStatusGenerating],
		Validating:       byTaskStatus[store.TaskStatusValidating],
		AwaitingApproval: byTaskStatus[store.TaskStatusAwaitingApproval],
		Applying:         byTaskStatus[store.TaskStatusApplying],
		Done:             byTaskStatus[store.TaskStatusDone],
		Rejected:         byTaskStatus[store.TaskStatusRejected],
		Failed:           byTaskStatus[store.TaskStatusFailed],
	}
	writeJSON(w, http.StatusOK, resp)
}
