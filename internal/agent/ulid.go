// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package agent

import (
	"github.com/chengyifei1991-ai/cadenza/internal/ulid"
)

// NewULID 生成一个"类 ULID"的唯一 ID（实现委托 internal/ulid，保持调用方不变）。
func NewULID() string {
	return ulid.New()
}
