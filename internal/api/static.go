// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package api

import (
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// SPAHandler 服务内嵌/磁盘静态资源，并对未命中文件的路径回退到 index.html，
// 以支持前端 History 路由（生产单二进制部署形态）。
//
// fallback 规则：
//   - 命中真实静态文件 → 原样返回；
//   - 未命中且路径末段**不带文件扩展名**（典型前端路由，如 /collectors/xxx）→ 回退 index.html；
//   - 未命中且末段带扩展名（缺失的 .js/.css/.svg 等资源）→ 404，避免浏览器拿到 HTML 报 MIME 错误。
//
// 注意：不能通过把请求路径改写为 "/index.html" 再交给 http.FileServer 的方式
// 实现 fallback —— FileServer 对以 /index.html 结尾的路径会 301 到 "./"，
// 这里直接以输出 index.html 内容实现。
func SPAHandler(root fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		// 直接命中静态文件 → 原样返回。
		if clean != "" {
			if f, err := fs.Stat(root, clean); err == nil && !f.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
			// 末段带扩展名的缺失文件 → 视为资源缺失，返回 404（不进 SPA fallback）。
			base := path.Base(clean)
			if strings.Contains(base, ".") {
				http.NotFound(w, r)
				return
			}
		}
		// SPA fallback：未命中文件（前端路由/根路径）→ 输出 index.html。
		f, err := root.Open("index.html")
		if err != nil {
			http.Error(w, "Cadenza Web UI 未构建：请执行 npm --prefix web run build && node web/scripts/embed.mjs 后重新编译。", http.StatusServiceUnavailable)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := io.Copy(w, f); err != nil {
			return
		}
	})
}
