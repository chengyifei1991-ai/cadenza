// Package task 实现任务状态机（设计方案 v3 第 4 节）。
//
// 状态机：
//
//	pending → generating → validating → awaiting_approval → applying → done
//	                                         │                       │
//	                                         ├── rejected            └── (终态)
//	任何非终态 → failed（可重试：failed → generating）
//
// 审批：单审批人（Approver），Approvers 字段预留多人扩展。
package task

import (
	"context"
	"fmt"
	"time"

	"github.com/chengyifei1991-ai/opamp-backend/internal/store"
)

// Service 是任务生命周期服务，封装状态迁移与持久化。
type Service struct {
	store store.Store
}

// NewService 创建任务服务。
func NewService(st store.Store) *Service {
	return &Service{store: st}
}

// Create 创建任务并落库。
func (s *Service) Create(ctx context.Context, t *store.Task) error {
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now
	if t.Status == "" {
		t.Status = store.TaskStatusPending
	}
	return s.store.CreateTask(ctx, t)
}

// Approve 审批通过：仅 awaiting_approval 状态可审批，通过后进入 applying。
func (s *Service) Approve(ctx context.Context, id, approver string) (*store.Task, error) {
	t, err := s.store.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := Transition(t, store.TaskStatusApplying); err != nil {
		return nil, err
	}
	t.Approver = approver
	if err := s.store.UpdateTask(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// Reject 审批拒绝：仅 awaiting_approval 状态可拒绝。
func (s *Service) Reject(ctx context.Context, id, approver, reason string) (*store.Task, error) {
	t, err := s.store.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := Transition(t, store.TaskStatusRejected); err != nil {
		return nil, err
	}
	t.Approver = approver
	t.RejectReason = reason
	if err := s.store.UpdateTask(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// SetStatus 将任务迁移到指定状态（调用方负责触发对应副作用）。
func (s *Service) SetStatus(ctx context.Context, id string, to store.TaskStatus) (*store.Task, error) {
	t, err := s.store.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := Transition(t, to); err != nil {
		return nil, err
	}
	if err := s.store.UpdateTask(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// Transition 校验并执行一次状态迁移，非法迁移返回错误。
func Transition(t *store.Task, to store.TaskStatus) error {
	if !CanTransition(t.Status, to) {
		return fmt.Errorf("task: 非法状态迁移 %s → %s", t.Status, to)
	}
	t.Status = to
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// CanTransition 返回从 from 到 to 是否合法（纯函数，便于表驱动测试）。
func CanTransition(from, to store.TaskStatus) bool {
	switch to {
	case store.TaskStatusFailed:
		// 任何非终态都可失败；终态不可再迁移。
		return from != store.TaskStatusDone && from != store.TaskStatusRejected &&
			from != store.TaskStatusFailed
	case store.TaskStatusGenerating:
		return from == store.TaskStatusPending || from == store.TaskStatusFailed
	case store.TaskStatusValidating:
		return from == store.TaskStatusGenerating
	case store.TaskStatusAwaitingApproval:
		return from == store.TaskStatusValidating
	case store.TaskStatusApplying:
		return from == store.TaskStatusAwaitingApproval
	case store.TaskStatusDone:
		return from == store.TaskStatusApplying
	case store.TaskStatusRejected:
		return from == store.TaskStatusAwaitingApproval
	case store.TaskStatusPending:
		// 仅允许创建时直接写入 pending（通过 Create 处理），不支持迁移回 pending。
		return from == store.TaskStatusPending
	}
	return false
}
