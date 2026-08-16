package opampserver

import (
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/chengyifei1991-ai/opamp-backend/internal/store"
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

// TestCollectorFromMessage 验证 AgentToServer 状态提取。
func TestCollectorFromMessage(t *testing.T) {
	srv := newTestServer(t, "")
	msg := agentToServerMsg("uid-1", "node-1", "0.156.0", true)
	c := srv.collectorFromMessage("uid-1", msg)
	if c.Hostname != "node-1" || c.Version != "0.156.0" {
		t.Errorf("hostname/version 提取失败: %+v", c)
	}
	if c.Status != store.CollectorStatusHealthy {
		t.Errorf("status = %q, want healthy", c.Status)
	}

	// 未上报 health 时状态应为 unknown。
	msg.Health = nil
	c = srv.collectorFromMessage("uid-1", msg)
	if c.Status != store.CollectorStatusUnknown {
		t.Errorf("status = %q, want unknown（health 未上报）", c.Status)
	}
	if c.EffectiveConfig == "" {
		t.Errorf("effective config 未提取")
	}

	// 不健康状态（前面已置 nil，需重新赋值）。
	msg.Health = &protobufs.ComponentHealth{Healthy: false}
	c = srv.collectorFromMessage("uid-1", msg)
	if c.Status != store.CollectorStatusUnhealthy {
		t.Errorf("status = %q, want unhealthy", c.Status)
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
