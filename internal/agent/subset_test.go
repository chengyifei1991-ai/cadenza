// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package agent

import "testing"

func TestConfigSubset(t *testing.T) {
	tests := []struct {
		name      string
		pushed    string
		effective string
		want      bool
	}{
		{
			name:      "完全相同",
			pushed:    "receivers:\n  otlp:\n    protocols:\n      grpc:\n        endpoint: 0.0.0.0:14317\n",
			effective: "receivers:\n  otlp:\n    protocols:\n      grpc:\n        endpoint: 0.0.0.0:14317\n",
			want:      true,
		},
		{
			name:   "effective 携带 otelcol 展开默认值",
			pushed: "processors:\n  batch:\n    timeout: 3s\n",
			effective: `processors:
  batch:
    timeout: 3s
    send_batch_size: 8192
    metadata_keys: []
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:14317
`,
			want: true,
		},
		{
			name:      "取值冲突（端口未切换）",
			pushed:    "receivers:\n  otlp:\n    protocols:\n      grpc:\n        endpoint: 0.0.0.0:14318\n",
			effective: "receivers:\n  otlp:\n    protocols:\n      grpc:\n        endpoint: 0.0.0.0:14317\n",
			want:      false,
		},
		{
			name:      "effective 缺少推送的键",
			pushed:    "processors:\n  memory_limiter:\n    limit_mib: 400\n",
			effective: "processors:\n  batch:\n    timeout: 3s\n",
			want:      false,
		},
		{
			name:      "枚举大小写差异（none vs None）",
			pushed:    "service:\n  telemetry:\n    metrics:\n      level: none\n",
			effective: "service:\n  telemetry:\n    metrics:\n      level: None\n",
			want:      true,
		},
		{
			name:      "列表顺序不同（无序包含兜底）",
			pushed:    "service:\n  extensions: [opamp, health_check]\n",
			effective: "service:\n  extensions: [health_check, opamp]\n",
			want:      true,
		},
		{
			name:      "敏感字段被 otelcol 脱敏上报",
			pushed:    "extensions:\n  opamp:\n    server:\n      ws:\n        headers:\n          Authorization: \"Bearer gate-token-1\"\n",
			effective: "extensions:\n  opamp:\n    server:\n      ws:\n        headers:\n          Authorization: '[REDACTED]'\n",
			want:      true,
		},
		{
			name:      "推送内容非法 YAML",
			pushed:    "receivers: [",
			effective: "receivers: {}\n",
			want:      false,
		},
		{
			name:      "空推送配置视为满足",
			pushed:    "{}\n",
			effective: "receivers: {}\n",
			want:      true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := configSubset(tc.pushed, tc.effective); got != tc.want {
				t.Errorf("configSubset = %v, want %v\npushed:\n%s\neffective:\n%s", got, tc.want, tc.pushed, tc.effective)
			}
		})
	}
}
