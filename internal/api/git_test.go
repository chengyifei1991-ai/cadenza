// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/config"
	"github.com/chengyifei1991-ai/cadenza/internal/gitsource"
	"github.com/chengyifei1991-ai/cadenza/internal/store"
)

// initGitRepoForAPI 创建临时 git 仓库（含 collectors/<uid>.yaml 两次提交）。
func initGitRepoForAPI(t *testing.T) (dir, firstSHA string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("环境无 git，跳过 GitOps 接口测试")
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
	if err := os.WriteFile(filepath.Join(dir, "collectors", "demo-gateway-1.yaml"),
		[]byte(sampleYAML), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "first: 初版配置")
	firstSHA = run("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "collectors", "demo-gateway-1.yaml"),
		[]byte(strings.ReplaceAll(sampleYAML, "otlp", "otlp-b")), 0o644); err != nil {
		t.Fatalf("write2: %v", err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "second: 调整 receiver")
	return dir, firstSHA
}

// gitHandlers 构造启用 GitOps 的 Handlers（真实临时仓库）。
func gitHandlers(t *testing.T) (*Handlers, string) {
	t.Helper()
	h := newTestHandlers(t)
	dir, firstSHA := initGitRepoForAPI(t)
	repo, err := gitsource.New(dir)
	if err != nil {
		t.Fatalf("gitsource.New: %v", err)
	}
	h.git = repo
	h.configSource = "git"
	h.deps.Config = &config.Config{
		RequireApproval: true, OtelcolBin: "",
		ConfigSource: "git", GitRepoDir: dir,
		GitConfigPathspec: "collectors/{uid}.yaml", GitRef: "HEAD",
	}
	return h, firstSHA
}

// TestGitEndpointsDisabled 验证内置模式下 git 端点返回 409（明确"未启用"），
// 且 system/info 标注来源为 builtin。
func TestGitEndpointsDisabled(t *testing.T) {
	h := newTestHandlers(t) // 默认 builtin / git == nil
	for _, tc := range []struct {
		name string
		h    http.HandlerFunc
		path string
	}{
		{name: "status", h: h.GitStatus, path: "/api/v1/git/status"},
		{name: "commits", h: h.GitCommits, path: "/api/v1/git/commits?instance_uid=x"},
		{name: "file", h: h.GitFile, path: "/api/v1/git/file?instance_uid=x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, tc.h, http.MethodGet, tc.path, nil)
			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409 (body=%s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "未启用 GitOps 模式") {
				t.Errorf("应给出中文原因: %s", rec.Body.String())
			}
		})
	}
	rec := doJSON(t, SystemInfo(newTestAuth(t), false, false, "builtin", false), http.MethodGet, "/api/v1/system/info", nil)
	if body := rec.Body.String(); !strings.Contains(body, `"config_source":"builtin"`) || !strings.Contains(body, `"git_enabled":false`) {
		t.Errorf("system/info 应标注 builtin/git_enabled=false: %s", body)
	}
}

// TestGitEndpoints 验证 git/status|commits|file 的读取与参数校验。
func TestGitEndpoints(t *testing.T) {
	h, firstSHA := gitHandlers(t)

	// status
	rec := doJSON(t, http.HandlerFunc(h.GitStatus), http.MethodGet, "/api/v1/git/status", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, body=%s", rec.Code, rec.Body.String())
	}
	var st gitsource.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("解析 status 失败: %v", err)
	}
	if st.SHA == "" || st.Subject == "" {
		t.Errorf("status 内容不完整: %+v", st)
	}

	// commits（按 instance_uid 展开 path）
	rec = doJSON(t, http.HandlerFunc(h.GitCommits), http.MethodGet, "/api/v1/git/commits?instance_uid=demo-gateway-1&limit=10", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("commits code = %d, body=%s", rec.Code, rec.Body.String())
	}
	var commits struct {
		Items []gitsource.Commit `json:"items"`
		Total int                `json:"total"`
		Path  string             `json:"path"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &commits); err != nil {
		t.Fatalf("解析 commits 失败: %v", err)
	}
	if commits.Total != 2 || commits.Path != "collectors/demo-gateway-1.yaml" {
		t.Errorf("commits 不符: total=%d path=%q", commits.Total, commits.Path)
	}

	// file（当前 HEAD 与历史 commit 内容不同 → GitOps 回退的基础）
	rec = doJSON(t, http.HandlerFunc(h.GitFile), http.MethodGet, "/api/v1/git/file?instance_uid=demo-gateway-1", nil)
	headBody := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(headBody, "otlp-b") {
		t.Fatalf("file(HEAD) code=%d body=%s", rec.Code, headBody)
	}
	rec = doJSON(t, http.HandlerFunc(h.GitFile), http.MethodGet, "/api/v1/git/file?instance_uid=demo-gateway-1&ref="+firstSHA, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ref":"`+firstSHA+`"`) {
		t.Fatalf("file(历史 commit) code=%d body=%s", rec.Code, rec.Body.String())
	}

	// 参数校验：缺 path/instance_uid、路径上跳、未知 ref
	for _, tc := range []struct{ name, path string }{
		{name: "commits 缺参数", path: "/api/v1/git/commits"},
		{name: "file 缺参数", path: "/api/v1/git/file"},
		{name: "路径上跳", path: "/api/v1/git/file?path=../secret.yaml"},
		{name: "未知 ref", path: "/api/v1/git/file?instance_uid=demo-gateway-1&ref=no-such-ref"},
		{name: "limit 越界", path: "/api/v1/git/commits?instance_uid=demo-gateway-1&limit=999"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var handler http.HandlerFunc
			if strings.Contains(tc.path, "/commits") {
				handler = h.GitCommits
			} else {
				handler = h.GitFile
			}
			rec := doJSON(t, handler, http.MethodGet, tc.path, nil)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestApplyFromGit 验证 GitOps 模式：apply 只给 git_ref 即可从仓库读取内容并记录溯源。
func TestApplyFromGit(t *testing.T) {
	h, firstSHA := gitHandlers(t)
	ctx := context.Background()
	const uid = "demo-gateway-1"
	if err := h.store.UpsertCollector(ctx, &store.Collector{
		InstanceUID: uid, Hostname: "n1", Version: "0.156.0",
		LastSeenAt: time.Now().UTC(), Status: store.CollectorStatusHealthy,
		EffectiveConfig: sampleYAML,
	}); err != nil {
		t.Fatalf("UpsertCollector: %v", err)
	}

	// 只给 git_ref（不给 yaml）→ 内容来自仓库，并按 commit 记录溯源。
	rec := doJSON(t, http.HandlerFunc(h.ApplyTask), http.MethodPost, "/api/v1/tasks/apply",
		map[string]string{"collector_instance_uid": uid, "git_ref": firstSHA, "note": "gitops 下发"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("apply code = %d, body=%s", rec.Code, rec.Body.String())
	}
	var task store.Task
	if err := json.Unmarshal(rec.Body.Bytes(), &task); err != nil {
		t.Fatalf("解析任务失败: %v", err)
	}
	if task.GitCommit == "" {
		t.Errorf("git_commit 应为解析后的 sha，实际 %q", task.GitCommit)
	}
	if task.GitPath != "collectors/demo-gateway-1.yaml" || task.GitRef != firstSHA {
		t.Errorf("git 溯源不符: path=%q ref=%q", task.GitPath, task.GitRef)
	}
	if !strings.Contains(task.GeneratedYAML, "otlp") {
		t.Errorf("生成内容应来自 git: %q", task.GeneratedYAML[:min(40, len(task.GeneratedYAML))])
	}

	// 审计含 git 溯源。
	logs, _, err := h.store.ListAudit(ctx, store.AuditFilter{}, 0, 0)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	foundGit := false
	for _, l := range logs {
		if strings.Contains(l.Detail, "git 溯源 commit=") {
			foundGit = true
		}
	}
	if !foundGit {
		t.Errorf("审计应记录 git 溯源: %+v", logs)
	}

	// git_ref 缺失且无 yaml → 400。
	rec = doJSON(t, http.HandlerFunc(h.ApplyTask), http.MethodPost, "/api/v1/tasks/apply",
		map[string]string{"collector_instance_uid": uid})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("无 yaml 且无 git_ref 应 400，实际 %d", rec.Code)
	}

	// 未启用 GitOps 时传 git_ref → 409。
	plain := newTestHandlers(t)
	if err := plain.store.UpsertCollector(ctx, &store.Collector{
		InstanceUID: uid, Hostname: "n1", LastSeenAt: time.Now().UTC(), Status: store.CollectorStatusHealthy,
	}); err != nil {
		t.Fatalf("UpsertCollector(plain): %v", err)
	}
	rec = doJSON(t, http.HandlerFunc(plain.ApplyTask), http.MethodPost, "/api/v1/tasks/apply",
		map[string]string{"collector_instance_uid": uid, "git_ref": "HEAD"})
	if rec.Code != http.StatusConflict {
		t.Errorf("内置模式传 git_ref 应 409，实际 %d", rec.Code)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
