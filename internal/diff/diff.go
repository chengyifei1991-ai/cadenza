// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

// Package diff 提供 stdlib 实现的**行级 unified diff**（不引入第三方依赖）。
//
// 用途：任务级 diff 服务端化——分组目标任务没有单一实例基准时，由服务端给出
// 基准配置与生成配置的差异文本，供 REST / MCP / 前端统一消费。
//
// 算法：最长公共子序列（LCS）动态规划；输入过大时退化为"整块替换"输出，
// 保证有界耗时与内存（配置文本通常在数百行量级）。
package diff

import (
	"fmt"
	"strings"
)

// contextLines 是 unified diff 的上下文行数（与 git 默认一致）。
const contextLines = 3

// maxDPCells 是 LCS 动态规划表的单元格上限，超过则走降级路径。
const maxDPCells = 4_000_000

// Unified 生成 a → b 的 unified diff 文本。
//
// 完全相同（含两者皆空）返回空串；行尾 \r\n 会被归一化为 \n。
func Unified(a, b string) string {
	al := splitLines(a)
	bl := splitLines(b)
	if len(al) == 0 && len(bl) == 0 {
		return ""
	}
	var ops []op
	if len(al)*len(bl) > maxDPCells {
		// 降级：整块替换（仍是合法 unified diff，只是上下文更粗）。
		for _, l := range al {
			ops = append(ops, op{kind: '-', text: l})
		}
		for _, l := range bl {
			ops = append(ops, op{kind: '+', text: l})
		}
	} else {
		ops = lcsOps(al, bl)
	}
	if !hasChange(ops) {
		return ""
	}
	return render(ops)
}

// op 是一次行级操作：' ' 保留、'-' 删除、'+' 新增。
type op struct {
	kind byte
	text string
}

// lcsOps 用 LCS 动态规划生成操作序列。
func lcsOps(a, b []string) []op {
	n, m := len(a), len(b)
	// dp[i][j] = a[i:] 与 b[j:] 的 LCS 长度。
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	ops := make([]op, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{kind: ' ', text: a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, op{kind: '-', text: a[i]})
			i++
		default:
			ops = append(ops, op{kind: '+', text: b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{kind: '-', text: a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, op{kind: '+', text: b[j]})
	}
	return ops
}

// hasChange 判断操作序列是否包含增删。
func hasChange(ops []op) bool {
	for _, o := range ops {
		if o.kind != ' ' {
			return true
		}
	}
	return false
}

// render 输出 unified 文本：文件头 + 合并后的 hunk。
func render(ops []op) string {
	var sb strings.Builder
	sb.WriteString("--- a\n+++ b\n")
	for _, h := range hunks(ops) {
		sb.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", h.aStart, h.aCount, h.bStart, h.bCount))
		for _, o := range h.ops {
			sb.WriteString(string(o.kind))
			sb.WriteString(o.text)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// hunk 是一个差异块及其在两侧的起止信息（1-based 起始行）。
type hunk struct {
	aStart, aCount int
	bStart, bCount int
	ops            []op
}

// hunks 按 contextLines 上下文合并相邻变更。
func hunks(ops []op) []hunk {
	var out []hunk
	// 变更行下标。
	changed := make([]int, 0, len(ops))
	for i, o := range ops {
		if o.kind != ' ' {
			changed = append(changed, i)
		}
	}
	if len(changed) == 0 {
		return nil
	}
	// 先确定 [start, end) 区间（含上下文的并集）。
	type span struct{ start, end int }
	var spans []span
	for _, idx := range changed {
		start := idx - contextLines
		if start < 0 {
			start = 0
		}
		end := idx + contextLines + 1
		if end > len(ops) {
			end = len(ops)
		}
		if len(spans) > 0 && start <= spans[len(spans)-1].end {
			if end > spans[len(spans)-1].end {
				spans[len(spans)-1].end = end
			}
			continue
		}
		spans = append(spans, span{start: start, end: end})
	}
	// 计算每块在 a/b 两侧的行号（按被消费的行计数）。
	aLine, bLine := 1, 1
	cursor := 0
	for _, sp := range spans {
		// 推进到块起点，累计 a/b 行号。
		for ; cursor < sp.start; cursor++ {
			switch ops[cursor].kind {
			case ' ':
				aLine++
				bLine++
			case '-':
				aLine++
			case '+':
				bLine++
			}
		}
		h := hunk{aStart: aLine, bStart: bLine}
		for ; cursor < sp.end; cursor++ {
			o := ops[cursor]
			h.ops = append(h.ops, o)
			switch o.kind {
			case ' ':
				h.aCount++
				h.bCount++
				aLine++
				bLine++
			case '-':
				h.aCount++
				aLine++
			case '+':
				h.bCount++
				bLine++
			}
		}
		// 与 git 惯例一致：某侧行数为 0 时，起始行取"变更前一行"（空侧即 0）。
		if h.aCount == 0 {
			h.aStart--
		}
		if h.bCount == 0 {
			h.bStart--
		}
		out = append(out, h)
	}
	return out
}

// splitLines 归一化行尾并拆分（末尾换行不产生空行）。
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		// 输入仅含换行：视为一个空行。
		return []string{""}
	}
	return strings.Split(s, "\n")
}
