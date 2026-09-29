// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package api

import (
	"net/http"
	"sort"
	"time"

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

// --- 变更效率指标（1.1.0-e）---

// statSummary 是一组时长的统计摘要（毫秒）。
type statSummary struct {
	Count int   `json:"count"`
	Avg   int64 `json:"avg"`
	P50   int64 `json:"p50"`
	P90   int64 `json:"p90"`
}

// opsTasksStats 是窗口内任务的终态分布与下发成功率。
type opsTasksStats struct {
	Total       int     `json:"total"`
	Done        int     `json:"done"`
	Failed      int     `json:"failed"`
	SuccessRate float64 `json:"success_rate"`
}

// opsRollbackStats 是回滚类任务的次数与平均时长。
type opsRollbackStats struct {
	Count         int   `json:"count"`
	AvgDurationMs int64 `json:"avg_duration_ms"`
}

// opsStatsResponse 是 /api/v1/stats/ops 的响应结构。
type opsStatsResponse struct {
	WindowDays     int              `json:"window_days"`
	Tasks          opsTasksStats    `json:"tasks"`
	ApprovalWaitMs statSummary      `json:"approval_wait_ms"`
	DispatchMs     statSummary      `json:"dispatch_ms"`
	Rollback       opsRollbackStats `json:"rollback"`
}

// StatsOps 处理 GET /api/v1/stats/ops?window_days=7：
// 基于任务状态迁移事件（task_events）计算变更效率指标——审批等待、下发时长、
// 下发成功率与回滚平均时长。窗口默认 7 天（1~90）。
func (h *Handlers) StatsOps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	windowDays, err := intParamInRange(r.URL.Query().Get("window_days"), 1, 90)
	if err != nil {
		writeError(w, http.StatusBadRequest, "window_days 必须在 1~90 之间")
		return
	}
	if windowDays == 0 {
		windowDays = 7
	}
	since := time.Now().UTC().AddDate(0, 0, -windowDays)
	events, err := h.store.ListTaskEvents(r.Context(), since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询任务事件失败")
		return
	}
	// 窗口内任务（用于终态分布与回滚归类）；任务量级受窗口约束。
	tasks, _, err := h.store.ListTasks(r.Context(), store.TaskFilter{Since: since}, 0, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询任务失败")
		return
	}
	writeJSON(w, http.StatusOK, computeOpsStats(windowDays, tasks, events))
}

// computeOpsStats 由任务与状态迁移事件计算效率指标（纯函数，便于表驱动测试）。
func computeOpsStats(windowDays int, tasks []store.Task, events []store.TaskEvent) opsStatsResponse {
	resp := opsStatsResponse{WindowDays: windowDays}
	resp.Tasks.Total = len(tasks)
	for _, t := range tasks {
		switch t.Status {
		case store.TaskStatusDone:
			resp.Tasks.Done++
		case store.TaskStatusFailed:
			resp.Tasks.Failed++
		}
	}
	if dispatched := resp.Tasks.Done + resp.Tasks.Failed; dispatched > 0 {
		resp.Tasks.SuccessRate = float64(resp.Tasks.Done) / float64(dispatched)
	}

	// 按任务聚合事件（ListTaskEvents 已按 id 升序，等价时间序）。
	type span struct {
		taskID      string
		first, last time.Time
		enterWait   time.Time
		leaveWait   time.Time
		enterApply  time.Time
		terminal    time.Time
	}
	spans := map[string]*span{}
	order := make([]*span, 0, 8)
	get := func(taskID string, at time.Time) *span {
		sp, ok := spans[taskID]
		if !ok {
			sp = &span{taskID: taskID, first: at}
			spans[taskID] = sp
			order = append(order, sp)
		}
		sp.last = at
		return sp
	}
	for _, e := range events {
		sp := get(e.TaskID, e.CreatedAt)
		if e.ToStatus == string(store.TaskStatusAwaitingApproval) && sp.enterWait.IsZero() {
			sp.enterWait = e.CreatedAt
		}
		if e.FromStatus == string(store.TaskStatusAwaitingApproval) && sp.leaveWait.IsZero() {
			sp.leaveWait = e.CreatedAt
		}
		if e.ToStatus == string(store.TaskStatusApplying) && sp.enterApply.IsZero() {
			sp.enterApply = e.CreatedAt
		}
		if e.ToStatus == string(store.TaskStatusDone) || e.ToStatus == string(store.TaskStatusFailed) {
			sp.terminal = e.CreatedAt
		}
	}
	var waitDurations, dispatchDurations, rollbackDurations []time.Duration
	rollbackTasks := map[string]bool{}
	for _, t := range tasks {
		if t.Type == store.TaskTypeRollback {
			rollbackTasks[t.ID] = true
		}
	}
	for _, sp := range order {
		if !sp.enterWait.IsZero() && !sp.leaveWait.IsZero() && sp.leaveWait.After(sp.enterWait) {
			waitDurations = append(waitDurations, sp.leaveWait.Sub(sp.enterWait))
		}
		// 下发时长：进入 applying → 终态；缺失 applying 事件时（历史/演示数据）
		// 回退用该任务首个事件时间作为起点，保证指标仍可用。
		start := sp.enterApply
		if start.IsZero() {
			start = sp.first
		}
		if !start.IsZero() && !sp.terminal.IsZero() && sp.terminal.After(start) {
			dispatchDurations = append(dispatchDurations, sp.terminal.Sub(start))
		}
		if rollbackTasks[sp.taskID] && !sp.first.IsZero() && !sp.terminal.IsZero() && sp.terminal.After(sp.first) {
			rollbackDurations = append(rollbackDurations, sp.terminal.Sub(sp.first))
		}
	}
	resp.ApprovalWaitMs = summarize(waitDurations)
	resp.DispatchMs = summarize(dispatchDurations)
	resp.Rollback.Count = len(rollbackDurations)
	if len(rollbackDurations) > 0 {
		var sum time.Duration
		for _, d := range rollbackDurations {
			sum += d
		}
		resp.Rollback.AvgDurationMs = (sum / time.Duration(len(rollbackDurations))).Milliseconds()
	}
	return resp
}

// summarize 计算时长的 count/avg/p50/p90（毫秒；最近秩法取分位）。
func summarize(durations []time.Duration) statSummary {
	out := statSummary{Count: len(durations)}
	if len(durations) == 0 {
		return out
	}
	sorted := make([]int64, 0, len(durations))
	var sum int64
	for _, d := range durations {
		ms := d.Milliseconds()
		sorted = append(sorted, ms)
		sum += ms
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	out.Avg = sum / int64(len(sorted))
	out.P50 = percentile(sorted, 50)
	out.P90 = percentile(sorted, 90)
	return out
}

// percentile 以最近秩法取分位（sorted 需升序）。
func percentile(sorted []int64, p int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := (p*len(sorted) + 99) / 100 // 向上取整
	if idx < 1 {
		idx = 1
	}
	if idx > len(sorted) {
		idx = len(sorted)
	}
	return sorted[idx-1]
}
