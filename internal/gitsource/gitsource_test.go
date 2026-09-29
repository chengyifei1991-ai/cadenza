// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package gitsource

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initRepo 创建一个临时 git 仓库，内含两次提交的配置文件。
func initRepo(t *testing.T) (dir string, firstSHA, secondSHA string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("环境无 git，跳过 gitsource 测试")
	}
	dir = t.TempDir()
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "main")
	if err := os.MkdirAll(filepath.Join(dir, "collectors"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "collectors", "demo.yaml"), []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	write("receivers:\n  otlp:\n    protocols:\n      grpc:\n        endpoint: 0.0.0.0:14341\n")
	run("add", ".")
	run("commit", "-q", "-m", "first: 初始配置")
	firstSHA = run("rev-parse", "HEAD")
	write("receivers:\n  otlp:\n    protocols:\n      grpc:\n        endpoint: 0.0.0.0:14342\n")
	run("add", ".")
	run("commit", "-q", "-m", "second: 调整端口")
	secondSHA = run("rev-parse", "HEAD")
	return dir, firstSHA, secondSHA
}

func TestNewAndStatus(t *testing.T) {
	dir, _, secondSHA := initRepo(t)
	r, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	st, err := r.StatusOf(context.Background(), "")
	if err != nil {
		t.Fatalf("StatusOf: %v", err)
	}
	if st.SHA != secondSHA {
		t.Errorf("HEAD sha = %q, want %q", st.SHA, secondSHA)
	}
	if st.ShortSHA == "" || !strings.HasPrefix(secondSHA, st.ShortSHA) {
		t.Errorf("短哈希不符: %q", st.ShortSHA)
	}
	if st.Author != "Test" {
		t.Errorf("author = %q, want Test", st.Author)
	}
	if !strings.Contains(st.Subject, "调整端口") {
		t.Errorf("subject = %q", st.Subject)
	}
	if st.Dirty {
		t.Errorf("干净仓库不应标记 dirty")
	}

	// 新增未提交文件 → dirty。
	if err := os.WriteFile(filepath.Join(dir, "collectors", "other.yaml"), []byte("x: 1\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	r2, err := New(dir) // 绕过 TTL 缓存
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	st2, err := r2.StatusOf(context.Background(), "")
	if err != nil {
		t.Fatalf("StatusOf: %v", err)
	}
	if !st2.Dirty {
		t.Errorf("有未提交文件时应标记 dirty")
	}
}

func TestNewRejectsNonRepo(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Error("空目录应报错")
	}
	if _, err := New(filepath.Join(t.TempDir(), "not-exist")); err == nil {
		t.Error("不存在的目录应报错")
	}
	plain := t.TempDir() // 非 git 仓库
	if _, err := New(plain); err == nil {
		t.Error("非 git 仓库目录应报错（fail-closed）")
	}
}

func TestCommitsAndShowFile(t *testing.T) {
	dir, firstSHA, secondSHA := initRepo(t)
	r, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()

	commits, err := r.Commits(ctx, "HEAD", "collectors/demo.yaml", 10)
	if err != nil {
		t.Fatalf("Commits: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("提交数 = %d, want 2", len(commits))
	}
	if commits[0].SHA != secondSHA || commits[1].SHA != firstSHA {
		t.Errorf("提交顺序不符: %s, %s", commits[0].SHA, commits[1].SHA)
	}
	if commits[0].Path != "collectors/demo.yaml" {
		t.Errorf("path 字段应回填: %q", commits[0].Path)
	}

	// 不同 ref 读到不同内容（GitOps 的"按 commit 回退"基础能力）。
	newest, err := r.ShowFile(ctx, "HEAD", "collectors/demo.yaml")
	if err != nil {
		t.Fatalf("ShowFile(HEAD): %v", err)
	}
	oldest, err := r.ShowFile(ctx, firstSHA, "collectors/demo.yaml")
	if err != nil {
		t.Fatalf("ShowFile(first): %v", err)
	}
	if !strings.Contains(newest, "14342") || !strings.Contains(oldest, "14341") {
		t.Errorf("按 ref 读取内容不符:\nnewest=%s\noldest=%s", newest, oldest)
	}
	if _, err := r.ShowFile(ctx, "HEAD", "collectors/missing.yaml"); err == nil {
		t.Error("读取不存在的文件应报错")
	}
	if _, err := r.Resolve(ctx, "no-such-ref"); err == nil {
		t.Error("不存在的 ref 应报错")
	}
}

func TestValidateRefAndPathspec(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		path    string
		wantErr bool
	}{
		{name: "正常 ref 与路径", ref: "main", path: "collectors/demo.yaml"},
		{name: "短 sha 与相对引用", ref: "abc1234", path: "a/b/c.yaml"},
		{name: "tag 形式", ref: "v1.2.3", path: "c.yaml"},
		{name: "空 ref 由调用方补 HEAD（此处仅校验函数）", ref: "HEAD", path: "c.yaml"},
		{name: "ref 以短横线开头（参数注入）", ref: "-upload-pack=x", wantErr: true},
		{name: "ref 含空格", ref: "ma in", wantErr: true},
		{name: "ref 含上跳", ref: "a..b", wantErr: true},
		{name: "路径绝对", path: "/etc/passwd", wantErr: true},
		{name: "路径上跳", path: "../secret.yaml", wantErr: true},
		{name: "路径含冒号（ref:path 注入）", path: "a:b.yaml", wantErr: true},
		{name: "路径含换行", path: "a\nb.yaml", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			refErr := validateRef(tc.ref)
			pathErr := validatePathspec(tc.path)
			gotErr := refErr != nil || pathErr != nil
			if gotErr != tc.wantErr {
				t.Fatalf("wantErr=%v, refErr=%v pathErr=%v", tc.wantErr, refErr, pathErr)
			}
		})
	}
}

func TestExpandPathspec(t *testing.T) {
	uid := "demo-gateway-1"
	tests := []struct {
		name    string
		tmpl    string
		want    string
		wantErr bool
	}{
		{name: "花括号占位", tmpl: "collectors/{uid}.yaml", want: "collectors/demo-gateway-1.yaml"},
		{name: "百分号占位", tmpl: "conf/%s.yaml", want: "conf/demo-gateway-1.yaml"},
		{name: "无占位", tmpl: "shared.yaml", want: "shared.yaml"},
		{name: "空模板", tmpl: "", wantErr: true},
		{name: "模板试图上跳", tmpl: "../{uid}.yaml", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExpandPathspec(tc.tmpl, uid)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExpandPathspec: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseLimit(t *testing.T) {
	if got, err := ParseLimit("", 20); err != nil || got != 20 {
		t.Errorf("默认值失败: %d, %v", got, err)
	}
	if got, err := ParseLimit("50", 20); err != nil || got != 50 {
		t.Errorf("解析失败: %d, %v", got, err)
	}
	for _, bad := range []string{"0", "201", "abc", "-1"} {
		if _, err := ParseLimit(bad, 20); err == nil {
			t.Errorf("%q 应报错", bad)
		}
	}
}
