// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package opampserver

import (
	"context"
	"encoding/hex"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/store"
	"github.com/open-telemetry/opamp-go/protobufs"
)

// nopWriter 丢弃日志输出。
type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

// newTestServer 创建不监听端口的测试服务器。
func newTestServer(t *testing.T, token string) *Server {
	t.Helper()
	st, err := store.New("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("store.New 失败: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	logger := slog.New(slog.NewTextHandler(nopWriter{}, nil))
	reg := NewRegistry(st, time.Minute)
	srv, err := NewServer(logger, st, reg, token)
	if err != nil {
		t.Fatalf("NewServer 失败: %v", err)
	}
	return srv
}

// TestBearerToken 表驱动测试 Authorization 头解析。
func TestBearerToken(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"标准 Bearer", "Bearer abc123", "abc123"},
		{"带空格", "Bearer   abc123  ", "abc123"},
		{"大小写敏感前缀", "bearer abc", ""},
		{"无 Bearer 前缀", "abc123", ""},
		{"空头", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bearerToken(tt.header); got != tt.want {
				t.Errorf("bearerToken(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

// TestOnConnecting 验证接入认证：配置 token 后拒绝无凭证连接。
func TestOnConnecting(t *testing.T) {
	srv := newTestServer(t, "secret-token")
	req := httptestNewRequest()
	resp := srv.onConnecting(req)
	if resp.Accept {
		t.Errorf("无凭证连接应被拒绝")
	}
	if resp.HTTPStatusCode != http.StatusUnauthorized {
		t.Errorf("HTTPStatusCode = %d, want 401", resp.HTTPStatusCode)
	}

	req = httptestNewRequest()
	req.Header.Set("Authorization", "Bearer secret-token")
	resp = srv.onConnecting(req)
	if !resp.Accept {
		t.Errorf("带正确 token 的连接应被接受")
	}
}

func httptestNewRequest() *http.Request {
	return &http.Request{Header: http.Header{}}
}

// TestOnConnectingNoToken 验证未配置 token 时放行所有连接。
func TestOnConnectingNoToken(t *testing.T) {
	srv := newTestServer(t, "")
	resp := srv.onConnecting(httptestNewRequest())
	if !resp.Accept {
		t.Errorf("未配置 token 时应放行")
	}
}

// TestPushConfigOfflineQueues 验证离线 Collector 下发进入待推送队列。
func TestPushConfigOfflineQueues(t *testing.T) {
	srv := newTestServer(t, "")
	// 未注册的 Collector 视为离线：应排队而不报错。
	if err := srv.PushConfig(context.Background(), "00000000000000000000000000000000", "receivers: {}"); err != nil {
		t.Fatalf("PushConfig 离线排队失败: %v", err)
	}
	if got := srv.takePending("00000000000000000000000000000000"); got == "" {
		t.Errorf("离线下发的配置应进入待推送队列")
	}
}

// TestCollectorFromMessage 验证 AgentToServer 状态提取与**健康状态沿用语义**。
//
// 回归缺陷：OpAMP 客户端只在健康变化时携带 health，心跳/配置上报消息不带；
// 旧实现把"未上报"当 unknown，导致 healthy 被紧随其后的心跳冲掉
// （实测：collector 活着、端口在听，页面却显示"未知"）。
func TestCollectorFromMessage(t *testing.T) {
	srv := newTestServer(t, "")
	const uid = "uid-1"

	// 1. 从未收到健康信号（无历史行）→ unknown。
	msg := agentToServerMsg(uid, "node-1", "0.156.0", true)
	msg.Health = nil
	c := srv.collectorFromMessage(uid, msg)
	if c.Hostname != "node-1" || c.Version != "0.156.0" {
		t.Errorf("hostname/version 提取失败: %+v", c)
	}
	if c.EffectiveConfig == "" {
		t.Errorf("effective config 未提取")
	}
	if c.Status != store.CollectorStatusUnknown {
		t.Errorf("无历史且未上报 health: status = %q, want unknown", c.Status)
	}

	// 2. 明确上报 healthy 后落库（模拟真实 onMessage 落库）。
	msg.Health = &protobufs.ComponentHealth{Healthy: true}
	c = srv.collectorFromMessage(uid, msg)
	if c.Status != store.CollectorStatusHealthy {
		t.Fatalf("status = %q, want healthy", c.Status)
	}
	srv.reg.Upsert(c, nil)

	// 3. 后续消息不带 health（心跳/effective 上报）→ 保持 healthy（本次回归点）。
	msg.Health = nil
	c = srv.collectorFromMessage(uid, msg)
	if c.Status != store.CollectorStatusHealthy {
		t.Errorf("未上报 health 应沿用上次状态: status = %q, want healthy", c.Status)
	}

	// 4. 明确上报不健康 → unhealthy；随后不带 health 仍沿用 unhealthy。
	msg.Health = &protobufs.ComponentHealth{Healthy: false}
	c = srv.collectorFromMessage(uid, msg)
	if c.Status != store.CollectorStatusUnhealthy {
		t.Fatalf("status = %q, want unhealthy", c.Status)
	}
	srv.reg.Upsert(c, nil)
	msg.Health = nil
	c = srv.collectorFromMessage(uid, msg)
	if c.Status != store.CollectorStatusUnhealthy {
		t.Errorf("未上报 health 应沿用 unhealthy: status = %q", c.Status)
	}

	// 5. 此前判定离线后重新连上（无 health）→ unknown（在线但健康未知），而非停留 offline。
	offline := *c
	offline.Status = store.CollectorStatusOffline
	srv.reg.Upsert(&offline, nil)
	c = srv.collectorFromMessage(uid, msg)
	if c.Status != store.CollectorStatusUnknown {
		t.Errorf("离线后重连未上报 health: status = %q, want unknown", c.Status)
	}
}

// agentToServerMsg 构造测试用 AgentToServer 消息。
func agentToServerMsg(uid, hostname, version string, healthy bool) *protobufs.AgentToServer {
	sv := func(s string) *protobufs.AnyValue {
		return &protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: s}}
	}
	msg := &protobufs.AgentToServer{
		InstanceUid: []byte(uid),
		AgentDescription: &protobufs.AgentDescription{
			IdentifyingAttributes: []*protobufs.KeyValue{
				{Key: "service.version", Value: sv(version)},
				{Key: "host.name", Value: sv(hostname)},
			},
		},
		Health: &protobufs.ComponentHealth{Healthy: healthy},
		EffectiveConfig: &protobufs.EffectiveConfig{
			ConfigMap: &protobufs.AgentConfigMap{
				ConfigMap: map[string]*protobufs.AgentConfigFile{
					"": {Body: []byte("receivers:\n  otlp:\n    protocols:\n      grpc:\n"), ContentType: "text/yaml"},
				},
			},
		},
	}
	return msg
}

// TestMetadataSurvivesRestart 回归：服务端重启（注册表为空）后收到不含 AgentDescription 的
// 增量状态消息时，必须沿用存储中的 hostname/version，不得被空值覆盖；
// 同时应回置 ReportFullState 让 Agent 重发完整状态。
func TestMetadataSurvivesRestart(t *testing.T) {
	srv := newTestServer(t, "")
	ctx := context.Background()
	const uid = "55555555555555555555555555555555"

	// 模拟上一代服务写入的持久化行（hostname/version 已知，注册表为空）。
	if err := srv.st.UpsertCollector(ctx, &store.Collector{
		InstanceUID: uid, Hostname: "node-x", Version: "0.156.0",
		LastSeenAt: time.Now().UTC(), Status: store.CollectorStatusHealthy,
		EffectiveConfig: "receivers: {}",
	}); err != nil {
		t.Fatalf("UpsertCollector: %v", err)
	}
	if _, ok := srv.reg.Get(uid); ok {
		t.Fatal("前置条件：注册表应为空（模拟重启）")
	}

	// 增量消息：无 AgentDescription、无 EffectiveConfig，仅健康上报。
	msg := &protobufs.AgentToServer{
		InstanceUid: mustHex(t, uid),
		Health:      &protobufs.ComponentHealth{Healthy: true},
	}
	resp := srv.onMessage(ctx, nil, msg)
	if resp == nil {
		t.Fatal("onMessage 返回 nil")
	}
	got, ok := srv.reg.Get(uid)
	if !ok {
		t.Fatal("注册表中应有记录")
	}
	if got.Hostname != "node-x" || got.Version != "0.156.0" {
		t.Errorf("元数据被覆盖: hostname=%q version=%q（应从存储沿用）", got.Hostname, got.Version)
	}
	if got.EffectiveConfig != "receivers: {}" {
		t.Errorf("effective config 应沿用存储值，实际 %q", got.EffectiveConfig)
	}
	if resp.Flags&uint64(protobufs.ServerToAgentFlags_ServerToAgentFlags_ReportFullState) == 0 {
		t.Errorf("元数据缺失时应置 ReportFullState 请求全量状态，实际 flags=%d", resp.Flags)
	}
}

// mustHex 解析 16 字节 instance_uid 的十六进制串。
func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex.DecodeString(%q): %v", s, err)
	}
	return b
}
