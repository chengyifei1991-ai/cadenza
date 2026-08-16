package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/chengyifei1991-ai/opamp-backend/internal/config"
	"github.com/chengyifei1991-ai/opamp-backend/internal/opampserver"
	"github.com/chengyifei1991-ai/opamp-backend/internal/store"
	"github.com/chengyifei1991-ai/opamp-backend/internal/task"
	"github.com/chengyifei1991-ai/opamp-backend/internal/validator"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// Deps 聚合工具集依赖，供 MCP Server 与内置对话 Agent 共用同一套实现。
type Deps struct {
	Store store.Store
	Tasks *task.Service
	OpAMP *opampserver.Server
	// Registry 用于下发成功后同步 Collector 生效配置（可为 nil）。
	Registry *opampserver.Registry
	Model    model.Model
	Config   *config.Config
	Logger   *slog.Logger
	// Now 可注入以便测试；nil 时使用 time.Now。
	Now func() time.Time
}

func (d *Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now().UTC()
}

func (d *Deps) log() *slog.Logger {
	if d.Logger != nil {
		return d.Logger
	}
	return slog.Default()
}

// simpleTool 是工具的统一实现骨架。
type simpleTool struct {
	decl   *tool.Declaration
	handle func(ctx context.Context, args map[string]any) (any, error)
}

// Declaration 实现 tool.Tool。
func (t *simpleTool) Declaration() *tool.Declaration { return t.decl }

// Call 实现 tool.CallableTool。
func (t *simpleTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	args := map[string]any{}
	if len(jsonArgs) > 0 && string(jsonArgs) != "null" {
		if err := json.Unmarshal(jsonArgs, &args); err != nil {
			return nil, fmt.Errorf("工具参数解析失败: %w", err)
		}
	}
	return t.handle(ctx, args)
}

// NewTools 构建全部管控工具（MCP 与内置 Agent 共用）。
func NewTools(d *Deps) []tool.Tool {
	str := func(desc string, required ...string) *tool.Schema {
		return &tool.Schema{Type: "object", Description: desc, Properties: map[string]*tool.Schema{}, Required: required}
	}
	addProp := func(s *tool.Schema, name, typ, desc string) {
		s.Properties[name] = &tool.Schema{Type: typ, Description: desc}
	}

	listSchema := str("查询 Collector 集群状态（可按分组过滤）", "target")
	listSchema.Required = nil
	addProp(listSchema, "group", "string", "可选分组 ID")

	getCfgSchema := str("获取指定 Collector 的当前生效配置", "instance_uid")
	addProp(getCfgSchema, "instance_uid", "string", "Collector 的 instance_uid")

	genSchema := str("根据自然语言描述生成 Collector 配置（YAML），生成后进入审批或直接下发", "target", "description")
	addProp(genSchema, "target", "string", "目标：分组 ID 或 Collector instance_uid")
	addProp(genSchema, "description", "string", "配置需求的自然语言描述")

	optSchema := str("基于 Collector 上报状态自动分析并提议配置优化方案", "instance_uid")
	addProp(optSchema, "instance_uid", "string", "Collector 的 instance_uid")
	addProp(optSchema, "goal", "string", "可选的优化目标，如降低内存占用")

	applySchema := str("直接下发配置（仅当全局审批开关关闭时允许）", "target", "yaml")
	addProp(applySchema, "target", "string", "目标：分组 ID 或 Collector instance_uid")
	addProp(applySchema, "yaml", "string", "要下发的 YAML 配置内容")

	upgradeSchema := str("（可选能力）为 Collector 创建版本升级任务", "target", "version")
	addProp(upgradeSchema, "target", "string", "目标：分组 ID 或 Collector instance_uid")
	addProp(upgradeSchema, "version", "string", "目标版本号，如 0.156.0")

	approveSchema := str("审批通过一个待审批任务并触发配置下发", "task_id")
	addProp(approveSchema, "task_id", "string", "任务 ID")
	addProp(approveSchema, "approver", "string", "审批人标识（可选，默认 user）")

	rejectSchema := str("拒绝一个待审批任务", "task_id")
	addProp(rejectSchema, "task_id", "string", "任务 ID")
	addProp(rejectSchema, "approver", "string", "审批人标识（可选）")
	addProp(rejectSchema, "reason", "string", "拒绝原因")

	pendingSchema := str("列出所有待审批任务")
	pendingSchema.Required = nil

	return []tool.Tool{
		&simpleTool{decl: &tool.Declaration{Name: "list_collectors", Description: "查询 Collector 集群状态", InputSchema: listSchema}, handle: d.handleListCollectors},
		&simpleTool{decl: &tool.Declaration{Name: "get_collector_config", Description: "获取 Collector 当前生效配置", InputSchema: getCfgSchema}, handle: d.handleGetConfig},
		&simpleTool{decl: &tool.Declaration{Name: "generate_config", Description: "对话生成 Collector 配置", InputSchema: genSchema}, handle: d.handleGenerateConfig},
		&simpleTool{decl: &tool.Declaration{Name: "optimize_config", Description: "自动分析并提议配置优化", InputSchema: optSchema}, handle: d.handleOptimizeConfig},
		&simpleTool{decl: &tool.Declaration{Name: "apply_config", Description: "直接下发配置（需全局审批关闭）", InputSchema: applySchema}, handle: d.handleApplyConfig},
		&simpleTool{decl: &tool.Declaration{Name: "upgrade_collector", Description: "创建 Collector 版本升级任务", InputSchema: upgradeSchema}, handle: d.handleUpgrade},
		&simpleTool{decl: &tool.Declaration{Name: "approve_task", Description: "审批通过并下发", InputSchema: approveSchema}, handle: d.handleApprove},
		&simpleTool{decl: &tool.Declaration{Name: "reject_task", Description: "拒绝任务", InputSchema: rejectSchema}, handle: d.handleReject},
		&simpleTool{decl: &tool.Declaration{Name: "list_pending_tasks", Description: "列出待审批任务", InputSchema: pendingSchema}, handle: d.handleListPending},
	}
}

func getString(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// targetCollectors 将 target 解析为 Collector 列表：instance_uid 或分组 ID。
func (d *Deps) targetCollectors(ctx context.Context, target string) ([]store.Collector, error) {
	if target == "" {
		return nil, fmt.Errorf("target 不能为空")
	}
	all, _, err := d.Store.ListCollectors(ctx, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("查询 Collector 失败: %w", err)
	}
	out := make([]store.Collector, 0)
	for _, c := range all {
		if c.InstanceUID == target || c.GroupID == target {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("未找到匹配 target=%q 的 Collector", target)
	}
	return out, nil
}

// handleListCollectors 实现 list_collectors。
func (d *Deps) handleListCollectors(ctx context.Context, args map[string]any) (any, error) {
	group := getString(args, "group")
	all, _, err := d.Store.ListCollectors(ctx, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("查询 Collector 失败: %w", err)
	}
	type view struct {
		InstanceUID string `json:"instance_uid"`
		Hostname    string `json:"hostname"`
		Version     string `json:"version"`
		Status      string `json:"status"`
		GroupID     string `json:"group_id"`
	}
	out := make([]view, 0)
	for _, c := range all {
		if group != "" && c.GroupID != group {
			continue
		}
		out = append(out, view{InstanceUID: c.InstanceUID, Hostname: c.Hostname, Version: c.Version, Status: string(c.Status), GroupID: c.GroupID})
	}
	return out, nil
}

// handleGetConfig 实现 get_collector_config。
func (d *Deps) handleGetConfig(ctx context.Context, args map[string]any) (any, error) {
	uid := getString(args, "instance_uid")
	if uid == "" {
		return nil, fmt.Errorf("instance_uid 不能为空")
	}
	c, err := d.Store.GetCollector(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("获取 Collector 失败: %w", err)
	}
	return map[string]any{
		"instance_uid":     c.InstanceUID,
		"version":          c.Version,
		"status":           string(c.Status),
		"effective_config": c.EffectiveConfig,
	}, nil
}

// handleGenerateConfig 实现 generate_config：LLM 生成 → 校验 → 任务。
func (d *Deps) handleGenerateConfig(ctx context.Context, args map[string]any) (any, error) {
	target := getString(args, "target")
	desc := getString(args, "description")
	if target == "" || desc == "" {
		return nil, fmt.Errorf("target 与 description 均为必填")
	}
	t := &store.Task{
		ID:              NewULID(),
		Type:            store.TaskTypeGenerate,
		Status:          store.TaskStatusPending,
		RequireApproval: d.Config.RequireApproval,
		Input:           desc,
		TargetGroupID:   target,
	}
	if err := d.Tasks.Create(ctx, t); err != nil {
		return nil, fmt.Errorf("创建任务失败: %w", err)
	}
	d.audit(ctx, "agent", store.AuditActionGenerate, t.ID, fmt.Sprintf("生成配置 target=%s", target))
	if _, err := d.Tasks.SetStatus(ctx, t.ID, store.TaskStatusGenerating); err != nil {
		return nil, err
	}

	yamlContent, modelUsed, err := d.generateYAML(ctx, t, desc, "")
	if err != nil {
		if _, setErr := d.Tasks.SetStatus(ctx, t.ID, store.TaskStatusFailed); setErr != nil {
			d.log().Warn("标记任务失败状态出错", "task_id", t.ID, "error", setErr)
		}
		return nil, err
	}
	t.ModelUsed = modelUsed
	t.GeneratedYAML = yamlContent
	if _, err := d.Tasks.SetStatus(ctx, t.ID, store.TaskStatusValidating); err != nil {
		return nil, err
	}
	res := validator.Validate(yamlContent, d.Config.OtelcolBin, d.Config.StrictValidate)
	if !res.Valid {
		if _, setErr := d.Tasks.SetStatus(ctx, t.ID, store.TaskStatusFailed); setErr != nil {
			d.log().Warn("标记任务失败状态出错", "task_id", t.ID, "error", setErr)
		}
		return nil, fmt.Errorf("配置校验未通过: %s", strings.Join(res.Errors, "; "))
	}
	t, err = d.Tasks.SetStatus(ctx, t.ID, store.TaskStatusAwaitingApproval)
	if err != nil {
		return nil, err
	}
	t.GeneratedYAML = yamlContent
	_ = d.Store.UpdateTask(ctx, t)

	if !d.Config.RequireApproval {
		// 审批关闭：直接下发。
		return d.DispatchApprove(ctx, t.ID, "agent")
	}
	return map[string]any{
		"task_id":    t.ID,
		"status":     string(t.Status),
		"message":    "配置已生成并通过校验，等待审批后下发",
		"model_used": modelUsed,
	}, nil
}

// generateYAML 调用 LLM 生成配置，校验失败自动重试（≤3 次）。
func (d *Deps) generateYAML(ctx context.Context, t *store.Task, desc, contextInfo string) (string, string, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		yamlContent, err := d.callConfigLLM(ctx, desc, contextInfo)
		if err != nil {
			lastErr = err
			continue
		}
		res := validator.Validate(yamlContent, d.Config.OtelcolBin, d.Config.StrictValidate)
		if res.Valid {
			return yamlContent, "deepseek", nil
		}
		lastErr = fmt.Errorf("配置校验失败: %s", strings.Join(res.Errors, "; "))
	}
	return "", "", fmt.Errorf("生成配置重试 3 次仍失败: %w", lastErr)
}

// callConfigLLM 以"配置专家"身份调用 LLM 输出纯 YAML。
func (d *Deps) callConfigLLM(ctx context.Context, desc, contextInfo string) (string, error) {
	system := "你是 OpenTelemetry Collector 配置专家。" +
		"根据用户描述生成 otelcol-contrib 的 YAML 配置。" +
		"只输出 YAML 代码块内容，不要任何解释文字。必须包含 service.pipelines 节。"
	if contextInfo != "" {
		system += "\n以下是相关上下文：\n" + contextInfo
	}
	req := model.NewRequest([]model.Message{
		{Role: model.RoleSystem, Content: system},
		{Role: model.RoleUser, Content: desc},
	})
	ch, err := d.Model.GenerateContent(ctx, req)
	if err != nil {
		return "", fmt.Errorf("LLM 调用失败: %w", err)
	}
	var sb strings.Builder
	for resp := range ch {
		if resp.Error != nil {
			return "", fmt.Errorf("LLM 返回错误: %s", resp.Error.Message)
		}
		for _, choice := range resp.Choices {
			sb.WriteString(choice.Message.Content)
		}
	}
	content := strings.TrimSpace(sb.String())
	content = stripCodeFence(content)
	if content == "" {
		return "", fmt.Errorf("LLM 返回空配置")
	}
	return content, nil
}

// stripCodeFence 提取 LLM 输出中的第一个 ```...``` 代码围栏块；
// 无围栏时原样返回。
func stripCodeFence(content string) string {
	content = strings.TrimSpace(content)
	start := strings.Index(content, "```")
	if start < 0 {
		return content
	}
	after := content[start+3:]
	// 跳过围栏语言标识行。
	if nl := strings.Index(after, "\n"); nl >= 0 {
		after = after[nl+1:]
	}
	if end := strings.Index(after, "```"); end >= 0 {
		return strings.TrimSpace(after[:end])
	}
	return strings.TrimSpace(after)
}

// handleOptimizeConfig 实现 optimize_config。
func (d *Deps) handleOptimizeConfig(ctx context.Context, args map[string]any) (any, error) {
	uid := getString(args, "instance_uid")
	if uid == "" {
		return nil, fmt.Errorf("instance_uid 不能为空")
	}
	goal := getString(args, "goal")
	c, err := d.Store.GetCollector(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("获取 Collector 失败: %w", err)
	}
	contextInfo := fmt.Sprintf("Collector 版本 %s，状态 %s。\n当前生效配置：\n%s",
		c.Version, c.Status, c.EffectiveConfig)
	desc := "请分析并优化上述配置"
	if goal != "" {
		desc = "请针对目标优化：" + goal + "\n" + desc
	}
	t := &store.Task{
		ID:              NewULID(),
		Type:            store.TaskTypeOptimize,
		Status:          store.TaskStatusPending,
		RequireApproval: d.Config.RequireApproval,
		Input:           desc,
		TargetGroupID:   uid,
	}
	if err := d.Tasks.Create(ctx, t); err != nil {
		return nil, fmt.Errorf("创建任务失败: %w", err)
	}
	if _, err := d.Tasks.SetStatus(ctx, t.ID, store.TaskStatusGenerating); err != nil {
		return nil, err
	}
	yamlContent, modelUsed, err := d.generateYAML(ctx, t, desc, contextInfo)
	if err != nil {
		if _, setErr := d.Tasks.SetStatus(ctx, t.ID, store.TaskStatusFailed); setErr != nil {
			d.log().Warn("标记任务失败状态出错", "task_id", t.ID, "error", setErr)
		}
		return nil, err
	}
	t.GeneratedYAML = yamlContent
	t.ModelUsed = modelUsed
	if _, err := d.Tasks.SetStatus(ctx, t.ID, store.TaskStatusValidating); err != nil {
		return nil, err
	}
	t, err = d.Tasks.SetStatus(ctx, t.ID, store.TaskStatusAwaitingApproval)
	if err != nil {
		return nil, err
	}
	t.GeneratedYAML = yamlContent
	_ = d.Store.UpdateTask(ctx, t)
	if !d.Config.RequireApproval {
		return d.DispatchApprove(ctx, t.ID, "agent")
	}
	return map[string]any{"task_id": t.ID, "status": string(t.Status), "message": "优化方案已生成，等待审批", "model_used": modelUsed}, nil
}

// handleApplyConfig 实现 apply_config（仅审批关闭时允许直接下发）。
func (d *Deps) handleApplyConfig(ctx context.Context, args map[string]any) (any, error) {
	if d.Config.RequireApproval {
		return nil, fmt.Errorf("全局审批已开启（REQUIRE_APPROVAL=true），禁止直接下发，请使用 generate_config + approve_task 流程")
	}
	target := getString(args, "target")
	yamlContent := getString(args, "yaml")
	if target == "" || yamlContent == "" {
		return nil, fmt.Errorf("target 与 yaml 均为必填")
	}
	res := validator.Validate(yamlContent, d.Config.OtelcolBin, d.Config.StrictValidate)
	if !res.Valid {
		return nil, fmt.Errorf("配置校验未通过: %s", strings.Join(res.Errors, "; "))
	}
	collectors, err := d.targetCollectors(ctx, target)
	if err != nil {
		return nil, err
	}
	for _, c := range collectors {
		if err := d.OpAMP.PushConfig(ctx, c.InstanceUID, yamlContent); err != nil {
			d.log().Warn("apply_config 下发失败", "instance_uid", c.InstanceUID, "error", err)
		}
		d.recordConfigVersion(ctx, c.InstanceUID, yamlContent)
	}
	d.audit(ctx, "agent", store.AuditActionApply, target, validator.Hash(yamlContent))
	return map[string]any{"message": fmt.Sprintf("已下发到 %d 个 Collector", len(collectors))}, nil
}

// handleUpgrade 实现 upgrade_collector（创建升级任务，包分发在后续版本实现）。
func (d *Deps) handleUpgrade(ctx context.Context, args map[string]any) (any, error) {
	target := getString(args, "target")
	version := getString(args, "version")
	if target == "" || version == "" {
		return nil, fmt.Errorf("target 与 version 均为必填")
	}
	t := &store.Task{
		ID:              NewULID(),
		Type:            store.TaskTypeUpgrade,
		Status:          store.TaskStatusAwaitingApproval,
		RequireApproval: true,
		Input:           fmt.Sprintf("升级 target=%s 到版本 %s", target, version),
		TargetGroupID:   target,
	}
	if err := d.Tasks.Create(ctx, t); err != nil {
		return nil, fmt.Errorf("创建任务失败: %w", err)
	}
	return map[string]any{"task_id": t.ID, "status": string(t.Status), "message": "升级任务已创建（包分发能力为 Beta，需 Collector 支持 PackagesAvailable）"}, nil
}

// handleApprove 实现 approve_task：审批通过并触发下发。
func (d *Deps) handleApprove(ctx context.Context, args map[string]any) (any, error) {
	taskID := getString(args, "task_id")
	approver := getString(args, "approver")
	if approver == "" {
		approver = "user"
	}
	if taskID == "" {
		return nil, fmt.Errorf("task_id 不能为空")
	}
	return d.DispatchApprove(ctx, taskID, approver)
}

// DispatchApprove 审批通过并触发下发（REST 与 MCP 共用）。
func (d *Deps) DispatchApprove(ctx context.Context, taskID, approver string) (any, error) {
	t, err := d.Tasks.Approve(ctx, taskID, approver)
	if err != nil {
		return nil, err
	}
	d.audit(ctx, approver, store.AuditActionApprove, taskID, "")
	// 升级任务：当前版本仅记录审批（包分发后续实现）。
	if t.Type == store.TaskTypeUpgrade {
		if _, err := d.Tasks.SetStatus(ctx, taskID, store.TaskStatusDone); err != nil {
			return nil, err
		}
		return map[string]any{"task_id": taskID, "status": string(store.TaskStatusDone), "message": "升级任务已审批（Beta 能力，需 Collector 支持包分发）"}, nil
	}
	// 回滚任务：下发目标历史版本。
	if t.Type == store.TaskTypeRollback {
		return d.dispatchRollback(ctx, t, approver)
	}
	// 常规任务（generate/optimize）：下发 GeneratedYAML。
	if t.GeneratedYAML == "" {
		return nil, fmt.Errorf("任务 %s 没有可下发的配置", taskID)
	}
	collectors, err := d.targetCollectors(ctx, t.TargetGroupID)
	if err != nil {
		if _, setErr := d.Tasks.SetStatus(ctx, taskID, store.TaskStatusFailed); setErr != nil {
			d.log().Warn("标记任务失败状态出错", "task_id", taskID, "error", setErr)
		}
		return nil, err
	}
	for _, c := range collectors {
		if err := d.OpAMP.PushConfig(ctx, c.InstanceUID, t.GeneratedYAML); err != nil {
			d.log().Warn("approve 下发失败", "task_id", taskID, "instance_uid", c.InstanceUID, "error", err)
		}
		d.recordConfigVersion(ctx, c.InstanceUID, t.GeneratedYAML)
	}
	if _, err := d.Tasks.SetStatus(ctx, taskID, store.TaskStatusDone); err != nil {
		return nil, err
	}
	d.audit(ctx, approver, store.AuditActionApply, taskID, validator.Hash(t.GeneratedYAML))
	return map[string]any{"task_id": taskID, "status": string(store.TaskStatusDone), "message": fmt.Sprintf("已下发到 %d 个 Collector", len(collectors))}, nil
}

// dispatchRollback 处理回滚任务审批后的版本下发。
func (d *Deps) dispatchRollback(ctx context.Context, t *store.Task, approver string) (any, error) {
	if t.TargetInstanceUID == "" || t.RollbackVersionID == 0 {
		return nil, fmt.Errorf("回滚任务缺少目标 Collector 或版本信息")
	}
	versions, _, err := d.Store.ListConfigVersions(ctx, t.TargetInstanceUID, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("查询版本历史失败: %w", err)
	}
	var target *store.ConfigVersion
	for i := range versions {
		if versions[i].ID == t.RollbackVersionID {
			target = &versions[i]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("回滚目标版本 %d 不存在", t.RollbackVersionID)
	}
	// 防御性校验归属（ListConfigVersions 已按 Collector 过滤）。
	if target.CollectorInstanceUID != t.TargetInstanceUID {
		return nil, fmt.Errorf("版本 %d 不属于该 Collector", t.RollbackVersionID)
	}
	if err := d.OpAMP.PushConfig(ctx, t.TargetInstanceUID, target.YAML); err != nil {
		if _, setErr := d.Tasks.SetStatus(ctx, t.ID, store.TaskStatusFailed); setErr != nil {
			d.log().Warn("标记任务失败状态出错", "task_id", t.ID, "error", setErr)
		}
		return nil, fmt.Errorf("回滚下发失败: %w", err)
	}
	d.recordConfigVersion(ctx, t.TargetInstanceUID, target.YAML)
	if _, err := d.Tasks.SetStatus(ctx, t.ID, store.TaskStatusDone); err != nil {
		return nil, err
	}
	d.audit(ctx, approver, store.AuditActionRollback, t.ID, validator.Hash(target.YAML))
	return map[string]any{"task_id": t.ID, "status": string(store.TaskStatusDone), "message": "已回滚到历史版本"}, nil
}

// recordConfigVersion 下发成功后写入版本快照（回滚闭环地基），
// 并同步注册表与持久层的生效配置（Collector 回传后会覆盖为真实值）。
func (d *Deps) recordConfigVersion(ctx context.Context, instanceUID, yamlContent string) {
	version := &store.ConfigVersion{
		CollectorInstanceUID: instanceUID,
		YAML:                 yamlContent,
		Hash:                 validator.Hash(yamlContent),
		Validated:            true,
		CreatedAt:            d.now(),
	}
	if err := d.Store.CreateConfigVersion(ctx, version); err != nil {
		d.log().Warn("写入版本快照失败", "instance_uid", instanceUID, "error", err)
		return
	}
	if d.Registry != nil {
		if c, ok := d.Registry.Get(instanceUID); ok {
			c.EffectiveConfig = yamlContent
			d.Registry.Upsert(c, nil)
		}
	}
	if c, err := d.Store.GetCollector(ctx, instanceUID); err == nil {
		c.EffectiveConfig = yamlContent
		_ = d.Store.UpsertCollector(ctx, c)
	}
}

// handleReject 实现 reject_task。
func (d *Deps) handleReject(ctx context.Context, args map[string]any) (any, error) {
	taskID := getString(args, "task_id")
	approver := getString(args, "approver")
	reason := getString(args, "reason")
	if approver == "" {
		approver = "user"
	}
	if taskID == "" {
		return nil, fmt.Errorf("task_id 不能为空")
	}
	t, err := d.Tasks.Reject(ctx, taskID, approver, reason)
	if err != nil {
		return nil, err
	}
	d.audit(ctx, approver, store.AuditActionReject, taskID, reason)
	return map[string]any{"task_id": t.ID, "status": string(t.Status), "reason": reason}, nil
}

// handleListPending 实现 list_pending_tasks。
func (d *Deps) handleListPending(ctx context.Context, args map[string]any) (any, error) {
	tasks, _, err := d.Store.ListTasks(ctx, store.TaskStatusAwaitingApproval, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("查询任务失败: %w", err)
	}
	return tasks, nil
}

// audit 写入审计日志（best effort）。
func (d *Deps) audit(ctx context.Context, actor string, action store.AuditAction, subject, detail string) {
	_ = d.Store.AppendAudit(ctx, &store.AuditLog{
		Actor: actor, Action: action, Subject: subject, Detail: detail, CreatedAt: d.now(),
	})
}
