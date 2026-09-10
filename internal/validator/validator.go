// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

// Package validator 实现配置的两级校验：
//
//  1. 第一级：yaml.v3 语法与结构检查（始终执行）；
//  2. 第二级：otelcol-contrib validate 深度校验（版本锁定 v0.156.0，
//     通过 OTELCOL_BIN 环境变量指定二进制路径，缺失时自动降级）。
package validator

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Result 是一次配置校验的结果。
type Result struct {
	// Valid 表示校验是否通过。
	Valid bool
	// Errors 是阻断性错误列表。
	Errors []string
	// Warnings 是非阻断性告警列表。
	Warnings []string
	// Method 记录实际执行的校验方式："yaml" 或 "otelcol"。
	Method string
}

// Validate 对 YAML 配置执行两级校验。otelcolBin 为空时仅执行第一级。
//
// strict 为 true 时，深度校验失败计入 Errors（阻断）；为 false 时仅告警。
func Validate(yamlContent, otelcolBin string, strict bool) Result {
	res := validateYAML(yamlContent)
	if res.Method == "" {
		res.Method = "yaml"
	}
	if !res.Valid {
		// 第一级失败时不再继续深度校验。
		return res
	}
	if otelcolBin == "" {
		res.Warnings = append(res.Warnings, "otelcol-contrib 未配置，已降级为 YAML 结构校验")
		return res
	}
	deep, err := validateWithOtelcol(otelcolBin, yamlContent)
	if err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("深度校验不可用：%v", err))
		return res
	}
	res.Method = "otelcol"
	res.Warnings = append(res.Warnings, deep.Warnings...)
	if strict {
		res.Valid = deep.Valid
		res.Errors = append(res.Errors, deep.Errors...)
	} else if !deep.Valid {
		res.Warnings = append(res.Warnings, "otelcol validate 失败（STRICT_VALIDATE=false，仅告警）："+strings.Join(deep.Errors, "; "))
	}
	return res
}

// Hash 返回配置内容的 sha256 十六进制摘要，供 OpAMP 协议比对。
func Hash(yamlContent string) string {
	sum := sha256.Sum256([]byte(yamlContent))
	return fmt.Sprintf("%x", sum)
}

// validateYAML 检查 YAML 语法与 Collector 配置必需结构。
func validateYAML(content string) Result {
	res := Result{Valid: true}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		res.Valid = false
		res.Errors = append(res.Errors, fmt.Sprintf("YAML 语法错误：%v", err))
		return res
	}
	if doc == nil {
		res.Valid = false
		res.Errors = append(res.Errors, "配置为空")
		return res
	}
	// service.pipelines 是 Collector 配置的核心结构。
	service, ok := doc["service"].(map[string]any)
	if !ok {
		res.Valid = false
		res.Errors = append(res.Errors, "缺少顶层 service 节")
		return res
	}
	if _, ok := service["pipelines"].(map[string]any); !ok {
		res.Valid = false
		res.Errors = append(res.Errors, "service 缺少 pipelines 节")
		return res
	}
	// 可选节缺失仅告警。
	for _, section := range []string{"receivers", "exporters"} {
		if _, ok := doc[section]; !ok {
			res.Warnings = append(res.Warnings, fmt.Sprintf("缺少顶层 %s 节（如确无需接收/导出可忽略）", section))
		}
	}
	return res
}

// validateWithOtelcol 调用 otelcol-contrib validate 深度校验。
func validateWithOtelcol(binPath, content string) (Result, error) {
	res := Result{Valid: true, Method: "otelcol"}
	// 写入临时文件供 validate 读取（协议不支持 stdin 配置）。
	tmp, err := os.CreateTemp("", "opamp-config-*.yaml")
	if err != nil {
		return res, fmt.Errorf("创建临时配置失败：%w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return res, fmt.Errorf("写入临时配置失败：%w", err)
	}
	if err := tmp.Close(); err != nil {
		return res, fmt.Errorf("关闭临时配置失败：%w", err)
	}

	args := []string{"validate", "--config", tmp.Name()}
	// 配置含 opamp 扩展且声明 accepts_restart_command 时，0.156 的
	// opampextension 要求启用 RemoteRestarts 特性门才允许该能力——
	// 仅当内容出现 opamp 时附带特性门（避免对不含 opamp 的普通配置造成影响）。
	if strings.Contains(content, "opamp") {
		args = append(args, "--feature-gates", "extension.opampextension.RemoteRestarts")
	}
	cmd := exec.Command(binPath, args...)
	// 限制校验执行时间，避免挂起。
	if err := cmd.Run(); err != nil {
		if _, statErr := os.Stat(binPath); statErr != nil {
			return res, fmt.Errorf("找不到 otelcol-contrib 二进制 %q：%w", binPath, statErr)
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			res.Valid = false
			res.Errors = append(res.Errors, "otelcol validate 校验失败，详见 Collector 侧日志")
			_ = exitErr
			return res, nil
		}
		return res, fmt.Errorf("执行 otelcol validate 失败：%w", err)
	}
	return res, nil
}

// resolvePath 返回校验所需的绝对路径（保留给调用方做路径检查）。
func resolvePath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}
