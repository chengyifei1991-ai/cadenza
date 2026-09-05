// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// sqlStore 是 Store 的统一 SQL 实现，SQLite 与 MySQL 共用同一套
// 语句（两者占位符均为 ?），差异仅在建表语句与驱动连接参数。
type sqlStore struct {
	db     *sql.DB
	driver string
}

// Close 实现 Store.Close。
func (s *sqlStore) Close() error {
	return s.db.Close()
}

// initSchema 按驱动创建全部数据表（幂等）。
func (s *sqlStore) initSchema() error {
	autoInc := "INTEGER PRIMARY KEY AUTOINCREMENT"
	if s.driver == "mysql" {
		autoInc = "BIGINT PRIMARY KEY AUTO_INCREMENT"
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS collectors (
			instance_uid TEXT PRIMARY KEY,
			hostname TEXT NOT NULL DEFAULT '',
			version TEXT NOT NULL DEFAULT '',
			last_seen_at TEXT NOT NULL,
			status TEXT NOT NULL,
			effective_config TEXT NOT NULL DEFAULT '',
			group_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS collector_groups (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			selector TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS config_versions (
			id ` + autoInc + `,
			collector_instance_uid TEXT NOT NULL,
			yaml TEXT NOT NULL,
			hash TEXT NOT NULL,
			validated INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			status TEXT NOT NULL,
			require_approval INTEGER NOT NULL DEFAULT 1,
			input TEXT NOT NULL DEFAULT '',
			generated_yaml TEXT NOT NULL DEFAULT '',
			target_group_id TEXT NOT NULL DEFAULT '',
			approvers TEXT NOT NULL DEFAULT '[]',
			approver TEXT NOT NULL DEFAULT '',
			reject_reason TEXT NOT NULL DEFAULT '',
			model_used TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status)`,
		`CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS chat_messages (
			id ` + autoInc + `,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_messages_session ON chat_messages(session_id)`,
		`CREATE TABLE IF NOT EXISTS agent_runs (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			agent_type TEXT NOT NULL,
			tool_calls TEXT NOT NULL DEFAULT '[]',
			status TEXT NOT NULL,
			model_used TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL,
			finished_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id ` + autoInc + `,
			actor TEXT NOT NULL,
			action TEXT NOT NULL,
			subject TEXT NOT NULL,
			detail TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
	}
	for _, stmt := range statements {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec schema: %w", err)
		}
	}
	// 幂等迁移：tasks 表新增回滚相关列（v1 配套）。
	if err := s.ensureColumn("tasks", "target_instance_uid", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("tasks", "rollback_version_id", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	return nil
}

// ensureColumn 检查列是否存在，不存在则 ALTER TABLE ADD COLUMN（SQLite/MySQL 兼容）。
func (s *sqlStore) ensureColumn(table, column, decl string) error {
	var exists bool
	if s.driver == "mysql" {
		var count int
		err := s.db.QueryRow(fmt.Sprintf(
			`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = '%s' AND column_name = '%s'`,
			table, column)).Scan(&count)
		if err != nil {
			return fmt.Errorf("check column %s.%s: %w", table, column, err)
		}
		exists = count > 0
	} else {
		rows, err := s.db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
		if err != nil {
			return fmt.Errorf("check column %s.%s: %w", table, column, err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				cid        int
				name, ctyp string
				notnull    int
				dflt       any
				pk         int
			)
			if err := rows.Scan(&cid, &name, &ctyp, &notnull, &dflt, &pk); err != nil {
				return fmt.Errorf("scan pragma: %w", err)
			}
			if name == column {
				exists = true
				break
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	if !exists {
		if _, err := s.db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, decl)); err != nil {
			return fmt.Errorf("add column %s.%s: %w", table, column, err)
		}
	}
	return nil
}

// timeFmt 是时间列的统一文本格式（RFC3339Nano，SQLite/MySQL 均兼容）。
const timeFmt = time.RFC3339Nano

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timeFmt)
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(timeFmt, s)
}

// scanRow 是行扫描的通用错误处理。
func mapNoRows(err error) error {
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	return err
}

// ---- Collectors ----

func (s *sqlStore) UpsertCollector(ctx context.Context, c *Collector) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO collectors (instance_uid, hostname, version, last_seen_at, status, effective_config, group_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instance_uid) DO UPDATE SET
			hostname=excluded.hostname, version=excluded.version, last_seen_at=excluded.last_seen_at,
			status=excluded.status, effective_config=excluded.effective_config, group_id=excluded.group_id`,
		c.InstanceUID, c.Hostname, c.Version, fmtTime(c.LastSeenAt), string(c.Status), c.EffectiveConfig, c.GroupID)
	return err
}

func (s *sqlStore) GetCollector(ctx context.Context, instanceUID string) (*Collector, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT instance_uid, hostname, version, last_seen_at, status, effective_config, group_id
		FROM collectors WHERE instance_uid = ?`, instanceUID)
	return scanCollector(row)
}

// ListCollectors 分页返回 Collector 列表（按 instance_uid 升序）及总数。
func (s *sqlStore) ListCollectors(ctx context.Context, page, pageSize int) ([]Collector, int64, error) {
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM collectors`).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `
		SELECT instance_uid, hostname, version, last_seen_at, status, effective_config, group_id
		FROM collectors ORDER BY instance_uid`
	var args []any
	if pageSize > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, pageSize, (page-1)*pageSize)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]Collector, 0)
	for rows.Next() {
		c, err := scanCollector(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *c)
	}
	return out, total, rows.Err()
}

// rowScanner 抽象了 *sql.Row 与 *sql.Rows，避免重复扫描代码。
type rowScanner interface {
	Scan(dest ...any) error
}

func scanCollector(sc rowScanner) (*Collector, error) {
	var (
		c        Collector
		lastSeen string
		status   string
	)
	if err := sc.Scan(&c.InstanceUID, &c.Hostname, &c.Version, &lastSeen, &status, &c.EffectiveConfig, &c.GroupID); err != nil {
		return nil, mapNoRows(err)
	}
	ts, err := parseTime(lastSeen)
	if err != nil {
		return nil, fmt.Errorf("parse last_seen_at: %w", err)
	}
	c.LastSeenAt = ts
	c.Status = CollectorStatus(status)
	return &c, nil
}

// ---- Groups ----

func (s *sqlStore) UpsertGroup(ctx context.Context, g *CollectorGroup) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO collector_groups (id, name, selector)
		VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, selector=excluded.selector`,
		g.ID, g.Name, g.Selector)
	return err
}

func (s *sqlStore) GetGroup(ctx context.Context, id string) (*CollectorGroup, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, selector FROM collector_groups WHERE id = ?`, id)
	var g CollectorGroup
	if err := row.Scan(&g.ID, &g.Name, &g.Selector); err != nil {
		return nil, mapNoRows(err)
	}
	return &g, nil
}

func (s *sqlStore) ListGroups(ctx context.Context) ([]CollectorGroup, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, selector FROM collector_groups ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CollectorGroup, 0)
	for rows.Next() {
		var g CollectorGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.Selector); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ---- ConfigVersions ----

func (s *sqlStore) CreateConfigVersion(ctx context.Context, v *ConfigVersion) error {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO config_versions (collector_instance_uid, yaml, hash, validated, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		v.CollectorInstanceUID, v.YAML, v.Hash, boolInt(v.Validated), fmtTime(v.CreatedAt))
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	v.ID = id
	return nil
}

// ListConfigVersions 分页返回某个 Collector 的配置版本历史（id 降序）及总数。
func (s *sqlStore) ListConfigVersions(ctx context.Context, instanceUID string, page, pageSize int) ([]ConfigVersion, int64, error) {
	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM config_versions WHERE collector_instance_uid = ?`, instanceUID).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `
		SELECT id, collector_instance_uid, yaml, hash, validated, created_at
		FROM config_versions WHERE collector_instance_uid = ? ORDER BY id DESC`
	var args []any
	args = append(args, instanceUID)
	if pageSize > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, pageSize, (page-1)*pageSize)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]ConfigVersion, 0)
	for rows.Next() {
		var (
			v       ConfigVersion
			created string
			valid   int
		)
		if err := rows.Scan(&v.ID, &v.CollectorInstanceUID, &v.YAML, &v.Hash, &valid, &created); err != nil {
			return nil, 0, err
		}
		ts, err := parseTime(created)
		if err != nil {
			return nil, 0, fmt.Errorf("parse created_at: %w", err)
		}
		v.Validated = valid != 0
		v.CreatedAt = ts
		out = append(out, v)
	}
	return out, total, rows.Err()
}

// ---- Tasks ----

func (s *sqlStore) CreateTask(ctx context.Context, t *Task) error {
	approvers, err := json.Marshal(t.Approvers)
	if err != nil {
		return fmt.Errorf("marshal approvers: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO tasks (id, type, status, require_approval, input, generated_yaml, target_group_id,
			target_instance_uid, rollback_version_id, approvers, approver, reject_reason, model_used, error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, string(t.Type), string(t.Status), boolInt(t.RequireApproval), t.Input, t.GeneratedYAML,
		t.TargetGroupID, t.TargetInstanceUID, t.RollbackVersionID, string(approvers), t.Approver, t.RejectReason,
		t.ModelUsed, t.Error, fmtTime(t.CreatedAt), fmtTime(t.UpdatedAt))
	return err
}

func (s *sqlStore) UpdateTask(ctx context.Context, t *Task) error {
	approvers, err := json.Marshal(t.Approvers)
	if err != nil {
		return fmt.Errorf("marshal approvers: %w", err)
	}
	t.UpdatedAt = time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `
		UPDATE tasks SET type=?, status=?, require_approval=?, input=?, generated_yaml=?, target_group_id=?,
			target_instance_uid=?, rollback_version_id=?, approvers=?, approver=?, reject_reason=?, model_used=?, error=?, updated_at=?
		WHERE id = ?`,
		string(t.Type), string(t.Status), boolInt(t.RequireApproval), t.Input, t.GeneratedYAML,
		t.TargetGroupID, t.TargetInstanceUID, t.RollbackVersionID, string(approvers), t.Approver,
		t.RejectReason, t.ModelUsed, t.Error, fmtTime(t.UpdatedAt), t.ID)
	return err
}

func (s *sqlStore) GetTask(ctx context.Context, id string) (*Task, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, status, require_approval, input, generated_yaml, target_group_id,
			target_instance_uid, rollback_version_id, approvers, approver, reject_reason, model_used, error, created_at, updated_at
		FROM tasks WHERE id = ?`, id)
	return scanTask(row)
}

// ListTasks 按状态过滤分页返回任务列表（created_at 降序）及过滤后总数。
func (s *sqlStore) ListTasks(ctx context.Context, status TaskStatus, page, pageSize int) ([]Task, int64, error) {
	where := ""
	var args []any
	if status != "" {
		where = " WHERE status = ?"
		args = append(args, string(status))
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `
		SELECT id, type, status, require_approval, input, generated_yaml, target_group_id,
			target_instance_uid, rollback_version_id, approvers, approver, reject_reason, model_used, error, created_at, updated_at
		FROM tasks` + where + ` ORDER BY created_at DESC`
	if pageSize > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, pageSize, (page-1)*pageSize)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *t)
	}
	return out, total, rows.Err()
}

func scanTask(sc rowScanner) (*Task, error) {
	var (
		t            Task
		approvers    string
		reqApproval  int
		created, upd string
	)
	if err := sc.Scan(&t.ID, &t.Type, &t.Status, &reqApproval, &t.Input, &t.GeneratedYAML,
		&t.TargetGroupID, &t.TargetInstanceUID, &t.RollbackVersionID, &approvers, &t.Approver,
		&t.RejectReason, &t.ModelUsed, &t.Error, &created, &upd); err != nil {
		return nil, mapNoRows(err)
	}
	if err := json.Unmarshal([]byte(approvers), &t.Approvers); err != nil {
		return nil, fmt.Errorf("unmarshal approvers: %w", err)
	}
	ct, err := parseTime(created)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	ut, err := parseTime(upd)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}
	t.RequireApproval = reqApproval != 0
	t.CreatedAt = ct
	t.UpdatedAt = ut
	return &t, nil
}

// ---- ChatSessions ----

func (s *sqlStore) CreateSession(ctx context.Context, sess *ChatSession) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO chat_sessions (id, created_at) VALUES (?, ?)`,
		sess.ID, fmtTime(sess.CreatedAt))
	return err
}

func (s *sqlStore) GetSession(ctx context.Context, id string) (*ChatSession, error) {
	var (
		sess    ChatSession
		created string
	)
	err := s.db.QueryRowContext(ctx, `SELECT id, created_at FROM chat_sessions WHERE id = ?`, id).
		Scan(&sess.ID, &created)
	if err != nil {
		return nil, mapNoRows(err)
	}
	ts, err := parseTime(created)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	sess.CreatedAt = ts
	rows, err := s.db.QueryContext(ctx,
		`SELECT role, content, created_at FROM chat_messages WHERE session_id = ? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			m       ChatMessage
			msgTime string
		)
		if err := rows.Scan(&m.Role, &m.Content, &msgTime); err != nil {
			return nil, err
		}
		t, err := parseTime(msgTime)
		if err != nil {
			return nil, fmt.Errorf("parse message time: %w", err)
		}
		m.CreatedAt = t
		sess.Messages = append(sess.Messages, m)
	}
	return &sess, rows.Err()
}

func (s *sqlStore) AppendMessage(ctx context.Context, sessionID string, m ChatMessage) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO chat_messages (session_id, role, content, created_at) VALUES (?, ?, ?, ?)`,
		sessionID, m.Role, m.Content, fmtTime(m.CreatedAt))
	return err
}

// listSessionQuery 是会话列表的查询骨架：每行附带消息聚合摘要（标量子查询），
// 按"最后一条消息 id"倒序（空会话置底），SQLite/MySQL 均兼容。
const listSessionQuery = `
	SELECT s.id, s.created_at,
		(SELECT COUNT(*) FROM chat_messages m0 WHERE m0.session_id = s.id) AS message_count,
		(SELECT m1.created_at FROM chat_messages m1 WHERE m1.session_id = s.id ORDER BY m1.id DESC LIMIT 1) AS last_at,
		(SELECT m2.content FROM chat_messages m2 WHERE m2.session_id = s.id AND m2.role = 'user' ORDER BY m2.id ASC LIMIT 1) AS first_msg,
		(SELECT m3.content FROM chat_messages m3 WHERE m3.session_id = s.id ORDER BY m3.id DESC LIMIT 1) AS last_msg
	FROM chat_sessions s
	ORDER BY COALESCE((SELECT MAX(m4.id) FROM chat_messages m4 WHERE m4.session_id = s.id), 0) DESC, s.created_at DESC`

// ListSessions 分页返回会话列表（按最近消息倒序，空会话置底）及总数。
func (s *sqlStore) ListSessions(ctx context.Context, page, pageSize int) ([]SessionSummary, int64, error) {
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_sessions`).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := listSessionQuery
	var args []any
	if pageSize > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, pageSize, (page-1)*pageSize)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]SessionSummary, 0)
	for rows.Next() {
		var (
			sum         SessionSummary
			created     string
			lastAt      sql.NullString
			first, last sql.NullString
			count       int64
		)
		if err := rows.Scan(&sum.ID, &created, &count, &lastAt, &first, &last); err != nil {
			return nil, 0, err
		}
		ct, err := parseTime(created)
		if err != nil {
			return nil, 0, fmt.Errorf("parse created_at: %w", err)
		}
		sum.CreatedAt = ct
		sum.MessageCount = int(count)
		if lastAt.Valid {
			lt, err := parseTime(lastAt.String)
			if err != nil {
				return nil, 0, fmt.Errorf("parse last message time: %w", err)
			}
			sum.LastMessageAt = lt
		}
		sum.FirstMessage = truncate(first.String)
		sum.LastMessage = truncate(last.String)
		out = append(out, sum)
	}
	return out, total, rows.Err()
}

// truncate 将消息内容截断为列表预览（含省略号总长 ≤80 字），空串保持不变。
func truncate(s string) string {
	const maxPreview = 80
	runes := []rune(s)
	if len(runes) <= maxPreview {
		return s
	}
	return string(runes[:maxPreview-1]) + "…"
}

// Ping 探测数据库连通性。
func (s *sqlStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// CountSessions 返回会话总数。
func (s *sqlStore) CountSessions(ctx context.Context) (int64, error) {
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_sessions`).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

// CountCollectorsByStatus 按状态分组统计 Collector 数。
func (s *sqlStore) CountCollectorsByStatus(ctx context.Context) (map[CollectorStatus]int64, error) {
	return countGrouped[CollectorStatus](ctx, s, "collectors", "status")
}

// CountTasksByStatus 按状态分组统计任务数。
func (s *sqlStore) CountTasksByStatus(ctx context.Context) (map[TaskStatus]int64, error) {
	return countGrouped[TaskStatus](ctx, s, "tasks", "status")
}

// countGrouped 执行 SELECT <column>, COUNT(*) FROM <table> GROUP BY <column>。
type groupKey interface {
	~string
}

func countGrouped[K groupKey](ctx context.Context, s *sqlStore, table, column string) (map[K]int64, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT %s, COUNT(*) FROM %s GROUP BY %s`, column, table, column))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[K]int64)
	for rows.Next() {
		var (
			k     string
			count int64
		)
		if err := rows.Scan(&k, &count); err != nil {
			return nil, err
		}
		out[K(k)] = count
	}
	return out, rows.Err()
}

// ---- AgentRuns ----

func (s *sqlStore) CreateAgentRun(ctx context.Context, r *AgentRun) error {
	toolCalls, err := json.Marshal(r.ToolCalls)
	if err != nil {
		return fmt.Errorf("marshal tool_calls: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO agent_runs (id, session_id, agent_type, tool_calls, status, model_used, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.SessionID, r.AgentType, string(toolCalls), string(r.Status), r.ModelUsed,
		fmtTime(r.StartedAt), fmtTime(r.FinishedAt))
	return err
}

func (s *sqlStore) UpdateAgentRun(ctx context.Context, r *AgentRun) error {
	toolCalls, err := json.Marshal(r.ToolCalls)
	if err != nil {
		return fmt.Errorf("marshal tool_calls: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE agent_runs SET session_id=?, agent_type=?, tool_calls=?, status=?, model_used=?, finished_at=?
		WHERE id = ?`,
		r.SessionID, r.AgentType, string(toolCalls), string(r.Status), r.ModelUsed,
		fmtTime(r.FinishedAt), r.ID)
	return err
}

// ---- Audit ----

func (s *sqlStore) AppendAudit(ctx context.Context, a *AuditLog) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_logs (actor, action, subject, detail, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		a.Actor, string(a.Action), a.Subject, a.Detail, fmtTime(a.CreatedAt))
	return err
}

// ListAudit 按 since 过滤分页返回审计记录（id 降序）及过滤后总数。
func (s *sqlStore) ListAudit(ctx context.Context, since int64, page, pageSize int) ([]AuditLog, int64, error) {
	where := ""
	var args []any
	if since > 0 {
		where = " WHERE id > ?"
		args = append(args, since)
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_logs`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `SELECT id, actor, action, subject, detail, created_at FROM audit_logs` + where + ` ORDER BY id DESC`
	if pageSize > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, pageSize, (page-1)*pageSize)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]AuditLog, 0)
	for rows.Next() {
		var (
			a       AuditLog
			created string
		)
		if err := rows.Scan(&a.ID, &a.Actor, &a.Action, &a.Subject, &a.Detail, &created); err != nil {
			return nil, 0, err
		}
		ts, err := parseTime(created)
		if err != nil {
			return nil, 0, fmt.Errorf("parse created_at: %w", err)
		}
		a.CreatedAt = ts
		out = append(out, a)
	}
	return out, total, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ensure strings import is used (kept for future SQL helpers).
var _ = strings.TrimSpace
