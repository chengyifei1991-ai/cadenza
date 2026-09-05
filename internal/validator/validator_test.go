// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package validator

import (
	"strings"
	"testing"
)

// TestValidateYAML 表驱动测试第一级 YAML 校验。
func TestValidateYAML(t *testing.T) {
	validConfig := `receivers:
  otlp:
    protocols:
      grpc:
exporters:
  debug:
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [debug]
`
	tests := []struct {
		name     string
		yaml     string
		wantErr  bool
		wantWarn bool
	}{
		{"合法配置通过", validConfig, false, false},
		{"非法 YAML 语法报错", "receivers: [", true, false},
		{"空配置报错", "", true, false},
		{"缺 service 节报错", "receivers: {}", true, false},
		{"service 缺 pipelines 报错", "service: {}", true, false},
		{"缺 receivers 仅告警", "exporters:\n  debug:\nservice:\n  pipelines:\n    traces:\n      exporters: [debug]\n", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := validateYAML(tt.yaml)
			if (len(res.Errors) > 0) != tt.wantErr {
				t.Errorf("Errors = %v, wantErr = %v", res.Errors, tt.wantErr)
			}
			if (len(res.Warnings) > 0) != tt.wantWarn {
				t.Errorf("Warnings = %v, wantWarn = %v", res.Warnings, tt.wantWarn)
			}
		})
	}
}

// TestValidateNoOtelcol 验证未配置 otelcol 时优雅降级。
func TestValidateNoOtelcol(t *testing.T) {
	res := Validate("receivers: {}\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      exporters: [debug]\n", "", true)
	if !res.Valid {
		t.Errorf("降级校验应通过，Errors = %v", res.Errors)
	}
	if res.Method != "yaml" {
		t.Errorf("Method = %q, want yaml", res.Method)
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "降级") {
			found = true
		}
	}
	if !found {
		t.Errorf("应包含降级告警，Warnings = %v", res.Warnings)
	}
}

// TestValidateWithMissingOtelcol 验证 otelcol 二进制缺失时告警不阻断。
func TestValidateWithMissingOtelcol(t *testing.T) {
	res := Validate("receivers: {}\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      exporters: [debug]\n", "/nonexistent/otelcol-contrib", true)
	if !res.Valid {
		t.Errorf("二进制缺失应降级为通过，Errors = %v", res.Errors)
	}
}

// TestHash 验证哈希稳定且随内容变化。
func TestHash(t *testing.T) {
	a := Hash("receivers: {}")
	b := Hash("receivers: {}")
	if a != b {
		t.Errorf("相同输入哈希应一致: %s != %s", a, b)
	}
	if a == Hash("exporters: {}") {
		t.Errorf("不同输入哈希不应相同")
	}
	if len(a) != 64 {
		t.Errorf("sha256 hex 长度应为 64，got %d", len(a))
	}
}
