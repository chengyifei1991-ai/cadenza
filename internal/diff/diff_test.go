// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package diff

import (
	"strings"
	"testing"
)

func TestUnified(t *testing.T) {
	tests := []struct {
		name     string
		a, b     string
		want     string // 期望的完整 unified 文本（""=无差异）
		contains []string
	}{
		{
			name: "完全相同 → 空",
			a:    "receivers:\n  otlp:\n",
			b:    "receivers:\n  otlp:\n",
			want: "",
		},
		{
			name: "两者皆空 → 空",
			a:    "",
			b:    "",
			want: "",
		},
		{
			name: "新增一行",
			a:    "a\nb\n",
			b:    "a\nb\nc\n",
			want: "--- a\n+++ b\n@@ -1,2 +1,3 @@\n a\n b\n+c\n",
		},
		{
			name: "删除一行",
			a:    "a\nb\nc\n",
			b:    "a\nc\n",
			want: "--- a\n+++ b\n@@ -1,3 +1,2 @@\n a\n-b\n c\n",
		},
		{
			name: "修改一行",
			a:    "a\nold\nc\n",
			b:    "a\nnew\nc\n",
			want: "--- a\n+++ b\n@@ -1,3 +1,3 @@\n a\n-old\n+new\n c\n",
		},
		{
			name:     "空 → 内容（a 侧 0 行）",
			a:        "",
			b:        "x\ny\n",
			contains: []string{"@@ -0,0 +1,2 @@\n+x\n+y\n"},
		},
		{
			name:     "内容 → 空（b 侧 0 行）",
			a:        "x\ny\n",
			b:        "",
			contains: []string{"@@ -1,2 +0,0 @@\n-x\n-y\n"},
		},
		{
			name: "CRLF 归一化后视为相同",
			a:    "a\r\nb\r\n",
			b:    "a\nb\n",
			want: "",
		},
		{
			name:     "远距离两处变更拆成两个 hunk",
			a:        strings.Join([]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}, "\n") + "\n",
			b:        strings.Join([]string{"1x", "2", "3", "4", "5", "6", "7", "8", "9", "10x"}, "\n") + "\n",
			contains: []string{"@@ -1,4 +1,4 @@", "@@ -7,4 +7,4 @@"},
		},
		{
			name:     "无尾换行的末行也能比对",
			a:        "a\nb",
			b:        "a\nc",
			contains: []string{"-b\n", "+c\n"},
		},
		{
			name:     "大输入走降级路径仍标记变更",
			a:        strings.Repeat("line\n", 2100),
			b:        strings.Repeat("line\n", 2099) + "changed\n",
			contains: []string{"@@", "-line", "+changed"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Unified(tc.a, tc.b)
			if tc.want != "" || len(tc.contains) == 0 {
				if got != tc.want {
					t.Fatalf("Unified 不符\n--- got ---\n%s\n--- want ---\n%s", got, tc.want)
				}
				return
			}
			for _, sub := range tc.contains {
				if !strings.Contains(got, sub) {
					t.Fatalf("Unified 缺少 %q\n--- got ---\n%s", sub, got)
				}
			}
		})
	}
}

// TestUnifiedHunkCounts 验证 hunk 头部行号/行数计算（含上下文与合并）。
func TestUnifiedHunkCounts(t *testing.T) {
	a := "1\n2\n3\n4\n5\n6\n7\n8\n"
	b := "1\n2\n3\nX\n5\n6\n7\n8\n"
	got := Unified(a, b)
	want := "--- a\n+++ b\n@@ -1,7 +1,7 @@\n 1\n 2\n 3\n-4\n+X\n 5\n 6\n 7\n"
	if got != want {
		t.Fatalf("hunk 头/上下文不符\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
