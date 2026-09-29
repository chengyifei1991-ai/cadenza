// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

// Package gitsource 提供 **只读** 的本地 git 仓库访问（GitOps 可选模式）。
//
// 设计要点：
//   - 零新依赖：调用系统 git CLI（`git -C <dir> ...`），不引入 Go git 库；
//   - 只读：仅使用 rev-parse / log / show 等读命令，绝不写入用户仓库；
//   - 安全：ref 与 pathspec 走白名单校验（拒绝 `..`、绝对路径、以 `-` 开头的参数注入），
//     命令统一带超时、禁用交互提示与可选锁，避免挂起与污染；
//   - 短 TTL 缓存：页面轮询命中缓存，避免频繁拉起 git 进程。
package gitsource

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultTimeout 是单次 git 命令的超时。
const DefaultTimeout = 10 * time.Second

// cacheTTL 是只读查询结果的缓存时长。
const cacheTTL = 10 * time.Second

// Commit 是一次提交的元数据。
type Commit struct {
	// SHA 是完整提交哈希。
	SHA string `json:"sha"`
	// ShortSHA 是短哈希（前 8 位）。
	ShortSHA string `json:"short_sha"`
	// Author 是作者名。
	Author string `json:"author"`
	// Date 是提交时间（RFC3339）。
	Date string `json:"date"`
	// Subject 是提交标题。
	Subject string `json:"subject"`
	// Path 是本次查询限定的文件路径（可空）。
	Path string `json:"path,omitempty"`
}

// Status 是仓库只读状态。
type Status struct {
	// Dir 是仓库根目录。
	Dir string `json:"dir"`
	// Ref 是当前解析到的 ref（默认为 HEAD）。
	Ref string `json:"ref"`
	// SHA 是该 ref 指向的提交。
	SHA string `json:"sha"`
	// ShortSHA 是短哈希。
	ShortSHA string `json:"short_sha"`
	// Author 是提交作者。
	Author string `json:"author"`
	// Subject 是该提交标题。
	Subject string `json:"subject"`
	// CommittedAt 是该提交时间。
	CommittedAt string `json:"committed_at"`
	// Dirty 表示工作区是否有未提交改动（只读 `status --porcelain`）。
	Dirty bool `json:"dirty"`
}

// Repo 是本地 git 仓库的只读访问器。
type Repo struct {
	dir     string
	timeout time.Duration

	mu       sync.Mutex
	cachedAt time.Time
	cached   *Status
}

// New 打开本地仓库并校验其确实是 git 仓库（失败即 fail-closed）。
func New(dir string) (*Repo, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("git 仓库目录为空")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("解析仓库路径失败: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("仓库目录不存在: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("仓库路径不是目录: %s", abs)
	}
	r := &Repo{dir: abs, timeout: DefaultTimeout}
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	out, err := r.run(ctx, "rev-parse", "--git-dir")
	if err != nil {
		return nil, fmt.Errorf("不是有效的 git 仓库（%s）: %w", abs, err)
	}
	if strings.TrimSpace(out) == "" {
		return nil, fmt.Errorf("不是有效的 git 仓库: %s", abs)
	}
	return r, nil
}

// Dir 返回仓库根目录。
func (r *Repo) Dir() string { return r.dir }

// StatusOf 返回指定 ref 的仓库状态（ref 为空时用 HEAD）；结果带短 TTL 缓存。
func (r *Repo) StatusOf(ctx context.Context, ref string) (*Status, error) {
	if ref == "" {
		ref = "HEAD"
	}
	if err := validateRef(ref); err != nil {
		return nil, err
	}
	if ref == "HEAD" {
		r.mu.Lock()
		if r.cached != nil && time.Since(r.cachedAt) < cacheTTL {
			cached := *r.cached
			r.mu.Unlock()
			return &cached, nil
		}
		r.mu.Unlock()
	}
	sha, err := r.Resolve(ctx, ref)
	if err != nil {
		return nil, err
	}
	// %H|%h|%an|%aI|%s
	out, err := r.run(ctx, "log", "-1", "--no-color", "--format=%H%x1f%h%x1f%an%x1f%aI%x1f%s", sha)
	if err != nil {
		return nil, err
	}
	parts := strings.SplitN(strings.TrimRight(out, "\n"), "\x1f", 5)
	if len(parts) < 5 {
		return nil, fmt.Errorf("git log 输出异常: %q", out)
	}
	st := &Status{
		Dir: r.dir, Ref: ref,
		SHA: parts[0], ShortSHA: parts[1], Author: parts[2], CommittedAt: parts[3], Subject: parts[4],
	}
	porcelain, err := r.run(ctx, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	st.Dirty = strings.TrimSpace(porcelain) != ""
	if ref == "HEAD" {
		r.mu.Lock()
		r.cached, r.cachedAt = st, time.Now()
		r.mu.Unlock()
	}
	return st, nil
}

// Resolve 把 ref 解析为完整 commit sha。
func (r *Repo) Resolve(ctx context.Context, ref string) (string, error) {
	if err := validateRef(ref); err != nil {
		return "", err
	}
	out, err := r.run(ctx, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("ref %q 无法解析为提交: %w", ref, err)
	}
	return strings.TrimSpace(out), nil
}

// Commits 返回某文件（path 为空表示全仓库）的历史提交，limit<=0 时取 20，上限 200。
func (r *Repo) Commits(ctx context.Context, ref, path string, limit int) ([]Commit, error) {
	if ref == "" {
		ref = "HEAD"
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	args := []string{"log", "--no-color", fmt.Sprintf("-n%d", limit), "--format=%H%x1f%h%x1f%an%x1f%aI%x1f%s", ref}
	if path != "" {
		if err := validatePathspec(path); err != nil {
			return nil, err
		}
		args = append(args, "--", path)
	}
	out, err := r.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	commits := make([]Commit, 0, limit)
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\x1f", 5)
		if len(parts) < 5 {
			continue
		}
		commits = append(commits, Commit{
			SHA: parts[0], ShortSHA: parts[1], Author: parts[2], Date: parts[3], Subject: parts[4], Path: path,
		})
	}
	return commits, nil
}

// ShowFile 读取指定 ref 下某文件的内容（只读）。
func (r *Repo) ShowFile(ctx context.Context, ref, path string) (string, error) {
	if ref == "" {
		ref = "HEAD"
	}
	if err := validateRef(ref); err != nil {
		return "", err
	}
	if err := validatePathspec(path); err != nil {
		return "", err
	}
	if path == "" {
		return "", errors.New("文件路径不能为空")
	}
	out, err := r.run(ctx, "show", ref+":"+path)
	if err != nil {
		return "", fmt.Errorf("读取 %s:%s 失败: %w", ref, path, err)
	}
	return out, nil
}

// run 执行只读 git 命令并返回标准输出。
func (r *Repo) run(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	full := append([]string{"-C", r.dir, "--no-pager"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...) //nolint:gosec // 参数经白名单校验
	// 非交互、不取可选锁（避免影响用户仓库上的其它 git 操作）。
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("git 命令超时（%s）", r.timeout)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return string(out), nil
}

// refPattern 是允许的 ref 字符集（分支/tag/短 sha/相对引用）。
var refPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/@^~{}-]*$`)

// validateRef 校验 ref：拒绝空值、以 `-` 开头（参数注入）、含空格或 `..`、超长。
func validateRef(ref string) error {
	if ref == "" {
		return errors.New("ref 不能为空")
	}
	if len(ref) > 256 {
		return errors.New("ref 过长")
	}
	if strings.Contains(ref, "..") || strings.ContainsAny(ref, " \t\n") {
		return fmt.Errorf("ref 含非法字符: %q", ref)
	}
	if !refPattern.MatchString(ref) {
		return fmt.Errorf("ref 格式非法: %q", ref)
	}
	return nil
}

// validatePathspec 校验文件路径：必须是仓库内相对路径，禁止上跳、绝对路径与 glob 逃逸。
func validatePathspec(path string) error {
	if path == "" {
		return nil
	}
	if len(path) > 512 {
		return errors.New("路径过长")
	}
	if filepath.IsAbs(path) || strings.HasPrefix(path, "/") {
		return fmt.Errorf("路径必须是仓库内相对路径: %q", path)
	}
	if strings.ContainsAny(path, "\n\t") {
		return fmt.Errorf("路径含非法字符: %q", path)
	}
	cleaned := filepath.Clean(path)
	if cleaned == ".." || strings.HasPrefix(cleaned, "..") {
		return fmt.Errorf("路径不得上跳仓库根: %q", path)
	}
	if strings.Contains(path, ":") {
		return fmt.Errorf("路径不得包含冒号（避免 ref:path 注入）: %q", path)
	}
	return nil
}

// ExpandPathspec 把配置中的模板展开为具体文件路径：支持 `{uid}` 与 `%s` 占位。
func ExpandPathspec(tmpl, uid string) (string, error) {
	if strings.TrimSpace(tmpl) == "" {
		return "", errors.New("未配置 git 配置文件路径（GIT_CONFIG_PATHSPEC）")
	}
	p := strings.ReplaceAll(tmpl, "{uid}", uid)
	p = strings.ReplaceAll(p, "%s", uid)
	if err := validatePathspec(p); err != nil {
		return "", err
	}
	return p, nil
}

// ParseLimit 解析可选的 limit 查询参数。
func ParseLimit(v string, def int) (int, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 200 {
		return 0, errors.New("limit 必须在 1~200 之间")
	}
	return n, nil
}
