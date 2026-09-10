// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/validator"
)

// fakePusher 记录调用并可注入行为。
type fakePusher struct {
	pushErr   error
	pushed    []string
	restarts  []string
	restartOK bool
	// onRestart 在收到重启命令时回调（用于模拟"重启后才回报新配置"的 Agent）。
	onRestart func(uid string)
}

func (f *fakePusher) PushConfig(_ context.Context, uid, _ string) error {
	f.pushed = append(f.pushed, uid)
	return f.pushErr
}

func (f *fakePusher) SendRestart(_ context.Context, uid string) bool {
	f.restarts = append(f.restarts, uid)
	if f.onRestart != nil {
		f.onRestart(uid)
	}
	return f.restartOK
}

// fakeReg 是 registryView 的可编程替身。
type fakeReg struct {
	online      map[string]bool
	acceptRst   map[string]bool
	reported    map[string]string
	reportedEff map[string]string
}

func newFakeReg() *fakeReg {
	return &fakeReg{
		online:      map[string]bool{},
		acceptRst:   map[string]bool{},
		reported:    map[string]string{},
		reportedEff: map[string]string{},
	}
}

func (f *fakeReg) Online(uid string) bool                { return f.online[uid] }
func (f *fakeReg) AcceptsRestartCommand(uid string) bool { return f.acceptRst[uid] }
func (f *fakeReg) ReportedHash(uid string) (string, bool) {
	h, ok := f.reported[uid]
	return h, ok
}

func (f *fakeReg) ReportedEffective(uid string) (string, bool) {
	c, ok := f.reportedEff[uid]
	return c, ok
}

func TestPushAndConfirm(t *testing.T) {
	const (
		onlineUID      = "online-accept-restart"
		onlineNoRstUID = "online-no-restart"
		offlineUID     = "offline"
		badUID         = "push-error"
	)
	yamlCfg := "receivers:\n  otlp:\n    protocols:\n      grpc:\n        endpoint: 0.0.0.0:14317\n"
	wantHash := validator.Hash(yamlCfg)

	// 立即回报正确哈希。
	confirmNow := newFakeReg()
	confirmNow.online[onlineUID] = true
	confirmNow.acceptRst[onlineUID] = true
	confirmNow.reported[onlineUID] = wantHash

	// 上报错误哈希（模拟 Agent 持有旧配置）→ 超时。
	stale := newFakeReg()
	stale.online[onlineUID] = true
	stale.acceptRst[onlineUID] = true
	stale.reported[onlineUID] = validator.Hash("old config")

	// 在线但不支持重启命令。
	noRst := newFakeReg()
	noRst.online[onlineNoRstUID] = true
	noRst.reported[onlineNoRstUID] = wantHash

	// 离线：不进入确认等待。
	off := newFakeReg()
	off.online[offlineUID] = false

	// 无 ack，但上报的 effective 已包含推送内容（opampextension 场景）→ 视为生效。
	effMatched := newFakeReg()
	effMatched.online[onlineUID] = true
	effMatched.acceptRst[onlineUID] = true
	effMatched.reportedEff[onlineUID] = "receivers:\n  otlp:\n    protocols:\n      grpc:\n        endpoint: 0.0.0.0:14317\n        read_buffer_size: 524288\n"

	// 无 ack 且上报内容仍是旧配置 → 超时。
	effStale := newFakeReg()
	effStale.online[onlineUID] = true
	effStale.acceptRst[onlineUID] = true
	effStale.reportedEff[onlineUID] = "receivers:\n  otlp:\n    protocols:\n      grpc:\n        endpoint: 0.0.0.0:9999\n"

	// 重启后才回报新配置（模拟需重启才换配置的 Agent）。
	restartThenAck := newFakeReg()
	restartThenAck.online[onlineUID] = true
	restartThenAck.acceptRst[onlineUID] = true
	restartPusher := &fakePusher{onRestart: func(uid string) {
		restartThenAck.reported[uid] = validator.Hash(yamlCfg)
	}}

	// PushConfig 报错。
	pushErrP := &fakePusher{pushErr: errors.New("send failed")}

	tests := []struct {
		name    string
		reg     registryView
		p       configPusher
		uid     string
		timeout time.Duration
		wantOK  bool
		wantErr bool
		wantRst bool
	}{
		{name: "在线+已确认：不发重启命令", reg: confirmNow, p: &fakePusher{}, uid: onlineUID, timeout: 500 * time.Millisecond, wantOK: true, wantRst: false},
		{name: "在线+不支持重启：无重启命令，生效确认", reg: noRst, p: &fakePusher{}, uid: onlineNoRstUID, timeout: 500 * time.Millisecond, wantOK: true, wantRst: false},
		{name: "离线：排队即受理，不等待", reg: off, p: &fakePusher{}, uid: offlineUID, timeout: 500 * time.Millisecond, wantOK: true, wantRst: false},
		{name: "生效回报不匹配：超时未确认", reg: stale, p: &fakePusher{}, uid: onlineUID, timeout: 50 * time.Millisecond, wantOK: false, wantRst: true},
		{name: "无 ack 但 effective 含推送内容：确认生效且不发重启", reg: effMatched, p: &fakePusher{}, uid: onlineUID, timeout: 500 * time.Millisecond, wantOK: true, wantRst: false},
		{name: "无 ack 且 effective 仍旧配置：超时未确认", reg: effStale, p: &fakePusher{}, uid: onlineUID, timeout: 50 * time.Millisecond, wantOK: false, wantRst: true},
		{name: "首段未确认+接受重启：补发重启命令后确认生效", reg: restartThenAck, p: restartPusher, uid: onlineUID, timeout: 600 * time.Millisecond, wantOK: true, wantRst: true},
		{name: "PushConfig 失败：返回错误", reg: off, p: pushErrP, uid: badUID, timeout: 50 * time.Millisecond, wantOK: false, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fp, _ := tc.p.(*fakePusher)
			if fp == nil {
				fp = &fakePusher{}
				tc.p = fp
			}
			ok, err := pushAndConfirm(context.Background(), tc.p, tc.reg, tc.uid, yamlCfg, tc.timeout)
			if tc.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if ok != tc.wantOK {
				t.Errorf("confirmed = %v, want %v", ok, tc.wantOK)
			}
			gotRst := len(fp.restarts) > 0 && fp.restarts[0] == tc.uid
			if gotRst != tc.wantRst {
				t.Errorf("SendRestart 调用 = %v, want %v (restarts=%v)", gotRst, tc.wantRst, fp.restarts)
			}
		})
	}
}

// TestPushAndConfirm_PushOrder 验证两段式的发送顺序与"已确认则不打扰"契约：
//  1. 已在首段确认 → 只发 RemoteConfig，不发重启命令；
//  2. 首段未确认且 Agent 接受重启命令 → 先 RemoteConfig 后单独 RestartCommand
//     （同一条消息带 Command 会令 opamp-go 客户端忽略 RemoteConfig）。
func TestPushAndConfirm_PushOrder(t *testing.T) {
	t.Run("首段已确认则不发重启", func(t *testing.T) {
		reg := newFakeReg()
		reg.online["u1"] = true
		reg.acceptRst["u1"] = true
		reg.reported["u1"] = validator.Hash("yaml: v")
		var events []string
		p := &orderedPusher{fn: func(kind, uid string) { events = append(events, kind+":"+uid) }}
		ok, err := pushAndConfirm(context.Background(), p, reg, "u1", "yaml: v", time.Second)
		if err != nil || !ok {
			t.Fatalf("pushAndConfirm = %v, %v", ok, err)
		}
		if got := strings.Join(events, ","); got != "push:u1" {
			t.Errorf("发送顺序错误: %s", got)
		}
	})

	t.Run("首段未确认则补发重启命令", func(t *testing.T) {
		reg := newFakeReg()
		reg.online["u1"] = true
		reg.acceptRst["u1"] = true
		var events []string
		p := &orderedPusher{fn: func(kind, uid string) { events = append(events, kind+":"+uid) }}
		ok, err := pushAndConfirm(context.Background(), p, reg, "u1", "yaml: v", 400*time.Millisecond)
		if err != nil {
			t.Fatalf("pushAndConfirm err = %v", err)
		}
		if ok {
			t.Errorf("未确认时应返回 false")
		}
		if got := strings.Join(events, ","); got != "push:u1,restart:u1" {
			t.Errorf("发送顺序错误: %s", got)
		}
	})
}

type orderedPusher struct {
	fn      func(kind, uid string)
	pushErr error
}

func (o *orderedPusher) PushConfig(_ context.Context, uid, _ string) error {
	o.fn("push", uid)
	return o.pushErr
}

func (o *orderedPusher) SendRestart(_ context.Context, uid string) bool {
	o.fn("restart", uid)
	return true
}
