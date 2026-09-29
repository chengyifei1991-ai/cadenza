// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package api

import (
	"net/http"
	"strings"

	"github.com/chengyifei1991-ai/cadenza/internal/gitsource"
)

// GitOps 可选模式（CONFIG_SOURCE=git）的只读查询端点。
//
// 未启用时统一返回 409 并给出中文原因——避免"以为在看 git，实际是内置模式"。

const gitDisabledMsg = "未启用 GitOps 模式（需 CONFIG_SOURCE=git 且配置 GIT_REPO_DIR/GIT_CONFIG_PATHSPEC）"

// requireGit 返回仓库访问器；未启用时写出 409 并返回 false。
func (h *Handlers) requireGit(w http.ResponseWriter) (*gitsource.Repo, bool) {
	repo, ok := h.gitRepo()
	if !ok {
		writeError(w, http.StatusConflict, gitDisabledMsg)
		return nil, false
	}
	return repo, true
}

// GitStatus 处理 GET /api/v1/git/status?ref=：
// 返回仓库当前 ref、HEAD 提交（sha/作者/时间/标题）与工作区是否脏（只读）。
func (h *Handlers) GitStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	repo, ok := h.requireGit(w)
	if !ok {
		return
	}
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	if ref == "" && h.deps != nil && h.deps.Config != nil {
		ref = h.deps.Config.GitRef
	}
	st, err := repo.StatusOf(r.Context(), ref)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// GitCommits 处理 GET /api/v1/git/commits?path=&ref=&limit=：
// 返回某文件（缺省为该 Collector 的配置模板展开路径需显式传 path）的历史提交。
func (h *Handlers) GitCommits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	repo, ok := h.requireGit(w)
	if !ok {
		return
	}
	q := r.URL.Query()
	limit, err := gitsource.ParseLimit(q.Get("limit"), 20)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ref := strings.TrimSpace(q.Get("ref"))
	if ref == "" && h.deps != nil && h.deps.Config != nil {
		ref = h.deps.Config.GitRef
	}
	path := strings.TrimSpace(q.Get("path"))
	// 未显式给 path 时，用配置模板按 instance_uid 展开（便于前端按 Collector 查历史）。
	if path == "" {
		uid := strings.TrimSpace(q.Get("instance_uid"))
		if uid == "" {
			writeError(w, http.StatusBadRequest, "需提供 path 或 instance_uid")
			return
		}
		if h.deps == nil || h.deps.Config == nil {
			writeError(w, http.StatusBadRequest, "缺少 git 配置")
			return
		}
		if path, err = gitsource.ExpandPathspec(h.deps.Config.GitConfigPathspec, uid); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	commits, err := repo.Commits(r.Context(), ref, path, limit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": commits, "total": len(commits), "path": path, "ref": ref})
}

// GitFile 处理 GET /api/v1/git/file?ref=&path=|instance_uid=：
// 返回指定 ref 下的配置文件内容（配置编辑器"从 git 载入"与 GitOps 下发共用）。
func (h *Handlers) GitFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	repo, ok := h.requireGit(w)
	if !ok {
		return
	}
	q := r.URL.Query()
	ref := strings.TrimSpace(q.Get("ref"))
	if ref == "" && h.deps != nil && h.deps.Config != nil {
		ref = h.deps.Config.GitRef
	}
	path := strings.TrimSpace(q.Get("path"))
	if path == "" {
		uid := strings.TrimSpace(q.Get("instance_uid"))
		if uid == "" {
			writeError(w, http.StatusBadRequest, "需提供 path 或 instance_uid")
			return
		}
		if h.deps == nil || h.deps.Config == nil {
			writeError(w, http.StatusBadRequest, "缺少 git 配置")
			return
		}
		var err error
		if path, err = gitsource.ExpandPathspec(h.deps.Config.GitConfigPathspec, uid); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	content, err := repo.ShowFile(r.Context(), ref, path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sha, err := repo.Resolve(r.Context(), ref)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ref": ref, "git_commit": sha, "path": path, "yaml": content,
	})
}
