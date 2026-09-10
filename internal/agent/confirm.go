// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package agent

import (
	"context"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/validator"
)

// 生效确认相关常量。
const (
	// defaultDispatchConfirmTimeout 是下发生效确认的总等待上限
	// （含"先等自律客户端应用"与"补发重启命令后再等"两段）。
	defaultDispatchConfirmTimeout = 12 * time.Second
	// confirmPollInterval 是生效确认轮询间隔。
	confirmPollInterval = 200 * time.Millisecond
)

// configPusher 是下发所需的最小服务端能力（便于测试注入替身）。
type configPusher interface {
	// PushConfig 发送 RemoteConfig（离线则排队，随下次上报下发）。
	PushConfig(ctx context.Context, instanceUID, yamlContent string) error
	// SendRestart 向在线且声明支持重启命令的 Agent 发送 RestartCommand。
	SendRestart(ctx context.Context, instanceUID string) bool
}

// registryView 是生效确认逻辑所需的注册表视图。
type registryView interface {
	// Online 判断该 Collector 是否有活跃连接。
	Online(instanceUID string) bool
	// AcceptsRestartCommand 判断该 Collector 是否声明支持重启命令。
	AcceptsRestartCommand(instanceUID string) bool
	// ReportedHash 返回 Collector 最近一次 APPLIED 回报的远端配置哈希（hex）。
	ReportedHash(instanceUID string) (string, bool)
	// ReportedEffective 返回 Collector **上报**的 effective config 内容。
	ReportedEffective(instanceUID string) (string, bool)
}

// pushAndConfirm 对单个实例执行"下发 → 等待生效回报 →（未确认且 Agent 支持时）
// 补发重启命令 → 再等待"。
//
// 语义：
//   - Collector 离线：PushConfig 已排队（随下次上报下发），沿用 1.0 语义返回已受理；
//   - Collector 在线：先等待其自行应用并回报（supervisor 等自律客户端会
//     SetRemoteConfigStatus(APPLIED) 或上报新的 effective config）；
//   - 首段未确认且 Agent 声明 AcceptsRestartCommand 时：补发 RestartCommand 再等
//     一段——opampextension 这类"需重启才换配置"的 Agent 依赖该命令。必须先发
//     RemoteConfig 再**单独**发 Command（同一条消息带 Command 时 opamp-go 客户端
//     会忽略 RemoteConfig，见 receivedprocessor）；
//   - 两段都未确认：返回 confirmed=false，调用方记录告警（不下发失败语义）。
func pushAndConfirm(ctx context.Context, p configPusher, r registryView,
	instanceUID, yamlContent string, timeout time.Duration) (bool, error) {
	online := r != nil && r.Online(instanceUID)
	if err := p.PushConfig(ctx, instanceUID, yamlContent); err != nil {
		return false, err
	}
	if !online {
		// 离线：配置已排队待下发，无在线生效确认可言。
		return true, nil
	}
	want := validator.Hash(yamlContent)
	half := timeout / 2
	if waitConfirm(ctx, r, instanceUID, want, yamlContent, half) {
		return true, nil
	}
	if r.AcceptsRestartCommand(instanceUID) {
		p.SendRestart(ctx, instanceUID)
		if waitConfirm(ctx, r, instanceUID, want, yamlContent, half) {
			return true, nil
		}
	}
	return false, nil
}

// waitConfirm 在超时内轮询两条确认通道：
//  1. remote config ack——RemoteConfigStatus(APPLIED) 回报的 LastRemoteConfigHash
//     与下发内容 sha256（hex）一致（标准客户端，如 opamp-go supervisor）；
//  2. 上报的 effective config 覆盖了推送内容（opampextension 只上报展开后的配置，
//     由 configSubset 做语义子集比对）。
func waitConfirm(ctx context.Context, r registryView, instanceUID, want, pushed string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if h, ok := r.ReportedHash(instanceUID); ok && h == want {
			return true
		}
		if eff, ok := r.ReportedEffective(instanceUID); ok && configSubset(pushed, eff) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(confirmPollInterval):
		}
	}
}
