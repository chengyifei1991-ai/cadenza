// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package store

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql" // MySQL driver
	_ "modernc.org/sqlite"             // 纯 Go SQLite driver（无 CGO）
)

// Store 是持久化存储的抽象接口，提供 SQLite 与 MySQL 两种实现，
// 通过 New 工厂按驱动名切换（设计方案 v3 第 6 节）。
type Store interface {
	// Close 关闭底层数据库连接。
	Close() error

	// UpsertCollector 插入或更新一个 Collector。
	UpsertCollector(ctx context.Context, c *Collector) error
	// GetCollector 按 instance_uid 查询 Collector，不存在返回 ErrNotFound。
	GetCollector(ctx context.Context, instanceUID string) (*Collector, error)
	// ListCollectors 分页返回 Collector 列表（按 instance_uid 升序）及总数。
	// page 从 1 开始；pageSize<=0 时返回全量（total 为全量条数）。
	ListCollectors(ctx context.Context, page, pageSize int) (items []Collector, total int64, err error)

	// UpsertGroup 插入或更新一个分组。
	UpsertGroup(ctx context.Context, g *CollectorGroup) error
	// GetGroup 按 ID 查询分组。
	GetGroup(ctx context.Context, id string) (*CollectorGroup, error)
	// ListGroups 返回全部分组。
	ListGroups(ctx context.Context) ([]CollectorGroup, error)

	// CreateConfigVersion 写入一个配置版本快照。
	CreateConfigVersion(ctx context.Context, v *ConfigVersion) error
	// ListConfigVersions 分页返回某个 Collector 的配置版本历史（id 降序）及总数。
	// page 从 1 开始；pageSize<=0 时返回全量。
	ListConfigVersions(ctx context.Context, instanceUID string, page, pageSize int) (items []ConfigVersion, total int64, err error)

	// CreateTask 创建任务并落库。
	CreateTask(ctx context.Context, t *Task) error
	// UpdateTask 更新任务（含状态迁移）。
	UpdateTask(ctx context.Context, t *Task) error
	// GetTask 按 ID 查询任务。
	GetTask(ctx context.Context, id string) (*Task, error)
	// ListTasks 按状态过滤分页返回任务列表（created_at 降序）及过滤后总数。
	// status 为空返回全部；page 从 1 开始；pageSize<=0 时返回全量。
	ListTasks(ctx context.Context, status TaskStatus, page, pageSize int) (items []Task, total int64, err error)

	// CreateSession 创建会话。
	CreateSession(ctx context.Context, s *ChatSession) error
	// GetSession 查询会话（含消息）。
	GetSession(ctx context.Context, id string) (*ChatSession, error)
	// AppendMessage 向会话追加一条消息。
	AppendMessage(ctx context.Context, sessionID string, m ChatMessage) error
	// ListSessions 分页返回会话列表（按最近消息倒序，空会话置底）及总数。
	// page 从 1 开始；pageSize<=0 时返回全量。
	ListSessions(ctx context.Context, page, pageSize int) (items []SessionSummary, total int64, err error)

	// Ping 探测数据库连通性（/healthz 使用）。
	Ping(ctx context.Context) error
	// CountSessions 返回会话总数。
	CountSessions(ctx context.Context) (int64, error)
	// CountCollectorsByStatus 按状态分组统计 Collector 数（仅含存在的状态）。
	CountCollectorsByStatus(ctx context.Context) (map[CollectorStatus]int64, error)
	// CountTasksByStatus 按状态分组统计任务数（仅含存在的状态）。
	CountTasksByStatus(ctx context.Context) (map[TaskStatus]int64, error)

	// CreateAgentRun 创建一次智能体运行。
	CreateAgentRun(ctx context.Context, r *AgentRun) error
	// UpdateAgentRun 更新运行状态。
	UpdateAgentRun(ctx context.Context, r *AgentRun) error

	// AppendAudit 写入一条审计记录。
	AppendAudit(ctx context.Context, a *AuditLog) error
	// ListAudit 按 since 过滤分页返回审计记录（id 降序）及过滤后总数。
	// since 为零值返回全部；page 从 1 开始；pageSize<=0 时返回全量。
	ListAudit(ctx context.Context, since int64, page, pageSize int) (items []AuditLog, total int64, err error)
}

// ErrNotFound 表示查询的记录不存在。
var ErrNotFound = fmt.Errorf("store: record not found")

// New 创建 Store。driver 支持 "mysql" 与 "sqlite"。
func New(driver, dsn string) (Store, error) {
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", driver, err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("store: ping %s: %w", driver, err)
	}
	s := &sqlStore{db: db, driver: driver}
	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: init schema: %w", err)
	}
	return s, nil
}
