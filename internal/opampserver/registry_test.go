// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package opampserver

import (
	"testing"

	"github.com/open-telemetry/opamp-go/protobufs"
)

func TestRegistryCapabilitiesAndHash(t *testing.T) {
	const (
		aUID = "aaaa00000000000000000000000000aa"
		bUID = "bbbb00000000000000000000000000bb"
	)
	const restartBit = uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRestartCommand)

	tests := []struct {
		name     string
		uid      string
		caps     uint64
		hash     string
		wantRst  bool
		wantHash string
	}{
		{name: "声明重启命令且上报哈希", uid: aUID, caps: restartBit, hash: "h1", wantRst: true, wantHash: "h1"},
		{name: "无重启能力且不上报", uid: bUID, caps: 0, hash: "", wantRst: false, wantHash: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRegistry(nil, 0)
			r.SetCapabilities(tc.uid, tc.caps)
			if tc.wantRst != r.AcceptsRestartCommand(tc.uid) {
				t.Errorf("AcceptsRestartCommand(%s) = %v, want %v", tc.uid, r.AcceptsRestartCommand(tc.uid), tc.wantRst)
			}
			if tc.hash != "" {
				r.SetReportedHash(tc.uid, tc.hash)
			}
			if got, ok := r.ReportedHash(tc.uid); tc.wantHash == "" {
				if ok {
					t.Errorf("ReportedHash(%s) 不应存在，got %q", tc.uid, got)
				}
			} else if !ok || got != tc.wantHash {
				t.Errorf("ReportedHash(%s) = %q,%v want %q", tc.uid, got, ok, tc.wantHash)
			}
			// Remove 应清理能力位与上报哈希（连接断开语义）。
			r.Remove(tc.uid)
			if r.AcceptsRestartCommand(tc.uid) {
				t.Errorf("Remove 后 AcceptsRestartCommand(%s) 应为 false", tc.uid)
			}
			if _, ok := r.ReportedHash(tc.uid); ok {
				t.Errorf("Remove 后 ReportedHash(%s) 应不存在", tc.uid)
			}
		})
	}
}
