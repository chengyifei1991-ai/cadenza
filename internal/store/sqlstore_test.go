package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// newTestStore 使用内存 SQLite 创建测试存储。
func newTestStore(t *testing.T) Store {
	t.Helper()
	st, err := New("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("New(sqlite) 失败: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestCollectorCRUD(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	now := time.Now().UTC()

	c := &Collector{
		InstanceUID: "uid-1", Hostname: "node-1", Version: "0.156.0",
		LastSeenAt: now, Status: CollectorStatusHealthy,
		EffectiveConfig: "receivers: {}", GroupID: "group-prod",
	}
	if err := st.UpsertCollector(ctx, c); err != nil {
		t.Fatalf("UpsertCollector 失败: %v", err)
	}
	got, err := st.GetCollector(ctx, "uid-1")
	if err != nil {
		t.Fatalf("GetCollector 失败: %v", err)
	}
	if got.Hostname != "node-1" || got.Status != CollectorStatusHealthy || got.Version != "0.156.0" {
		t.Errorf("GetCollector 结果不符: %+v", got)
	}
	// 更新（upsert）。
	c.Status = CollectorStatusOffline
	c.Hostname = "node-1-updated"
	if err := st.UpsertCollector(ctx, c); err != nil {
		t.Fatalf("UpsertCollector(update) 失败: %v", err)
	}
	got, _ = st.GetCollector(ctx, "uid-1")
	if got.Status != CollectorStatusOffline || got.Hostname != "node-1-updated" {
		t.Errorf("upsert 未生效: %+v", got)
	}
	// 不存在查询。
	if _, err := st.GetCollector(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetCollector(不存在) err = %v, want ErrNotFound", err)
	}
	// 列表。
	list, err := st.ListCollectors(ctx)
	if err != nil || len(list) != 1 {
		t.Errorf("ListCollectors = %v, err = %v", list, err)
	}
}

func TestGroupCRUD(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	g := &CollectorGroup{ID: "g1", Name: "生产", Selector: `{"env":"prod"}`}
	if err := st.UpsertGroup(ctx, g); err != nil {
		t.Fatalf("UpsertGroup 失败: %v", err)
	}
	got, err := st.GetGroup(ctx, "g1")
	if err != nil || got.Name != "生产" {
		t.Errorf("GetGroup = %+v, err = %v", got, err)
	}
}

func TestTaskCRUD(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	now := time.Now().UTC()
	tk := &Task{
		ID: "task-1", Type: TaskTypeGenerate, Status: TaskStatusPending,
		RequireApproval: true, Input: "加 tail sampling",
		Approvers: []string{"alice", "bob"}, CreatedAt: now, UpdatedAt: now,
	}
	if err := st.CreateTask(ctx, tk); err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}
	got, err := st.GetTask(ctx, "task-1")
	if err != nil {
		t.Fatalf("GetTask 失败: %v", err)
	}
	if len(got.Approvers) != 2 || got.Approvers[0] != "alice" {
		t.Errorf("Approvers 未正确序列化: %v", got.Approvers)
	}
	// 状态过滤查询。
	tk.Status = TaskStatusAwaitingApproval
	if err := st.UpdateTask(ctx, tk); err != nil {
		t.Fatalf("UpdateTask 失败: %v", err)
	}
	got, _ = st.GetTask(ctx, "task-1")
	if got.Status != TaskStatusAwaitingApproval {
		t.Errorf("UpdateTask 未生效: %q", got.Status)
	}
	pending, err := st.ListTasks(ctx, TaskStatusAwaitingApproval)
	if err != nil || len(pending) != 1 {
		t.Errorf("ListTasks(awaiting) = %v, err = %v", pending, err)
	}
	done, _ := st.ListTasks(ctx, TaskStatusDone)
	if len(done) != 0 {
		t.Errorf("ListTasks(done) 应为空: %v", done)
	}
}

func TestConfigVersionAndAudit(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	now := time.Now().UTC()

	v := &ConfigVersion{
		CollectorInstanceUID: "uid-1", YAML: "receivers: {}", Hash: "abc",
		Validated: true, CreatedAt: now,
	}
	if err := st.CreateConfigVersion(ctx, v); err != nil {
		t.Fatalf("CreateConfigVersion 失败: %v", err)
	}
	if v.ID == 0 {
		t.Errorf("CreateConfigVersion 应回填自增 ID")
	}
	versions, err := st.ListConfigVersions(ctx, "uid-1")
	if err != nil || len(versions) != 1 {
		t.Errorf("ListConfigVersions = %v, err = %v", versions, err)
	}

	if err := st.AppendAudit(ctx, &AuditLog{Actor: "alice", Action: AuditActionApply, Subject: "uid-1", Detail: "hash", CreatedAt: now}); err != nil {
		t.Fatalf("AppendAudit 失败: %v", err)
	}
	logs, err := st.ListAudit(ctx, 0)
	if err != nil || len(logs) != 1 {
		t.Errorf("ListAudit = %v, err = %v", logs, err)
	}
	// since 过滤。
	logs, _ = st.ListAudit(ctx, logs[0].ID)
	if len(logs) != 0 {
		t.Errorf("since 过滤后应为空: %v", logs)
	}
}

func TestSessionAndAgentRun(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	now := time.Now().UTC()

	sess := &ChatSession{ID: "sess-1", CreatedAt: now}
	if err := st.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession 失败: %v", err)
	}
	if err := st.AppendMessage(ctx, "sess-1", ChatMessage{Role: "user", Content: "你好", CreatedAt: now}); err != nil {
		t.Fatalf("AppendMessage 失败: %v", err)
	}
	if err := st.AppendMessage(ctx, "sess-1", ChatMessage{Role: "assistant", Content: "你好！", CreatedAt: now}); err != nil {
		t.Fatalf("AppendMessage 失败: %v", err)
	}
	got, err := st.GetSession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("GetSession 失败: %v", err)
	}
	if len(got.Messages) != 2 || got.Messages[0].Content != "你好" {
		t.Errorf("GetSession 消息不符: %+v", got.Messages)
	}

	run := &AgentRun{
		ID: "run-1", SessionID: "sess-1", AgentType: "config_generator",
		Status: RunStatusRunning, StartedAt: now,
		ToolCalls: []ToolCall{{Name: "generate_config", ArgumentsJSON: `{}`, ResultJSON: `{}`}},
	}
	if err := st.CreateAgentRun(ctx, run); err != nil {
		t.Fatalf("CreateAgentRun 失败: %v", err)
	}
	run.Status = RunStatusDone
	run.FinishedAt = now
	if err := st.UpdateAgentRun(ctx, run); err != nil {
		t.Fatalf("UpdateAgentRun 失败: %v", err)
	}
}

// TestListTasksPage 表驱动测试任务分页：总数、页内条数、过滤、越界页。
func TestListTasksPage(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	now := time.Now().UTC()
	// 插入 5 条任务：3 条 done，2 条 pending。
	for i := 0; i < 5; i++ {
		status := TaskStatusDone
		if i >= 3 {
			status = TaskStatusPending
		}
		tk := &Task{
			ID: "page-task-" + string(rune('a'+i)), Type: TaskTypeGenerate, Status: status,
			RequireApproval: true, CreatedAt: now, UpdatedAt: now,
		}
		if err := st.CreateTask(ctx, tk); err != nil {
			t.Fatalf("CreateTask(%d) 失败: %v", i, err)
		}
	}
	tests := []struct {
		name      string
		status    TaskStatus
		page      int
		pageSize  int
		wantTotal int64
		wantLen   int
	}{
		{"全部第 1 页", "", 1, 2, 5, 2},
		{"全部第 2 页", "", 2, 2, 5, 2},
		{"全部第 3 页（余 1 条）", "", 3, 2, 5, 1},
		{"越界页返回空", "", 9, 2, 5, 0},
		{"按 done 过滤", TaskStatusDone, 1, 10, 3, 3},
		{"按 pending 过滤", TaskStatusPending, 1, 10, 2, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, total, err := st.ListTasksPage(ctx, tt.status, tt.page, tt.pageSize)
			if err != nil {
				t.Fatalf("ListTasksPage 失败: %v", err)
			}
			if total != tt.wantTotal {
				t.Errorf("total = %d, want %d", total, tt.wantTotal)
			}
			if len(items) != tt.wantLen {
				t.Errorf("len(items) = %d, want %d", len(items), tt.wantLen)
			}
		})
	}
}

// TestListAuditPage 表驱动测试审计分页与 since 过滤。
func TestListAuditPage(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	now := time.Now().UTC()
	for i := 0; i < 4; i++ {
		if err := st.AppendAudit(ctx, &AuditLog{Actor: "tester", Action: AuditActionApply,
			Subject: "uid-x", Detail: "d", CreatedAt: now}); err != nil {
			t.Fatalf("AppendAudit(%d) 失败: %v", i, err)
		}
	}
	tests := []struct {
		name      string
		since     int64
		page      int
		pageSize  int
		wantTotal int64
		wantLen   int
	}{
		{"第 1 页", 0, 1, 3, 4, 3},
		{"第 2 页（余 1 条）", 0, 2, 3, 4, 1},
		{"since=2 后剩 2 条", 2, 1, 10, 2, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, total, err := st.ListAuditPage(ctx, tt.since, tt.page, tt.pageSize)
			if err != nil {
				t.Fatalf("ListAuditPage 失败: %v", err)
			}
			if total != tt.wantTotal {
				t.Errorf("total = %d, want %d", total, tt.wantTotal)
			}
			if len(items) != tt.wantLen {
				t.Errorf("len(items) = %d, want %d", len(items), tt.wantLen)
			}
		})
	}
}

// TestListCollectorsPage 验证 Collector 分页。
func TestListCollectorsPage(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		if err := st.UpsertCollector(ctx, &Collector{
			InstanceUID: "page-uid-" + string(rune('0'+i)), LastSeenAt: now,
			Status: CollectorStatusHealthy,
		}); err != nil {
			t.Fatalf("UpsertCollector(%d) 失败: %v", i, err)
		}
	}
	items, total, err := st.ListCollectorsPage(ctx, 1, 2)
	if err != nil {
		t.Fatalf("ListCollectorsPage 失败: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(items) != 2 {
		t.Errorf("len(items) = %d, want 2", len(items))
	}
}
