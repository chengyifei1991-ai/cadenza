// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

// Package ulid 提供不依赖第三方的唯一 ID 生成（毫秒时间前缀 + 随机后缀），
// 供任务、会话、演示数据等各处复用，保证排序性与唯一性。
package ulid

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// New 生成一个"类 ULID"的唯一 ID。
func New() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败时退回时间戳，保证不阻塞。
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x%s", time.Now().UnixMilli(), hex.EncodeToString(b))
}
