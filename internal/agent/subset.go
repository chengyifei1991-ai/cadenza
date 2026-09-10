// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package agent

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// configSubset 判断 pushed 配置是否"已被 effective 覆盖"：即 Agent 上报的
// 生效配置中包含了 pushed 显式声明的全部键与取值（effective 允许携带
// otelcol 展开的默认值）。
//
// 用途：opampextension 等 Agent 不回传 remote config ack（无 RemoteConfigStatus），
// 只上报展开后的 effective config，无法做哈希比对——此时以"推送内容 ⊆ 上报内容"
// 作为生效判据。局限：仅删除键的变更无法据此识别（调用方配合重启命令与
// 上报时间戳兜底）。
func configSubset(pushed, effective string) bool {
	var p, e any
	if err := yaml.Unmarshal([]byte(pushed), &p); err != nil {
		return false
	}
	if err := yaml.Unmarshal([]byte(effective), &e); err != nil {
		return false
	}
	return yamlSubset(p, e)
}

// yamlSubset 递归实现子集判定：
//   - map：pushed 的每个键都需在 effective 中存在且递归满足；
//   - list：长度一致时按序比较，否则退化为"无序包含"；
//   - 标量：先严格相等，再退化为大小写无关/字面量比较（如 none 与 None）。
func yamlSubset(p, e any) bool {
	switch pv := p.(type) {
	case map[string]any:
		ev, ok := e.(map[string]any)
		if !ok {
			return false
		}
		for k, pvv := range pv {
			evv, ok := ev[k]
			if !ok {
				return false
			}
			if !yamlSubset(pvv, evv) {
				return false
			}
		}
		return true
	case []any:
		ev, ok := e.([]any)
		if !ok {
			return false
		}
		if len(pv) == 0 {
			return true
		}
		if len(ev) == len(pv) {
			all := true
			for i := range pv {
				if !yamlSubset(pv[i], ev[i]) {
					all = false
					break
				}
			}
			if all {
				return true
			}
		}
		// 无序包含兜底（otelcol 展开可能改变元素顺序）。
		for _, pvv := range pv {
			found := false
			for _, evv := range ev {
				if yamlSubset(pvv, evv) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	default:
		return scalarEqual(p, e)
	}
}

// redactedValue 是 otelcol 对敏感字段（configopaque）的上报占位值。
const redactedValue = "[REDACTED]"

// scalarEqual 比较标量：
//  1. Agent 上报的脱敏占位值（如 Authorization 头）对任意推送值视为匹配；
//  2. 严格相等；
//  3. 大小写无关（none/None、true/True 等表示差异）。
func scalarEqual(p, e any) bool {
	ps, pok := p.(string)
	es, eok := e.(string)
	if pok && es == redactedValue {
		return true
	}
	if eok && ps == redactedValue {
		return true
	}
	if fmt.Sprint(p) == fmt.Sprint(e) {
		return true
	}
	if pok && eok {
		return strings.EqualFold(ps, es)
	}
	return false
}
