// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

package config

import "testing"

// TestValidateGitOps 表驱动验证 GitOps 可选模式的配置校验（fail-closed）。
func TestValidateGitOps(t *testing.T) {
	base := func() *Config {
		return &Config{
			DBDriver: "sqlite",
			LLM:      LLMConfig{APIKey: "k", BaseURL: "http://x"},
			Web:      WebConfig{AuthMode: WebAuthModeOff},
		}
	}
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
		wantSrc string
	}{
		{name: "默认 builtin", mutate: func(c *Config) {}, wantSrc: "builtin"},
		{name: "显式 builtin", mutate: func(c *Config) { c.ConfigSource = "builtin" }, wantSrc: "builtin"},
		{
			name:    "git 缺仓库目录（fail-closed）",
			mutate:  func(c *Config) { c.ConfigSource = "git"; c.GitConfigPathspec = "collectors/{uid}.yaml" },
			wantErr: true,
		},
		{
			name:    "git 缺路径模板",
			mutate:  func(c *Config) { c.ConfigSource = "git"; c.GitRepoDir = "/tmp/repo" },
			wantErr: true,
		},
		{
			name: "git 配置齐全",
			mutate: func(c *Config) {
				c.ConfigSource = "git"
				c.GitRepoDir = "/tmp/repo"
				c.GitConfigPathspec = "collectors/{uid}.yaml"
			},
			wantSrc: "git",
		},
		{
			name:    "未知来源",
			mutate:  func(c *Config) { c.ConfigSource = "svn" },
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := base()
			tc.mutate(c)
			err := c.validateGitOps()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil（source=%q）", c.ConfigSource)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateGitOps: %v", err)
			}
			if c.ConfigSource != tc.wantSrc {
				t.Errorf("ConfigSource = %q, want %q", c.ConfigSource, tc.wantSrc)
			}
			if c.ConfigSource == "git" && c.GitRef == "" {
				t.Errorf("git 模式下 GitRef 应默认 HEAD")
			}
		})
	}
}

// TestLoadGitOpsEnv 验证环境变量装载（CONFIG_SOURCE/GIT_*）。
func TestLoadGitOpsEnv(t *testing.T) {
	t.Setenv("CONFIG_SOURCE", "git")
	t.Setenv("GIT_REPO_DIR", "/tmp/repo")
	t.Setenv("GIT_CONFIG_PATHSPEC", "collectors/{uid}.yaml")
	t.Setenv("GIT_REF", "release-1.2")
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("LLM_API_KEY", "k")
	t.Setenv("WEB_AUTH_MODE", "off")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ConfigSource != "git" || cfg.GitRepoDir != "/tmp/repo" ||
		cfg.GitConfigPathspec != "collectors/{uid}.yaml" || cfg.GitRef != "release-1.2" {
		t.Errorf("GitOps 配置装载不符: %+v", cfg)
	}
}
