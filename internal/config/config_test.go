package config

import (
	"testing"
	"time"
)

// TestLoad 表驱动测试环境变量加载与校验。
func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
		check   func(t *testing.T, c *Config)
	}{
		{
			name: "默认值加载",
			env:  map[string]string{"LLM_API_KEY": "sk-test"},
			check: func(t *testing.T, c *Config) {
				if c.HTTPAddr != ":8080" {
					t.Errorf("HTTPAddr = %q, want :8080", c.HTTPAddr)
				}
				if c.DBDriver != "sqlite" {
					t.Errorf("DBDriver = %q, want sqlite", c.DBDriver)
				}
				if c.LLM.BaseURL != "https://api.deepseek.com" {
					t.Errorf("BaseURL = %q", c.LLM.BaseURL)
				}
				if c.LLM.Retry != 3 || c.LLM.Timeout != 60*time.Second {
					t.Errorf("LLM 稳定性默认值错误: %+v", c.LLM)
				}
			},
		},
		{
			name: "自定义环境变量",
			env: map[string]string{
				"LLM_API_KEY":        "sk-1",
				"LLM_BACKUP_API_KEY": "sk-2",
				"LLM_BASE_URL":       "http://localhost:11434",
				"LLM_TIMEOUT":        "30s",
				"LLM_RETRY":          "5",
				"DB_DRIVER":          "mysql",
				"DB_DSN":             "user:pass@tcp(127.0.0.1:3306)/db",
				"STRICT_VALIDATE":    "true",
				"REQUIRE_APPROVAL":   "false",
			},
			check: func(t *testing.T, c *Config) {
				if c.LLM.Retry != 5 || c.LLM.Timeout != 30*time.Second {
					t.Errorf("自定义稳定性参数未生效: %+v", c.LLM)
				}
				if !c.StrictValidate || c.RequireApproval {
					t.Errorf("布尔配置未生效: strict=%v requireApproval=%v", c.StrictValidate, c.RequireApproval)
				}
			},
		},
		{
			name:    "缺 API Key 报错",
			env:     map[string]string{},
			wantErr: true,
		},
		{
			name: "非法 DB_DRIVER 报错",
			env: map[string]string{
				"LLM_API_KEY": "sk-test",
				"DB_DRIVER":   "oracle",
			},
			wantErr: true,
		},
		{
			name: "mysql 缺 DSN 报错",
			env: map[string]string{
				"LLM_API_KEY": "sk-test",
				"DB_DRIVER":   "mysql",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("LLM_API_KEY", "")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			// 清理可能影响的其他变量。
			for _, k := range []string{"LLM_BACKUP_API_KEY", "LLM_BASE_URL", "LLM_TIMEOUT", "LLM_RETRY", "DB_DSN", "STRICT_VALIDATE", "REQUIRE_APPROVAL"} {
				if _, ok := tt.env[k]; !ok {
					t.Setenv(k, "")
				}
			}
			cfg, err := Load()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Load() err = %v, wantErr = %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}
