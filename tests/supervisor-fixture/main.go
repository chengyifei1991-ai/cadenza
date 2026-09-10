// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

// supervisor-fixture 是**真机门禁专用**的最小 OpAMP supervisor：
// 它作为 OpAMP 侧 Agent（instance_uid 归属自己），管理真实 otelcol-contrib
// 子进程的生命周期——收到远端配置即写入本地配置文件、重启子进程，并回报
// RemoteConfigStatus(APPLIED) 与 effective config。
//
// 为什么需要它：otelcol 的 opampextension 收到远端配置后**只认账不上身**
// （重启仍用原 argv 的旧配置文件），无法验证"配置真正生效"；真实生效需要
// 由管理进程负责重写配置文件并重启 Collector（即 opampsupervisor 模型）。
// 本 fixture 用 opamp-go client 复现该模型，作为可复现门禁的采集端。
//
// 用法（见 tests/real-collector-gate.sh）：
//
//	supervisor-fixture -server ws://127.0.0.1:18765/v1/opamp -token <t> \
//	  -instance-uid <uuid> -collector /usr/bin/otelcol-contrib \
//	  -config <初始功能配置 yaml> -workdir <目录>
package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/open-telemetry/opamp-go/client"
	"github.com/open-telemetry/opamp-go/client/types"
	"github.com/open-telemetry/opamp-go/protobufs"
)

// stdoutLogger 是 opamp-go 所需的最小日志实现。
type stdoutLogger struct{ prefix string }

func (l stdoutLogger) Debugf(_ context.Context, format string, v ...interface{}) {
	fmt.Printf("[%s] "+format+"\n", append([]interface{}{l.prefix}, v...)...)
}

func (l stdoutLogger) Errorf(_ context.Context, format string, v ...interface{}) {
	fmt.Fprintf(os.Stderr, "[%s] "+format+"\n", append([]interface{}{l.prefix}, v...)...)
}

// collectorProc 管理被托管的 otelcol 子进程。
type collectorProc struct {
	mu       sync.Mutex
	bin      string
	cfgPath  string
	logPath  string
	cmd      *exec.Cmd
	restarts int
}

// start 启动（或重启）子进程；配置内容已由调用方写入 cfgPath。
func (p *collectorProc) start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
		_, _ = p.cmd.Process.Wait()
	}
	logFile, err := os.OpenFile(p.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	cmd := exec.Command(p.bin, "--config", p.cfgPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	p.cmd = cmd
	p.restarts++
	fmt.Printf("[supervisor] collector 已启动 pid=%d (第 %d 次)\n", cmd.Process.Pid, p.restarts)
	return nil
}

// pid 返回当前子进程 pid（无则 0）。
func (p *collectorProc) pid() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// stop 终止子进程。
func (p *collectorProc) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
		_, _ = p.cmd.Process.Wait()
		p.cmd = nil
	}
}

func main() {
	var (
		serverURL   = flag.String("server", "ws://127.0.0.1:18765/v1/opamp", "OpAMP 服务端 WS 地址")
		token       = flag.String("token", "", "OpAMP 接入 Bearer token")
		instanceUID = flag.String("instance-uid", "", "实例 UID（UUID）")
		collector   = flag.String("collector", "/usr/bin/otelcol-contrib", "otelcol 二进制路径")
		initConfig  = flag.String("config", "", "初始功能配置 yaml（不含 opamp 扩展）")
		workdir     = flag.String("workdir", ".", "工作目录（写出 effective.yaml 与 agent.log）")
		hostname    = flag.String("hostname", "", "上报主机名（默认取系统主机名）")
	)
	flag.Parse()

	if *instanceUID == "" || *initConfig == "" {
		fmt.Fprintln(os.Stderr, "instance-uid 与 config 必填")
		os.Exit(2)
	}
	uidBytes, err := parseInstanceUID(*instanceUID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "instance-uid 非法: %v\n", err)
		os.Exit(2)
	}
	initial, err := os.ReadFile(*initConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取初始配置失败: %v\n", err)
		os.Exit(2)
	}
	if *hostname == "" {
		*hostname, _ = os.Hostname()
	}
	if err := os.MkdirAll(*workdir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "创建 workdir 失败: %v\n", err)
		os.Exit(2)
	}
	cfgPath := filepath.Join(*workdir, "effective.yaml")
	if err := os.WriteFile(cfgPath, initial, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "写入初始配置失败: %v\n", err)
		os.Exit(2)
	}

	proc := &collectorProc{bin: *collector, cfgPath: cfgPath, logPath: filepath.Join(*workdir, "agent.log")}
	if err := proc.start(); err != nil {
		fmt.Fprintf(os.Stderr, "启动 collector 失败: %v\n", err)
		os.Exit(1)
	}
	defer proc.stop()

	logger := stdoutLogger{prefix: "supervisor"}
	opampClient := client.NewWebSocket(logger)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// 配置读写与重启互斥，避免并发写坏 effective.yaml。
	var cfgMu sync.Mutex
	applyRemoteConfig := func(rc *protobufs.AgentRemoteConfig) error {
		cfgMu.Lock()
		defer cfgMu.Unlock()
		body := remoteConfigBody(rc)
		if body == "" {
			return fmt.Errorf("远端配置为空")
		}
		if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
			return err
		}
		return proc.start() // 重写配置文件后重启子进程 = 真正生效
	}

	caps := protobufs.AgentCapabilities_AgentCapabilities_ReportsEffectiveConfig |
		protobufs.AgentCapabilities_AgentCapabilities_ReportsHealth |
		protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig |
		protobufs.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig

	callbacks := types.Callbacks{
		OnConnect: func(ctx context.Context) {
			fmt.Println("[supervisor] 已连接 OpAMP 服务端")
			_ = opampClient.SetHealth(healthyStatus(proc.pid()))
			_ = opampClient.UpdateEffectiveConfig(ctx)
		},
		OnConnectFailed: func(_ context.Context, err error) {
			fmt.Fprintf(os.Stderr, "[supervisor] 连接失败: %v\n", err)
		},
		OnMessage: func(ctx context.Context, msg *types.MessageData) {
			if msg.RemoteConfig == nil {
				return
			}
			status := &protobufs.RemoteConfigStatus{
				LastRemoteConfigHash: msg.RemoteConfig.ConfigHash,
				Status:               protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED,
			}
			if err := applyRemoteConfig(msg.RemoteConfig); err != nil {
				status.Status = protobufs.RemoteConfigStatuses_RemoteConfigStatuses_FAILED
				status.ErrorMessage = err.Error()
				fmt.Fprintf(os.Stderr, "[supervisor] 应用远端配置失败: %v\n", err)
			} else {
				fmt.Printf("[supervisor] 已应用远端配置 hash=%s\n", hex.EncodeToString(msg.RemoteConfig.ConfigHash))
			}
			_ = opampClient.SetRemoteConfigStatus(status)
			_ = opampClient.SetHealth(healthyStatus(proc.pid()))
			_ = opampClient.UpdateEffectiveConfig(ctx)
		},
		GetEffectiveConfig: func(_ context.Context) (*protobufs.EffectiveConfig, error) {
			cfgMu.Lock()
			defer cfgMu.Unlock()
			body, err := os.ReadFile(cfgPath)
			if err != nil {
				return nil, err
			}
			return &protobufs.EffectiveConfig{
				ConfigMap: &protobufs.AgentConfigMap{
					ConfigMap: map[string]*protobufs.AgentConfigFile{
						"": {Body: body, ContentType: "text/yaml"},
					},
				},
			}, nil
		},
		OnCommand: func(_ context.Context, cmd *protobufs.ServerToAgentCommand) error {
			fmt.Printf("[supervisor] 收到命令 type=%v，按当前配置文件重启\n", cmd.GetType())
			return proc.start()
		},
	}

	if err := opampClient.SetAgentDescription(&protobufs.AgentDescription{
		IdentifyingAttributes: []*protobufs.KeyValue{
			stringKV("service.name", "otelcol-contrib"),
			stringKV("service.version", collectorVersion(*collector)),
			stringKV("service.instance.id", *instanceUID),
		},
		NonIdentifyingAttributes: []*protobufs.KeyValue{
			stringKV("host.name", *hostname),
		},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "设置 AgentDescription 失败: %v\n", err)
		os.Exit(1)
	}
	// 注意顺序：opamp-go 要求先设置健康状态，再设置能力位（否则报 health is nil）。
	if err := opampClient.SetHealth(healthyStatus(proc.pid())); err != nil {
		fmt.Fprintf(os.Stderr, "设置健康状态失败: %v\n", err)
		os.Exit(1)
	}
	if err := opampClient.SetCapabilities(&caps); err != nil {
		fmt.Fprintf(os.Stderr, "设置能力位失败: %v\n", err)
		os.Exit(1)
	}

	var uidArr types.InstanceUid
	copy(uidArr[:], uidBytes)
	header := http.Header{}
	if *token != "" {
		header["Authorization"] = []string{"Bearer " + *token}
	}
	settings := types.StartSettings{
		OpAMPServerURL: *serverURL,
		Header:         header,
		InstanceUid:    uidArr,
		Callbacks:      callbacks,
	}
	if err := opampClient.Start(ctx, settings); err != nil {
		fmt.Fprintf(os.Stderr, "OpAMP Start 失败: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer stopCancel()
		_ = opampClient.Stop(stopCtx)
	}()

	fmt.Println("[supervisor] 运行中，Ctrl-C 退出")
	<-ctx.Done()
	fmt.Println("[supervisor] 退出")
}

// remoteConfigBody 取出远端配置单文件内容（优先空 key，其次唯一一项）。
func remoteConfigBody(rc *protobufs.AgentRemoteConfig) string {
	if rc == nil || rc.Config == nil {
		return ""
	}
	if f, ok := rc.Config.ConfigMap[""]; ok && f != nil {
		return string(f.Body)
	}
	for _, f := range rc.Config.ConfigMap {
		if f != nil {
			return string(f.Body)
		}
	}
	return ""
}

// healthyStatus 构造健康上报（含子进程 pid）.
func healthyStatus(pid int) *protobufs.ComponentHealth {
	status := "StatusOK"
	if pid == 0 {
		status = "StatusStarting"
	}
	return &protobufs.ComponentHealth{
		Healthy: pid != 0,
		Status:  status,
	}
}

// collectorVersion 读取 collector 二进制版本（best effort）。
func collectorVersion(bin string) string {
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		return "unknown"
	}
	fields := strings.Fields(string(out))
	if len(fields) >= 2 {
		return fields[1]
	}
	return strings.TrimSpace(string(out))
}

// parseInstanceUID 解析 UUID 字符串为 16 字节实例 UID（不引入 UUID 依赖）。
func parseInstanceUID(s string) ([]byte, error) {
	clean := strings.ReplaceAll(strings.TrimSpace(s), "-", "")
	if len(clean) != 32 {
		return nil, fmt.Errorf("UUID 长度应为 32 个十六进制字符（去连字符后），实际 %d", len(clean))
	}
	b, err := hex.DecodeString(clean)
	if err != nil {
		return nil, fmt.Errorf("UUID 含非十六进制字符: %w", err)
	}
	return b, nil
}

func stringKV(key, value string) *protobufs.KeyValue {
	return &protobufs.KeyValue{
		Key:   key,
		Value: &protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: value}},
	}
}
