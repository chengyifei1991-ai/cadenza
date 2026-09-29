// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/store"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// orchestrator 是内置对话 Agent 的编排循环（轻量 ReAct）：
// 用户输入 → LLM（携带工具）→ 执行工具 → 结果回填 → 循环，直至无工具调用。
type Orchestrator struct {
	deps          *Deps
	tools         map[string]tool.CallableTool
	systemPrompt  string
	maxIterations int
}

// NewOrchestrator 创建对话编排器。
func NewOrchestrator(deps *Deps) *Orchestrator {
	tools := make(map[string]tool.CallableTool, 9)
	for _, t := range NewTools(deps) {
		if ct, ok := t.(tool.CallableTool); ok {
			tools[t.Declaration().Name] = ct
		}
	}
	return &Orchestrator{
		deps:  deps,
		tools: tools,
		systemPrompt: "你是 OpAMP 运维助手，负责管理 OpenTelemetry Collector 集群。" +
			"你可以使用工具查询集群状态、生成/优化/下发配置、审批任务。" +
			"生成配置时请先调用 generate_config；需要审批时提醒用户调用 approve_task。" +
			"回答使用中文。",
		maxIterations: 6,
	}
}

// Chat 处理一次对话：将用户消息追加到会话，执行 ReAct 循环，返回最终回复。
func (o *Orchestrator) Chat(ctx context.Context, sess *store.ChatSession, userMsg string) (string, error) {
	run := &store.AgentRun{
		ID:        NewULID(),
		SessionID: sess.ID,
		AgentType: "config_generator",
		Status:    store.RunStatusRunning,
		StartedAt: time.Now().UTC(),
	}
	if err := o.deps.Store.CreateAgentRun(ctx, run); err != nil {
		return "", fmt.Errorf("创建 AgentRun 失败: %w", err)
	}
	defer func() {
		run.FinishedAt = time.Now().UTC()
		_ = o.deps.Store.UpdateAgentRun(ctx, run)
	}()

	// 会话上下文下传：Agent 工具创建任务时回写 session_id（任务↔会话绑定）。
	ctx = withSessionID(ctx, sess.ID)
	messages := o.buildMessages(sess, userMsg)
	requestTools := o.toolMap()

	for iter := 0; iter < o.maxIterations; iter++ {
		req := model.NewRequest(messages)
		req.Tools = requestTools
		resp, err := o.generate(ctx, req)
		if err != nil {
			run.Status = store.RunStatusFailed
			return "", err
		}
		msg := o.firstContent(resp)
		run.ModelUsed = resp.Model
		if msg == nil {
			run.Status = store.RunStatusFailed
			return "", fmt.Errorf("LLM 返回空响应")
		}
		messages = append(messages, *msg)

		if len(msg.ToolCalls) == 0 {
			// 无工具调用：对话结束，返回文本。
			run.Status = store.RunStatusDone
			_ = o.deps.Store.AppendMessage(ctx, sess.ID, store.ChatMessage{Role: "assistant", Content: msg.Content, CreatedAt: time.Now().UTC()})
			return msg.Content, nil
		}
		for _, tc := range msg.ToolCalls {
			result, err := o.callTool(ctx, run, tc)
			toolMsg := model.Message{
				Role:     model.RoleTool,
				ToolID:   tc.ID,
				ToolName: tc.Function.Name,
				Content:  result,
			}
			if err != nil {
				toolMsg.Content = fmt.Sprintf("工具调用失败: %v", err)
			}
			messages = append(messages, toolMsg)
		}
	}
	run.Status = store.RunStatusDone
	return "已达到最大工具迭代次数，请简化指令或拆分步骤后重试。", nil
}

// buildMessages 构造 system + 会话历史 + 当前用户消息。
func (o *Orchestrator) buildMessages(sess *store.ChatSession, userMsg string) []model.Message {
	messages := []model.Message{{Role: model.RoleSystem, Content: o.systemPrompt}}
	// 只带最近 20 条历史，控制上下文长度。
	hist := sess.Messages
	if len(hist) > 20 {
		hist = hist[len(hist)-20:]
	}
	for _, m := range hist {
		role := model.Role(m.Role)
		if role != model.RoleUser && role != model.RoleAssistant {
			continue
		}
		messages = append(messages, model.Message{Role: role, Content: m.Content})
	}
	messages = append(messages, model.Message{Role: model.RoleUser, Content: userMsg})
	return messages
}

// toolMap 将工具集转为 model.Request.Tools 所需结构。
func (o *Orchestrator) toolMap() map[string]tool.Tool {
	out := make(map[string]tool.Tool, len(o.tools))
	for name, t := range o.tools {
		out[name] = t
	}
	return out
}

// generate 调用稳定性包装后的模型。
func (o *Orchestrator) generate(ctx context.Context, req *model.Request) (*model.Response, error) {
	ch, err := o.deps.Model.GenerateContent(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("LLM 调用失败: %w", err)
	}
	var last *model.Response
	for resp := range ch {
		if resp.Error != nil {
			return nil, fmt.Errorf("LLM 返回错误: %s", resp.Error.Message)
		}
		last = resp
	}
	if last == nil {
		return nil, fmt.Errorf("LLM 响应流为空")
	}
	return last, nil
}

// firstContent 提取响应中的第一个 Choice.Message。
func (o *Orchestrator) firstContent(resp *model.Response) *model.Message {
	if len(resp.Choices) == 0 {
		return nil
	}
	msg := resp.Choices[0].Message
	return &msg
}

// callTool 执行单个工具调用并记录到 AgentRun。
func (o *Orchestrator) callTool(ctx context.Context, run *store.AgentRun, tc model.ToolCall) (string, error) {
	call := store.ToolCall{
		Name:          tc.Function.Name,
		ArgumentsJSON: string(tc.Function.Arguments),
		StartedAt:     time.Now().UTC(),
	}
	t, ok := o.tools[tc.Function.Name]
	if !ok {
		call.Error = fmt.Sprintf("未知工具 %q", tc.Function.Name)
		run.ToolCalls = append(run.ToolCalls, call)
		return "", fmt.Errorf("未知工具 %q", tc.Function.Name)
	}
	result, err := t.Call(ctx, tc.Function.Arguments)
	if err != nil {
		call.Error = err.Error()
		run.ToolCalls = append(run.ToolCalls, call)
		return "", err
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("序列化工具结果失败: %w", err)
	}
	call.ResultJSON = string(payload)
	call.FinishedAt = time.Now().UTC()
	run.ToolCalls = append(run.ToolCalls, call)
	// 工具结果截断，避免上下文爆炸。
	if len(call.ResultJSON) > 4000 {
		call.ResultJSON = call.ResultJSON[:4000] + "...(截断)"
	}
	return call.ResultJSON, nil
}

// ensure strings import used.
var _ = strings.TrimSpace
