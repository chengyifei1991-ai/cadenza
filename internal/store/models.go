// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

// Package store 定义持久化数据模型与存储抽象接口。
//
// 数据模型与设计方案 v3 第 2 节保持一致：
// Collector / CollectorGroup / ConfigVersion / Task / ChatSession /
// AgentRun / AuditLog。
package store

import "time"

// CollectorStatus 表示 Collector 实例的在线健康状态。
type CollectorStatus string

const (
	// CollectorStatusHealthy 表示 Collector 最近上报且状态正常。
	CollectorStatusHealthy CollectorStatus = "healthy"
	// CollectorStatusUnhealthy 表示 Collector 最近上报但 health 异常。
	CollectorStatusUnhealthy CollectorStatus = "unhealthy"
	// CollectorStatusOffline 表示 Collector 超过阈值未上报。
	CollectorStatusOffline CollectorStatus = "offline"
	// CollectorStatusUnknown 表示尚未收到健康信号或无法判定（连接在线但 health 未上报）。
	CollectorStatusUnknown CollectorStatus = "unknown"
)

// Collector 代表一个已接入的 OTel Collector 实例（OpAMP Agent）。
type Collector struct {
	// InstanceUID 是 OpAMP 协议的 instance_uid，作为主键。
	InstanceUID string `json:"instance_uid"`
	// Hostname 是 Agent 上报的主机名。
	Hostname string `json:"hostname"`
	// Version 是 Collector 的版本号。
	Version string `json:"version"`
	// LastSeenAt 是最近一次收到该 Agent 上报的时间。
	LastSeenAt time.Time `json:"last_seen_at"`
	// Status 是派生出的健康状态。
	Status CollectorStatus `json:"status"`
	// EffectiveConfig 是 Collector 当前生效的配置内容。
	EffectiveConfig string `json:"effective_config"`
	// GroupID 是所属 CollectorGroup 的 ID，可为空。
	GroupID string `json:"group_id"`
}

// CollectorGroup 表示一个 Collector 分组，用于批量下发与选择器匹配。
type CollectorGroup struct {
	// ID 是分组唯一标识。
	ID string `json:"id"`
	// Name 是分组显示名称。
	Name string `json:"name"`
	// Selector 是 JSON 编码的标签选择器，例如 {"env":"prod"}。
	Selector string `json:"selector"`
}

// ConfigVersion 是一次配置版本的不可变快照。
type ConfigVersion struct {
	// ID 是自增主键。
	ID int64 `json:"id"`
	// CollectorInstanceUID 指向目标 Collector。
	CollectorInstanceUID string `json:"collector_instance_uid"`
	// YAML 是配置内容。
	YAML string `json:"yaml"`
	// Hash 是配置内容的 sha256 哈希，OpAMP 协议用于比对。
	Hash string `json:"hash"`
	// Validated 标记该版本是否通过了配置校验。
	Validated bool `json:"validated"`
	// CreatedAt 是版本创建时间。
	CreatedAt time.Time `json:"created_at"`
}

// TaskType 表示任务类型。
type TaskType string

const (
	// TaskTypeGenerate 表示对话生成配置任务。
	TaskTypeGenerate TaskType = "generate"
	// TaskTypeOptimize 表示自动优化配置任务。
	TaskTypeOptimize TaskType = "optimize"
	// TaskTypeApply 表示主动下发配置任务。
	TaskTypeApply TaskType = "apply"
	// TaskTypeUpgrade 表示 Collector 版本升级任务。
	TaskTypeUpgrade TaskType = "upgrade"
	// TaskTypeRollback 表示配置回滚任务（目标为某历史版本）。
	TaskTypeRollback TaskType = "rollback"
)

// TaskStatus 表示任务状态机的当前状态。
type TaskStatus string

const (
	// TaskStatusPending 表示任务已创建、尚未开始。
	TaskStatusPending TaskStatus = "pending"
	// TaskStatusGenerating 表示正在调用 LLM 生成配置。
	TaskStatusGenerating TaskStatus = "generating"
	// TaskStatusValidating 表示正在执行配置校验。
	TaskStatusValidating TaskStatus = "validating"
	// TaskStatusAwaitingApproval 表示等待审批。
	TaskStatusAwaitingApproval TaskStatus = "awaiting_approval"
	// TaskStatusApplying 表示正在通过 OpAMP 下发。
	TaskStatusApplying TaskStatus = "applying"
	// TaskStatusDone 表示任务成功完成。
	TaskStatusDone TaskStatus = "done"
	// TaskStatusRejected 表示任务被审批人拒绝。
	TaskStatusRejected TaskStatus = "rejected"
	// TaskStatusFailed 表示任务执行失败。
	TaskStatusFailed TaskStatus = "failed"
)

// Task 是配置生成/下发任务的执行单元，驱动完整状态机。
type Task struct {
	// ID 是任务唯一标识（ULID）。
	ID string `json:"id"`
	// Type 是任务类型。
	Type TaskType `json:"type"`
	// Status 是当前状态。
	Status TaskStatus `json:"status"`
	// RequireApproval 标记该任务下发前是否需要审批。
	RequireApproval bool `json:"require_approval"`
	// Input 是自然语言输入（generate/optimize 任务）。
	Input string `json:"input"`
	// GeneratedYAML 是生成/待下发的配置内容。
	GeneratedYAML string `json:"generated_yaml"`
	// SessionID 是发起该任务的 AI 会话 ID（会话内创建的任务）；
	// 为空表示非会话发起（如 REST/MCP 直调）。
	SessionID string `json:"session_id,omitempty"`
	// TargetGroupID 是目标分组 ID（generate/optimize 任务使用）。
	TargetGroupID string `json:"target_group_id"`
	// TargetInstanceUID 是目标 Collector 的 instance_uid（rollback/apply 任务使用）。
	TargetInstanceUID string `json:"target_instance_uid,omitempty"`
	// RollbackVersionID 是回滚目标版本 ID（非回滚任务为零值）。
	RollbackVersionID int64 `json:"rollback_version_id,omitempty"`
	// Approvers 是允许审批该任务的人员列表，预留扩展（当前为空表示任意审批人）。
	Approvers []string `json:"approvers,omitempty"`
	// Approver 是实际执行审批的人。
	Approver string `json:"approver,omitempty"`
	// RejectReason 是拒绝原因。
	RejectReason string `json:"reject_reason,omitempty"`
	// ModelUsed 是实际命中的 LLM 模型名（稳定性链路审计）。
	ModelUsed string `json:"model_used,omitempty"`
	// Error 记录任务失败原因。
	Error string `json:"error,omitempty"`
	// CreatedAt / UpdatedAt 记录任务时间线。
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ChatMessage 是会话中的一条消息。
type ChatMessage struct {
	// Role 是消息角色：user / assistant。
	Role string `json:"role"`
	// Content 是消息内容。
	Content string `json:"content"`
	// CreatedAt 是消息时间。
	CreatedAt time.Time `json:"created_at"`
}

// ChatSession 是一个对话会话，其消息作为 Agent 记忆。
type ChatSession struct {
	// ID 是会话唯一标识（ULID）。
	ID string `json:"id"`
	// Messages 是会话历史消息。
	Messages []ChatMessage `json:"messages"`
	// CreatedAt 是会话创建时间。
	CreatedAt time.Time `json:"created_at"`
}

// SessionSummary 是会话列表项：会话元信息 + 消息聚合摘要。
// 该结构不入库，查询时由 chat_messages 聚合派生（前端会话列表/标题预览用）。
type SessionSummary struct {
	// ID 是会话唯一标识（ULID）。
	ID string `json:"id"`
	// CreatedAt 是会话创建时间。
	CreatedAt time.Time `json:"created_at"`
	// MessageCount 是会话内的消息条数。
	MessageCount int `json:"message_count"`
	// LastMessageAt 是最后一条消息的时间；空会话为零值。
	LastMessageAt time.Time `json:"last_message_at,omitempty"`
	// FirstMessage 是会话首条用户消息（截断预览，用作列表标题）。
	FirstMessage string `json:"first_message,omitempty"`
	// LastMessage 是会话最后一条消息（截断预览）。
	LastMessage string `json:"last_message,omitempty"`
}

// RunStatus 表示一次智能体运行的终态。
type RunStatus string

const (
	// RunStatusRunning 表示运行中。
	RunStatusRunning RunStatus = "running"
	// RunStatusDone 表示运行成功完成。
	RunStatusDone RunStatus = "done"
	// RunStatusFailed 表示运行失败。
	RunStatusFailed RunStatus = "failed"
)

// ToolCall 记录一次工具调用（入参与出参），用于审计与可观测。
type ToolCall struct {
	// Name 是工具名。
	Name string `json:"name"`
	// ArgumentsJSON 是入参的 JSON 序列化。
	ArgumentsJSON string `json:"arguments_json"`
	// ResultJSON 是出参的 JSON 序列化。
	ResultJSON string `json:"result_json"`
	// Error 记录工具调用错误（为空表示成功）。
	Error string `json:"error,omitempty"`
	// StartedAt / FinishedAt 记录调用时间线。
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

// AgentRun 是一次智能体运行实例，挂载在会话下。
type AgentRun struct {
	// ID 是运行唯一标识（ULID）。
	ID string `json:"id"`
	// SessionID 是所属会话 ID。
	SessionID string `json:"session_id"`
	// AgentType 是智能体类型：config_generator / config_optimizer。
	AgentType string `json:"agent_type"`
	// ToolCalls 是该次运行的工具调用记录。
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// Status 是运行状态。
	Status RunStatus `json:"status"`
	// ModelUsed 记录实际命中的 LLM 模型名。
	ModelUsed string `json:"model_used,omitempty"`
	// StartedAt / FinishedAt 记录运行时间线。
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}

// AuditAction 表示审计动作类型。
type AuditAction string

const (
	// AuditActionGenerate 表示配置生成。
	AuditActionGenerate AuditAction = "generate"
	// AuditActionApprove 表示审批通过。
	AuditActionApprove AuditAction = "approve"
	// AuditActionReject 表示审批拒绝。
	AuditActionReject AuditAction = "reject"
	// AuditActionApply 表示配置下发。
	AuditActionApply AuditAction = "apply"
	// AuditActionUpgrade 表示版本升级。
	AuditActionUpgrade AuditAction = "upgrade"
	// AuditActionRollback 表示配置回滚。
	AuditActionRollback AuditAction = "rollback"
)

// AuditLog 是审计记录，所有配置变更必须留痕。
type AuditLog struct {
	// ID 是自增主键。
	ID int64 `json:"id"`
	// Actor 是操作者：user / agent / mcp。
	Actor string `json:"actor"`
	// Action 是动作类型。
	Action AuditAction `json:"action"`
	// Subject 是关联对象（TaskID 或 CollectorInstanceUID）。
	Subject string `json:"subject"`
	// Detail 是详细描述（如配置哈希、目标分组）。
	Detail string `json:"detail"`
	// CreatedAt 是记录时间。
	CreatedAt time.Time `json:"created_at"`
}
