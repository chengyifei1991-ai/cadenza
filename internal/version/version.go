// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

// Package version 集中定义产品版本号，供 REST /api/v1/system/info 与 MCP 等
// 对外能力使用，避免各处硬编码漂移。
package version

// Version 是当前产品版本号（语义化版本）。
const Version = "1.0.0-rc.1"
