// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

// Package demo 提供开箱即用的演示数据（DEMO_MODE=true 时注入空库），
// 覆盖 Collector 健康分布 / 分组 / 版本历史 / 任务状态机 / 审计 / 会话，
// 便于新用户在不接入真实 Collector 的情况下完整体验产品流程。
package demo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/store"
	"github.com/chengyifei1991-ai/cadenza/internal/ulid"
)

// hashOf 计算配置内容 sha256 摘要（与 validator.Hash 同算法，避免引入额外依赖）。
func hashOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// demoCollectorVersion 是演示 Collector 的 opentelemetry-collector-contrib 版本。
const demoCollectorVersion = "0.156.0"

// yamlV1 是最简演示配置（结构完整，可通过第一级校验）。
const yamlV1 = `receivers:
  otlp:
    protocols:
      grpc:
      http:
exporters:
  debug:
    verbosity: basic
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [debug]
    metrics:
      receivers: [otlp]
      exporters: [debug]
`

// yamlV2 是 yamlV1 的优化版（新增 memory_limiter，演示 diff 视图）。
const yamlV2 = `receivers:
  otlp:
    protocols:
      grpc:
      http:
processors:
  memory_limiter:
    check_interval: 1s
    limit_mib: 512
exporters:
  debug:
    verbosity: basic
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter]
      exporters: [debug]
    metrics:
      receivers: [otlp]
      processors: [memory_limiter]
      exporters: [debug]
`

// Seed 向空库注入演示数据。仅当 collectors 表为空时执行（避免重复注入）；
// 返回注入的概要信息，便于启动日志与运维确认。
func Seed(ctx context.Context, st store.Store, logger *slog.Logger) error {
	existing, _, err := st.ListCollectors(ctx, 0, 0)
	if err != nil {
		return fmt.Errorf("demo: 检查 Collector 失败: %w", err)
	}
	if len(existing) > 0 {
		logger.Info("DEMO_MODE=true 但库中已有数据，跳过演示数据注入", "collectors", len(existing))
		return nil
	}

	now := time.Now().UTC()
	uid := func(s string) string { return fmt.Sprintf("demo-%s", s) }
	groupID := "grp-demo"
	host := func(n string) string { return fmt.Sprintf("demo-%s", n) }

	collectors := []store.Collector{
		{InstanceUID: uid("gateway-1"), Hostname: host("gateway-1"), Version: demoCollectorVersion,
			LastSeenAt: now.Add(-30 * time.Second), Status: store.CollectorStatusHealthy,
			EffectiveConfig: yamlV2, GroupID: groupID},
		{InstanceUID: uid("api-1"), Hostname: host("api-1"), Version: demoCollectorVersion,
			LastSeenAt: now.Add(-45 * time.Second), Status: store.CollectorStatusHealthy,
			EffectiveConfig: yamlV1, GroupID: groupID},
		{InstanceUID: uid("worker-1"), Hostname: host("worker-1"), Version: demoCollectorVersion,
			LastSeenAt: now.Add(-10 * time.Second), Status: store.CollectorStatusUnhealthy,
			EffectiveConfig: yamlV1},
		{InstanceUID: uid("legacy-1"), Hostname: host("legacy-1"), Version: "0.120.0",
			LastSeenAt: now.Add(-3 * time.Hour), Status: store.CollectorStatusOffline,
			EffectiveConfig: yamlV1},
		{InstanceUID: uid("agent-1"), Hostname: host("agent-1"), Version: demoCollectorVersion,
			LastSeenAt: now.Add(-5 * time.Second), Status: store.CollectorStatusUnknown,
			EffectiveConfig: ""},
	}
	for _, c := range collectors {
		if err := st.UpsertCollector(ctx, &c); err != nil {
			return fmt.Errorf("demo: 注入 Collector %s 失败: %w", c.InstanceUID, err)
		}
	}
	if err := st.UpsertGroup(ctx, &store.CollectorGroup{
		ID: groupID, Name: "演示分组（网关/API）", Selector: `{"env":"demo"}`,
	}); err != nil {
		return fmt.Errorf("demo: 注入分组失败: %w", err)
	}

	// 版本历史：demo-gateway-1 具备两个可对比/回滚的版本。
	gwUID := uid("gateway-1")
	versions := []store.ConfigVersion{
		{CollectorInstanceUID: gwUID, YAML: yamlV1, Hash: hashOf(yamlV1), Validated: true, CreatedAt: now.Add(-2 * time.Hour)},
		{CollectorInstanceUID: gwUID, YAML: yamlV2, Hash: hashOf(yamlV2), Validated: true, CreatedAt: now.Add(-30 * time.Minute)},
	}
	for _, v := range versions {
		if err := st.CreateConfigVersion(ctx, &v); err != nil {
			return fmt.Errorf("demo: 注入版本失败: %w", err)
		}
	}

	// 演示会话先建，便于把"会话内创建的任务"绑定到它（1.1.0 任务↔会话绑定演示）。
	sess := &store.ChatSession{ID: ulid.New(), CreatedAt: now.Add(-30 * time.Minute)}
	if err := st.CreateSession(ctx, sess); err != nil {
		return fmt.Errorf("demo: 注入会话失败: %w", err)
	}

	// 任务：覆盖"待审批 / 已完成 / 已拒绝"三种典型状态。
	apiUID := uid("api-1")
	tasks := []store.Task{
		{ID: ulid.New(), Type: store.TaskTypeGenerate, Status: store.TaskStatusAwaitingApproval,
			RequireApproval: true, Input: "为 demo-gateway-1 增加 memory_limiter（演示，等待审批）",
			GeneratedYAML: yamlV2, TargetGroupID: gwUID, ModelUsed: "demo-model", SessionID: sess.ID,
			CreatedAt: now.Add(-1 * time.Hour), UpdatedAt: now.Add(-55 * time.Minute)},
		{ID: ulid.New(), Type: store.TaskTypeApply, Status: store.TaskStatusDone,
			RequireApproval: true, Input: "直接下发基础配置到 demo-gateway-1",
			GeneratedYAML: yamlV1, TargetGroupID: gwUID, TargetInstanceUID: gwUID,
			Approver: "admin", CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour)},
		{ID: ulid.New(), Type: store.TaskTypeGenerate, Status: store.TaskStatusRejected,
			RequireApproval: true, Input: "为 demo-api-1 启用 tail sampling（演示，被拒绝）",
			GeneratedYAML: yamlV2, TargetGroupID: apiUID, Approver: "admin", SessionID: sess.ID,
			RejectReason: "当前环境无 tail_sampling 需求，先不加", ModelUsed: "demo-model",
			CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-100 * time.Minute)},
	}
	for _, t := range tasks {
		if err := st.CreateTask(ctx, &t); err != nil {
			return fmt.Errorf("demo: 注入任务失败: %w", err)
		}
	}

	// 审计：动作覆盖生成/审批/下发/拒绝。
	audit := []store.AuditLog{
		{Actor: "admin", Action: store.AuditActionApply, Subject: gwUID,
			Detail: fmt.Sprintf("下发配置 hash=%s", hashOf(yamlV1)), CreatedAt: now.Add(-3 * time.Hour)},
		{Actor: "admin", Action: store.AuditActionGenerate, Subject: apiUID,
			Detail: "生成配置 target=" + apiUID, CreatedAt: now.Add(-2 * time.Hour)},
		{Actor: "admin", Action: store.AuditActionReject, Subject: tasks[2].ID,
			Detail: "当前环境无 tail_sampling 需求，先不加", CreatedAt: now.Add(-100 * time.Minute)},
		{Actor: "agent", Action: store.AuditActionGenerate, Subject: tasks[0].ID,
			Detail: fmt.Sprintf("生成配置 target=%s", gwUID), CreatedAt: now.Add(-55 * time.Minute)},
	}
	for _, a := range audit {
		if err := st.AppendAudit(ctx, &a); err != nil {
			return fmt.Errorf("demo: 注入审计失败: %w", err)
		}
	}

	// 会话（已在上方创建）：继续注入演示消息，供 AI 助手历史回读。
	messages := []store.ChatMessage{
		{Role: "user", Content: "帮我给 demo-gateway-1 增加内存限制，避免 OOM", CreatedAt: now.Add(-30 * time.Minute)},
		{Role: "assistant", Content: "已生成带 memory_limiter 的配置并通过校验，已创建任务等待审批（演示数据）。",
			CreatedAt: now.Add(-29 * time.Minute)},
	}
	for _, m := range messages {
		if err := st.AppendMessage(ctx, sess.ID, m); err != nil {
			return fmt.Errorf("demo: 注入会话消息失败: %w", err)
		}
	}

	logger.Info("演示数据已注入（DEMO_MODE=true）",
		"collectors", len(collectors), "tasks", len(tasks), "sessions", 1, "group", groupID)
	return nil
}
