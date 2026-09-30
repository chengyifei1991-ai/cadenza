// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package task

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/store"
)

// stuckStatuses 是"服务端已出手、但还没等到终局"的中间态。
// 正常路径下它们会被生效确认或失败兜底推走；若因进程崩溃、写入失败或下游永不回报而卡住，
// 任务会永久停留在这里（1.0.1 的 F-15/F-18 就是这个形状），巡检负责收口。
var stuckStatuses = []store.TaskStatus{store.TaskStatusValidating, store.TaskStatusApplying}

// SweptTask 描述一次巡检收口的结果。
type SweptTask struct {
	// TaskID 是任务 id。
	TaskID string
	// From 是收口前的中间态。
	From store.TaskStatus
	// StuckFor 是在该中间态停留的时长。
	StuckFor time.Duration
	// Task 是收口后的任务快照（status=failed，error 写明巡检来源）。
	Task *store.Task
}

// SweepStuck 收口停留超过 timeout 的中间态任务：置 failed + 写明原因 + 记状态迁移事件 + 审计（actor=system）。
//
// 收口走**条件更新**（store.FailStuckTask）而非"读-改-写"：任务若在此期间已被并发流转
// （生效确认恰好到达、人工处理），条件不满足 → 本次不动、也不记事件，避免把终态覆盖成 failed。
func (s *Service) SweepStuck(ctx context.Context, timeout time.Duration, now time.Time) ([]SweptTask, error) {
	if timeout <= 0 {
		return nil, nil
	}
	now = now.UTC()
	cutoff := now.Add(-timeout)
	stuck, err := s.store.ListStuckTasks(ctx, stuckStatuses, cutoff)
	if err != nil {
		return nil, err
	}
	out := make([]SweptTask, 0, len(stuck))
	for i := range stuck {
		t := stuck[i]
		from := t.Status
		stuckFor := now.Sub(t.UpdatedAt)
		msg := fmt.Sprintf("任务超时未收到生效确认（中间态巡检收口：%s 停留超过 %s）", from, timeout)
		ok, err := s.store.FailStuckTask(ctx, t.ID, from, cutoff, msg)
		if err != nil {
			return out, fmt.Errorf("收口任务 %s: %w", t.ID, err)
		}
		if !ok {
			continue // 已被并发流转：不覆盖状态、不补事件
		}
		s.recordEvent(ctx, t.ID, string(from), string(store.TaskStatusFailed))
		// 审计留痕（actor=system）：事后能分辨"这是系统收口，不是人为失败"。
		_ = s.store.AppendAudit(ctx, &store.AuditLog{
			Actor:     "system",
			Action:    store.AuditActionSweep,
			Subject:   t.ID,
			Detail:    msg,
			CreatedAt: now,
		})
		t.Status = store.TaskStatusFailed
		t.Error = msg
		t.UpdatedAt = now
		swept := t
		out = append(out, SweptTask{TaskID: t.ID, From: from, StuckFor: stuckFor, Task: &swept})
	}
	return out, nil
}

// RunStuckSweeper 周期性执行中间态巡检，直到 ctx 取消。
// interval<=0 或 timeout<=0 表示关闭（默认 60s / 5m，见 TASK_SWEEP_INTERVAL / TASK_STUCK_TIMEOUT）。
func (s *Service) RunStuckSweeper(ctx context.Context, interval, timeout time.Duration, logger *slog.Logger) {
	if interval <= 0 || timeout <= 0 {
		if logger != nil {
			logger.Info("中间态超时巡检未启用", "interval", interval.String(), "timeout", timeout.String())
		}
		return
	}
	if logger != nil {
		logger.Info("中间态超时巡检已启用", "interval", interval.String(), "timeout", timeout.String())
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			swept, err := s.SweepStuck(ctx, timeout, time.Now())
			if err != nil {
				if logger != nil {
					logger.Error("中间态巡检失败", "error", err)
				}
				continue
			}
			for _, st := range swept {
				if logger != nil {
					logger.Warn("中间态任务超时收口",
						"task_id", st.TaskID, "from", string(st.From), "stuck_for", st.StuckFor.String())
				}
			}
		}
	}
}
