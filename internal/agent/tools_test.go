// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/config"
	"github.com/chengyifei1991-ai/cadenza/internal/opampserver"
	"github.com/chengyifei1991-ai/cadenza/internal/store"
	"github.com/chengyifei1991-ai/cadenza/internal/task"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// newTestDeps 构建集成测试所需的依赖（内存 SQLite + fake model + 未监听端口的 OpAMP 封装）。
func newTestDeps(t *testing.T, cfg *config.Config, fake *fakeModel) *Deps {
	t.Helper()
	st, err := store.New("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("store.New 失败: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	registry := opampserver.NewRegistry(st, time.Minute)
	opampSrv, err := opampserver.NewServer(slog.New(slog.NewTextHandler(nopWriter{}, nil)), st, registry, "")
	if err != nil {
		t.Fatalf("opampserver.NewServer 失败: %v", err)
	}
	return &Deps{
		Store:    st,
		Tasks:    task.NewService(st),
		OpAMP:    opampSrv,
		Registry: registry,
		Model:    fake,
		Config:   cfg,
		Logger:   slog.New(slog.NewTextHandler(nopWriter{}, nil)),
	}
}

// nopWriter 丢弃日志输出。
type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

const validGeneratedYAML = `receivers:
  otlp:
    protocols:
      grpc:
exporters:
  debug:
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [debug]
`

// TestHandleListCollectors 验证集群状态查询工具。
func TestHandleListCollectors(t *testing.T) {
	deps := newTestDeps(t, &config.Config{}, &fakeModel{})
	ctx := context.Background()
	now := time.Now().UTC()
	if err := deps.Store.UpsertCollector(ctx, &store.Collector{
		InstanceUID: "uid-1", Hostname: "node-1", Version: "0.156.0",
		LastSeenAt: now, Status: store.CollectorStatusHealthy, GroupID: "g-prod",
	}); err != nil {
		t.Fatalf("UpsertCollector 失败: %v", err)
	}
	tools := NewTools(deps)
	var target tool.CallableTool
	for _, tl := range tools {
		if tl.Declaration().Name == "list_collectors" {
			var ok bool
			target, ok = tl.(tool.CallableTool)
			if !ok {
				t.Fatal("list_collectors 未实现 CallableTool")
			}
		}
	}
	if target == nil {
		t.Fatal("找不到 list_collectors 工具")
	}
	result, err := target.Call(ctx, []byte(`{}`))
	if err != nil {
		t.Fatalf("list_collectors 失败: %v", err)
	}
	payload, _ := json.Marshal(result)
	if !strings.Contains(string(payload), "uid-1") || !strings.Contains(string(payload), "g-prod") {
		t.Errorf("list_collectors 结果缺失字段: %s", payload)
	}
}

// TestHandleGenerateConfigFlow 验证生成配置 → 待审批 → 审批下发的完整链路。
func TestHandleGenerateConfigFlow(t *testing.T) {
	fake := &fakeModel{seq: []callResult{{resp: okResponse("```yaml\n" + validGeneratedYAML + "\n```")}}}
	cfg := &config.Config{RequireApproval: true, OtelcolBin: ""}
	deps := newTestDeps(t, cfg, fake)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := deps.Store.UpsertCollector(ctx, &store.Collector{
		InstanceUID: "uid-1", Hostname: "node-1", LastSeenAt: now,
		Status: store.CollectorStatusHealthy, GroupID: "g-prod",
	}); err != nil {
		t.Fatalf("UpsertCollector 失败: %v", err)
	}

	// 1) 生成配置。
	result, err := deps.handleGenerateConfig(ctx, map[string]any{"target": "g-prod", "description": "加 tail sampling"})
	if err != nil {
		t.Fatalf("handleGenerateConfig 失败: %v", err)
	}
	r, _ := json.Marshal(result)
	var resp struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(r, &resp); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}
	if resp.Status != string(store.TaskStatusAwaitingApproval) {
		t.Fatalf("任务应处于待审批，got %q", resp.Status)
	}
	tk, err := deps.Store.GetTask(ctx, resp.TaskID)
	if err != nil {
		t.Fatalf("GetTask 失败: %v", err)
	}
	if tk.GeneratedYAML == "" {
		t.Errorf("任务应保存生成配置")
	}

	// 2) 审批下发。
	approveResult, err := deps.DispatchApprove(ctx, resp.TaskID, "alice")
	if err != nil {
		t.Fatalf("DispatchApprove 失败: %v", err)
	}
	ar, _ := json.Marshal(approveResult)
	if !strings.Contains(string(ar), "done") {
		t.Errorf("审批后任务应 done: %s", ar)
	}
	tk, _ = deps.Store.GetTask(ctx, resp.TaskID)
	if tk.Status != store.TaskStatusDone {
		t.Errorf("任务状态 = %q, want done", tk.Status)
	}
	// 审计留痕。
	logs, _, err := deps.Store.ListAudit(ctx, store.AuditFilter{}, 0, 0)
	if err != nil || len(logs) < 2 {
		t.Errorf("应有生成+审批+下发审计记录，got %d", len(logs))
	}
}

// TestHandleGenerateConfigInvalid 验证生成非法配置时任务失败。
func TestHandleGenerateConfigInvalid(t *testing.T) {
	fake := &fakeModel{
		seq: []callResult{
			{resp: okResponse("receivers: {}")}, // 缺 service 节，校验失败
			{resp: okResponse("receivers: {}")},
			{resp: okResponse("receivers: {}")},
		},
	}
	cfg := &config.Config{RequireApproval: true, OtelcolBin: ""}
	deps := newTestDeps(t, cfg, fake)
	_, err := deps.handleGenerateConfig(context.Background(), map[string]any{"target": "g-prod", "description": "生成"})
	if err == nil {
		t.Fatal("非法配置应返回错误")
	}
	if !strings.Contains(err.Error(), "校验") {
		t.Errorf("错误信息应包含校验失败原因: %v", err)
	}
}

// TestHandleApplyConfigRequiresApproval 验证审批开启时禁止直接下发。
func TestHandleApplyConfigRequiresApproval(t *testing.T) {
	deps := newTestDeps(t, &config.Config{RequireApproval: true}, &fakeModel{})
	_, err := deps.handleApplyConfig(context.Background(), map[string]any{"target": "g", "yaml": "x"})
	if err == nil {
		t.Fatal("审批开启时应拒绝直接下发")
	}
	if !strings.Contains(err.Error(), "审批") {
		t.Errorf("错误信息应提示审批: %v", err)
	}
}

// TestDispatchRollback 验证回滚任务审批后下发历史版本并写入新快照。
func TestDispatchRollback(t *testing.T) {
	deps := newTestDeps(t, &config.Config{RequireApproval: true, OtelcolBin: ""}, &fakeModel{})
	ctx := context.Background()
	now := time.Now().UTC()
	// 1) 注册 Collector + 写两个版本快照。
	if err := deps.Store.UpsertCollector(ctx, &store.Collector{
		InstanceUID: "rb-uid-1", Hostname: "node-rb", LastSeenAt: now,
		Status: store.CollectorStatusHealthy,
	}); err != nil {
		t.Fatalf("UpsertCollector 失败: %v", err)
	}
	v1 := &store.ConfigVersion{
		CollectorInstanceUID: "rb-uid-1",
		YAML:                 "receivers: {}\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      exporters: [debug]\n",
		Hash:                 "v1", Validated: true, CreatedAt: now,
	}
	if err := deps.Store.CreateConfigVersion(ctx, v1); err != nil {
		t.Fatalf("CreateConfigVersion v1 失败: %v", err)
	}
	v2 := &store.ConfigVersion{
		CollectorInstanceUID: "rb-uid-1", YAML: "exporters:\n  debug:\nservice:\n  pipelines:\n    traces:\n      exporters: [debug]\n",
		Hash: "v2", Validated: true, CreatedAt: now,
	}
	if err := deps.Store.CreateConfigVersion(ctx, v2); err != nil {
		t.Fatalf("CreateConfigVersion v2 失败: %v", err)
	}

	// 2) 创建回滚任务（仿照 RollbackTask 端点逻辑：target=v1）。
	rb := &store.Task{
		ID: NewULID(), Type: store.TaskTypeRollback,
		Status: store.TaskStatusAwaitingApproval, RequireApproval: true,
		Input: "回滚 rb-uid-1", TargetInstanceUID: "rb-uid-1", RollbackVersionID: v1.ID,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := deps.Tasks.Create(ctx, rb); err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}

	// 3) 审批下发。
	result, err := deps.DispatchApprove(ctx, rb.ID, "alice")
	if err != nil {
		t.Fatalf("DispatchApprove 失败: %v", err)
	}
	r, _ := json.Marshal(result)
	if !strings.Contains(string(r), "回滚") {
		t.Errorf("回滚结果应包含回滚提示: %s", r)
	}
	// 4) 任务 done + 审计 rollback。
	tk, _ := deps.Store.GetTask(ctx, rb.ID)
	if tk.Status != store.TaskStatusDone {
		t.Errorf("任务状态 = %q, want done", tk.Status)
	}
	audits, _, _ := deps.Store.ListAudit(ctx, store.AuditFilter{}, 0, 0)
	found := false
	for _, a := range audits {
		if a.Action == store.AuditActionRollback {
			found = true
		}
	}
	if !found {
		t.Errorf("应包含 rollback 审计记录")
	}
	// 5) 回滚本身写入新版本快照（v1 配置被记录为新版本）。
	versions, total, _ := deps.Store.ListConfigVersions(ctx, "rb-uid-1", 0, 0)
	if total != 3 {
		t.Errorf("回滚后应有 3 个版本快照，got %d", total)
	}
	// 6) 注册表生效配置同步为回滚版本。
	if c, ok := deps.Registry.Get("rb-uid-1"); ok {
		if !strings.Contains(c.EffectiveConfig, "receivers") {
			t.Errorf("生效配置应同步为回滚版本内容")
		}
	}
	_ = versions
}

// TestRecordConfigVersion 验证普通下发（apply）写入版本快照。
func TestRecordConfigVersion(t *testing.T) {
	deps := newTestDeps(t, &config.Config{RequireApproval: false, OtelcolBin: ""}, &fakeModel{})
	ctx := context.Background()
	now := time.Now().UTC()
	if err := deps.Store.UpsertCollector(ctx, &store.Collector{
		InstanceUID: "snap-uid-1", Hostname: "node-snap", LastSeenAt: now,
		Status: store.CollectorStatusHealthy,
	}); err != nil {
		t.Fatalf("UpsertCollector 失败: %v", err)
	}
	yaml := "receivers:\n  otlp:\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      exporters: [debug]\n"
	if _, err := deps.handleApplyConfig(ctx, map[string]any{"target": "snap-uid-1", "yaml": yaml}); err != nil {
		t.Fatalf("handleApplyConfig 失败: %v", err)
	}
	versions, total, _ := deps.Store.ListConfigVersions(ctx, "snap-uid-1", 0, 0)
	if total != 1 {
		t.Errorf("apply 后应有 1 个版本快照，got %d", total)
	}
	if versions[0].Validated != true || versions[0].YAML != yaml {
		t.Errorf("快照内容不符: %+v", versions[0])
	}
}
