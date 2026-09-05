// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

// Package webui 以内嵌静态资源的形式托管 Web 控制台产物（生产单二进制部署）。
//
// 构建流程：web 产物先同步至 static/ 目录，再由 go:embed 打进二进制；
// 仓库内 static/index.html 为占位页，保证未构建前端时 go build 依然可编译。
package webui

import (
	"embed"
	"io/fs"
)

// static 内嵌 Web 控制台静态资源（含占位 index.html）。
//
//go:embed static
var static embed.FS

// Embedded 返回内嵌静态资源文件系统（static 子树），供 HTTP 静态路由使用。
func Embedded() (fs.FS, error) {
	return fs.Sub(static, "static")
}
