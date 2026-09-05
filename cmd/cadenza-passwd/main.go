// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

// cadenza-passwd 生成 Web 管理员口令的 bcrypt 哈希（WEB_ADMIN_PASSWORD_HASH 配置用）。
//
// 用法：
//
//	cadenza-passwd 'mypassword'   # 命令行传入（注意 shell 历史）
//	printf 'mypassword\n' | cadenza-passwd   # 或从 stdin 读取
//
// 输出可直接粘贴到 .env 或作为 WEB_ADMIN_PASSWORD_HASH 环境变量。
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	var password string
	if len(os.Args) > 1 {
		password = os.Args[1]
	} else {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, "读取 stdin 失败:", err)
			os.Exit(1)
		}
		password = string(b)
	}
	password = strings.TrimSpace(password)
	if password == "" {
		fmt.Fprintln(os.Stderr, "口令不能为空（用法: cadenza-passwd '<password>'）")
		os.Exit(1)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fmt.Fprintln(os.Stderr, "生成哈希失败:", err)
		os.Exit(1)
	}
	fmt.Println(string(hash))
}
