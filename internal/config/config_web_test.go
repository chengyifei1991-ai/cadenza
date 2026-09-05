// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

// Package config 的 Web 配置校验测试（同包访问未导出 validateWeb）。
package config

import (
	"strings"
	"testing"
)

func validWebBase() Config {
	return Config{
		DBDriver:        "sqlite",
		LLM:             LLMConfig{BaseURL: "https://example.com", APIKey: "k"},
		RequireApproval: true,
		Web: WebConfig{
			AuthMode:          WebAuthModeSimple,
			AdminUser:         "admin",
			AdminPasswordHash: "$2a$10$abcdefghijklmnopqrstuv",
			DemoMode:          false,
		},
	}
}

// TestValidateWeb 表驱动校验 Web 配置组合。
func TestValidateWeb(t *testing.T) {
	hash := "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "simple 默认合法", mutate: func(c *Config) {
			c.Web.AuthMode = WebAuthModeSimple
			c.Web.AdminPasswordHash = hash
		}},
		{name: "simple 缺口令哈希应报错", mutate: func(c *Config) {
			c.Web.AuthMode = WebAuthModeSimple
			c.Web.AdminPasswordHash = ""
		}, wantErr: "WEB_ADMIN_PASSWORD_HASH is required"},
		{name: "simple 空用户名应报错", mutate: func(c *Config) {
			c.Web.AuthMode = WebAuthModeSimple
			c.Web.AdminPasswordHash = hash
			c.Web.AdminUser = ""
		}, wantErr: "WEB_ADMIN_USER must not be empty"},
		{name: "off 无需口令", mutate: func(c *Config) { c.Web.AuthMode = WebAuthModeOff }},
		{name: "simple + DISABLE_WEB 无需口令", mutate: func(c *Config) {
			c.Web.DisableWeb = true
			c.Web.AdminPasswordHash = ""
		}},
		{name: "非法模式应报错", mutate: func(c *Config) { c.Web.AuthMode = WebAuthMode("sso") },
			wantErr: "WEB_AUTH_MODE must be"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validWebBase()
			tc.mutate(&cfg)
			err := cfg.validateWeb()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validateWeb() err = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validateWeb() err = %v, want contains %q", err, tc.wantErr)
			}
		})
	}
}
