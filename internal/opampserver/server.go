package opampserver

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/chengyifei1991-ai/opamp-backend/internal/store"
	"github.com/open-telemetry/opamp-go/protobufs"
	"github.com/open-telemetry/opamp-go/server"
	"github.com/open-telemetry/opamp-go/server/types"
)

// Server 是 OpAMP 服务器的封装，实现连接认证、状态接收与配置下发。
type Server struct {
	opamp     server.OpAMPServer
	reg       *Registry
	st        store.Store
	authToken string
	logger    *slog.Logger
	handler   http.HandlerFunc
	connCtx   server.ConnContext

	// pending 保存无法主动推送（HTTP 拉取模式）的待下发配置，
	// 在 Collector 下一次上报时随响应附带（key 为 instance_uid）。
	pendingMu sync.Mutex
	pending   map[string]string
}

// NewServer 创建 OpAMP 服务器封装。
func NewServer(logger *slog.Logger, st store.Store, reg *Registry, authToken string) (*Server, error) {
	s := &Server{
		reg:       reg,
		st:        st,
		authToken: authToken,
		logger:    logger,
		pending:   make(map[string]string),
	}
	s.opamp = server.New(slogAdapter{logger: logger})
	handler, connCtx, err := s.opamp.Attach(server.Settings{
		Callbacks: types.Callbacks{
			OnConnecting: s.onConnecting,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("opampserver: attach: %w", err)
	}
	s.handler = http.HandlerFunc(handler)
	s.connCtx = connCtx
	return s, nil
}

// Handler 返回 OpAMP HTTP handler 与 ConnContext，由主 HTTP 服务器挂载。
func (s *Server) Handler() (http.HandlerFunc, server.ConnContext) {
	return s.handler, s.connCtx
}

// onConnecting 校验 Collector 接入凭证。配置了 authToken 时，
// 校验 Authorization: Bearer <token> 头（常量时间比较）。
func (s *Server) onConnecting(r *http.Request) types.ConnectionResponse {
	if s.authToken == "" {
		return types.ConnectionResponse{Accept: true, ConnectionCallbacks: types.ConnectionCallbacks{
			OnConnected:            s.onConnected,
			OnMessage:              s.onMessage,
			OnConnectionClose:      s.onConnectionClose,
			OnReadMessageError:     s.onReadMessageError,
			OnMessageResponseError: s.onMessageResponseError,
		}}
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(s.authToken)) == 1 {
		return types.ConnectionResponse{Accept: true, ConnectionCallbacks: types.ConnectionCallbacks{
			OnConnected:            s.onConnected,
			OnMessage:              s.onMessage,
			OnConnectionClose:      s.onConnectionClose,
			OnReadMessageError:     s.onReadMessageError,
			OnMessageResponseError: s.onMessageResponseError,
		}}
	}
	s.logger.Warn("opamp: 拒绝未认证连接", "remote", r.RemoteAddr)
	return types.ConnectionResponse{Accept: false, HTTPStatusCode: http.StatusUnauthorized}
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if strings.HasPrefix(header, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(header, prefix))
	}
	return ""
}

func (s *Server) onConnected(ctx context.Context, conn types.Connection) {
	s.logger.Info("opamp: Collector 已连接", "remote", conn.Connection().RemoteAddr().String())
}

// onMessage 处理 AgentToServer 上报：更新注册表并返回 ServerToAgent。
func (s *Server) onMessage(ctx context.Context, conn types.Connection, msg *protobufs.AgentToServer) *protobufs.ServerToAgent {
	uid := hex.EncodeToString(msg.InstanceUid)
	if uid == "" {
		s.logger.Error("opamp: 收到空 instance_uid 的消息")
		return nil
	}
	c := s.collectorFromMessage(uid, msg)
	c.LastSeenAt = time.Now().UTC()
	s.reg.Upsert(c, conn)
	resp := &protobufs.ServerToAgent{
		InstanceUid:  msg.InstanceUid,
		Capabilities: serverCapabilities,
	}
	// HTTP 拉取模式下，若存在待下发配置则随本次响应下发。
	if cfg := s.takePending(uid); cfg != "" {
		resp.RemoteConfig = remoteConfigOf(cfg)
		s.logger.Info("opamp: 随轮询下发待推送配置", "instance_uid", uid)
	}
	return resp
}

// collectorFromMessage 从 AgentToServer 提取 Collector 状态字段。
func (s *Server) collectorFromMessage(uid string, msg *protobufs.AgentToServer) *store.Collector {
	c := &store.Collector{InstanceUID: uid, Status: store.CollectorStatusHealthy}
	if prev, ok := s.reg.Get(uid); ok {
		*c = *prev
	}
	if msg.AgentDescription != nil {
		for _, kv := range msg.AgentDescription.IdentifyingAttributes {
			switch kv.Key {
			case "service.version":
				c.Version = kv.Value.GetStringValue()
			case "host.name":
				c.Hostname = kv.Value.GetStringValue()
			}
		}
		for _, kv := range msg.AgentDescription.NonIdentifyingAttributes {
			switch kv.Key {
			case "service.version":
				if c.Version == "" {
					c.Version = kv.Value.GetStringValue()
				}
			case "host.name":
				if c.Hostname == "" {
					c.Hostname = kv.Value.GetStringValue()
				}
			}
		}
	}
	if msg.Health != nil {
		if !msg.Health.Healthy {
			c.Status = store.CollectorStatusUnhealthy
		}
	}
	if msg.EffectiveConfig != nil && msg.EffectiveConfig.ConfigMap != nil {
		c.EffectiveConfig = configMapToString(msg.EffectiveConfig.ConfigMap)
	}
	return c
}

// configMapToString 将 OpAMP ConfigMap 序列化为单字符串（key: body 拼接）。
func configMapToString(cm *protobufs.AgentConfigMap) string {
	var sb strings.Builder
	for k, f := range cm.ConfigMap {
		if k != "" {
			sb.WriteString("# ")
			sb.WriteString(k)
			sb.WriteString("\n")
		}
		sb.Write(f.Body)
		sb.WriteString("\n")
	}
	return sb.String()
}

func (s *Server) onConnectionClose(conn types.Connection) {
	// 从注册表移除连接绑定，状态保留（后续 MarkOffline 判定）。
	uid := s.uidForConn(conn)
	if uid != "" {
		s.reg.Remove(uid)
		s.logger.Info("opamp: Collector 连接关闭", "instance_uid", uid)
	}
}

// uidForConn 从注册表反查连接对应的 instance_uid（O(n)，连接数有限）。
func (s *Server) uidForConn(conn types.Connection) string {
	for _, c := range s.reg.List() {
		if cConn, ok := s.reg.Conn(c.InstanceUID); ok && cConn == conn {
			return c.InstanceUID
		}
	}
	return ""
}

func (s *Server) onReadMessageError(conn types.Connection, mt int, msgByte []byte, err error) {
	s.logger.Warn("opamp: 读取消息失败", "error", err)
}

func (s *Server) onMessageResponseError(conn types.Connection, message *protobufs.ServerToAgent, err error) {
	s.logger.Warn("opamp: 发送响应失败", "error", err)
}

// PushConfig 向指定 Collector 下发配置。
//
//   - WebSocket 连接：通过 conn.Send 主动即时推送；
//   - HTTP 拉取连接：记录为待下发，随 Collector 下一次上报返回。
//
// 调用方需先通过 validator 完成配置校验。
func (s *Server) PushConfig(ctx context.Context, instanceUID, yamlContent string) error {
	conn, ok := s.reg.Conn(instanceUID)
	if !ok {
		// Collector 不在线：仅记录待下发（恢复后随首次上报下发）。
		s.setPending(instanceUID, yamlContent)
		s.logger.Info("opamp: Collector 离线，配置已排队待下发", "instance_uid", instanceUID)
		return nil
	}
	uidBytes, err := hex.DecodeString(instanceUID)
	if err != nil {
		return fmt.Errorf("opampserver: 非法 instance_uid %q: %w", instanceUID, err)
	}
	msg := &protobufs.ServerToAgent{
		InstanceUid:  uidBytes,
		RemoteConfig: remoteConfigOf(yamlContent),
	}
	if err := conn.Send(ctx, msg); err != nil {
		// 主动推送失败（如 HTTP 连接）：回退为随轮询下发。
		s.setPending(instanceUID, yamlContent)
		s.logger.Warn("opamp: 主动推送失败，改为随轮询下发", "instance_uid", instanceUID, "error", err)
		return nil
	}
	return nil
}

// remoteConfigOf 构造单文件 RemoteConfig 消息。
func remoteConfigOf(yamlContent string) *protobufs.AgentRemoteConfig {
	return &protobufs.AgentRemoteConfig{
		Config: &protobufs.AgentConfigMap{
			ConfigMap: map[string]*protobufs.AgentConfigFile{
				"": {
					Body:        []byte(yamlContent),
					ContentType: "text/yaml",
				},
			},
		},
	}
}

func (s *Server) setPending(uid, yamlContent string) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	s.pending[uid] = yamlContent
}

// takePending 取出并清除指定 Collector 的待下发配置。
func (s *Server) takePending(uid string) string {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	cfg, ok := s.pending[uid]
	if !ok {
		return ""
	}
	delete(s.pending, uid)
	return cfg
}

// serverCapabilities 声明服务器支持的 OpAMP 能力位掩码：
// AcceptsStatus | OffersRemoteConfig | AcceptsEffectiveConfig |
// OffersPackages | AcceptsPackagesStatus。
const serverCapabilities uint64 = 1 | 2 | 4 | 8 | 16

// slogAdapter 将 *slog.Logger 适配为 opamp-go 的 types.Logger 接口。
type slogAdapter struct {
	logger *slog.Logger
}

// Debugf 实现 types.Logger.Debugf。
func (a slogAdapter) Debugf(ctx context.Context, format string, v ...any) {
	a.logger.Debug(fmt.Sprintf(format, v...))
}

// Errorf 实现 types.Logger.Errorf。
func (a slogAdapter) Errorf(ctx context.Context, format string, v ...any) {
	a.logger.Error(fmt.Sprintf(format, v...))
}
