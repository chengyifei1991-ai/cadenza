// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

// Package config 从环境变量加载全部运行时配置。
//
// 敏感信息（LLM API Key、OpAMP 认证 token）一律通过环境变量注入，
// 禁止硬编码（项目宪章红线 4）。
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// WebAuthMode 是 Web 管理面的鉴权模式。
type WebAuthMode string

const (
	// WebAuthModeSimple 表示单管理员登录保护（安全默认值，口令哈希必填）。
	WebAuthModeSimple WebAuthMode = "simple"
	// WebAuthModeOff 表示免登（仅本地/演示环境，风险自负）。
	WebAuthModeOff WebAuthMode = "off"
)

// WebConfig 汇总 Web 控制台（前端阶段 v2）相关配置。
type WebConfig struct {
	// DisableWeb 为 true 时不注册静态路由（纯后端部署：OpAMP/MCP/REST）。
	DisableWeb bool
	// Dir 覆盖静态资源目录（可选）；为空时读取 go:embed 内嵌资源。
	Dir string

	// AuthMode 是 Web 鉴权模式（simple/off）。
	AuthMode WebAuthMode
	// AdminUser 是管理员用户名（simple 模式）。
	AdminUser string
	// AdminPasswordHash 是管理员口令的 bcrypt 哈希（simple 模式必填）。
	AdminPasswordHash string

	// CORSAllowedOrigins 是允许的跨域 Origin 列表（逗号分隔，默认空=同源）。
	CORSAllowedOrigins []string

	// DemoMode 为 true 时在空库注入演示数据（开箱体验）。
	DemoMode bool
}

// LLMConfig 汇总 LLM 接入与稳定性相关配置。
type LLMConfig struct {
	// BaseURL / APIKey / Model 是主模型（OpenAI 兼容，如 DeepSeek）。
	BaseURL string
	APIKey  string
	Model   string

	// BackupBaseURL / BackupAPIKey / BackupModel 是 failover 第二候选。
	BackupBaseURL string
	BackupAPIKey  string
	BackupModel   string

	// LocalBaseURL / LocalModel 是 failover 第三候选（本地兜底，如 Ollama）。
	LocalBaseURL string
	LocalModel   string

	// Timeout 是 LLM HTTP 客户端总超时。
	Timeout time.Duration
	// Retry 是可重试错误的指数退避重试次数。
	Retry int
	// RetryBase 是指数退避基数（1s → 2s → 4s）。
	RetryBase time.Duration
	// CircuitThreshold 是连续失败触发熔断的阈值。
	CircuitThreshold int
	// CircuitCooldown 是熔断后的冷却时长。
	CircuitCooldown time.Duration
	// CacheTTL 是配置生成结果缓存 TTL。
	CacheTTL time.Duration
}

// Config 汇总全部运行时配置。
type Config struct {
	// HTTPAddr 是主 HTTP 监听地址（承载 OpAMP / MCP / REST）。
	HTTPAddr string

	// DBDriver 是存储驱动：mysql 或 sqlite。
	DBDriver string
	// DBDSN 是 MySQL DSN（DBDriver=mysql 时使用）。
	DBDSN string
	// DBSQLitePath 是 SQLite 文件路径（DBDriver=sqlite 时使用）。
	DBSQLitePath string

	// OpAMPAuthToken 是 Collector 接入认证共享 token，为空则放行。
	OpAMPAuthToken string

	// OtelcolBin 是 otelcol-contrib 二进制路径（锁定 v0.156.0）。
	OtelcolBin string
	// StrictValidate 为 true 时深度校验失败强制阻断下发。
	StrictValidate bool

	// LLM 接入与稳定性配置。
	LLM LLMConfig

	// RequireApproval 控制 generate/optimize 任务默认是否要求审批。
	RequireApproval bool

	// Web 是 Web 控制台相关配置。
	Web WebConfig
}

// Load 从环境变量加载配置，返回缺失必需项的错误。
func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:        getEnv("HTTP_ADDR", ":8080"),
		DBDriver:        getEnv("DB_DRIVER", "sqlite"),
		DBDSN:           getEnv("DB_DSN", ""),
		DBSQLitePath:    getEnv("DB_SQLITE_PATH", "./data/opamp.db"),
		OpAMPAuthToken:  os.Getenv("OPAMP_AUTH_TOKEN"),
		OtelcolBin:      getEnv("OTELCOL_BIN", "/usr/local/bin/otelcol-contrib"),
		StrictValidate:  getBool("STRICT_VALIDATE", false),
		RequireApproval: getBool("REQUIRE_APPROVAL", true),
		Web: WebConfig{
			DisableWeb:         getBool("DISABLE_WEB", false),
			Dir:                os.Getenv("WEB_DIR"),
			AuthMode:           WebAuthMode(getEnv("WEB_AUTH_MODE", string(WebAuthModeSimple))),
			AdminUser:          getEnv("WEB_ADMIN_USER", "admin"),
			AdminPasswordHash:  os.Getenv("WEB_ADMIN_PASSWORD_HASH"),
			CORSAllowedOrigins: splitCSV(os.Getenv("CORS_ALLOWED_ORIGINS")),
			DemoMode:           getBool("DEMO_MODE", false),
		},
		LLM: LLMConfig{
			BaseURL:          getEnv("LLM_BASE_URL", "https://api.deepseek.com"),
			APIKey:           os.Getenv("LLM_API_KEY"),
			Model:            getEnv("LLM_MODEL", "deepseek-chat"),
			BackupBaseURL:    os.Getenv("LLM_BACKUP_BASE_URL"),
			BackupAPIKey:     os.Getenv("LLM_BACKUP_API_KEY"),
			BackupModel:      os.Getenv("LLM_BACKUP_MODEL"),
			LocalBaseURL:     os.Getenv("LLM_LOCAL_BASE_URL"),
			LocalModel:       os.Getenv("LLM_LOCAL_MODEL"),
			Timeout:          getDuration("LLM_TIMEOUT", 60*time.Second),
			Retry:            getInt("LLM_RETRY", 3),
			RetryBase:        getDuration("LLM_RETRY_BASE", time.Second),
			CircuitThreshold: getInt("LLM_CIRCUIT_THRESHOLD", 5),
			CircuitCooldown:  getDuration("LLM_CIRCUIT_COOLDOWN", 30*time.Second),
			CacheTTL:         getDuration("LLM_CACHE_TTL", 10*time.Minute),
		},
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validate 检查必需配置项。
func (c *Config) validate() error {
	if c.DBDriver != "mysql" && c.DBDriver != "sqlite" {
		return fmt.Errorf("config: DB_DRIVER must be \"mysql\" or \"sqlite\", got %q", c.DBDriver)
	}
	if c.DBDriver == "mysql" && c.DBDSN == "" {
		return fmt.Errorf("config: DB_DSN is required when DB_DRIVER=mysql")
	}
	if c.LLM.APIKey == "" && c.LLM.BackupAPIKey == "" {
		return fmt.Errorf("config: LLM_API_KEY (or LLM_BACKUP_API_KEY) is required")
	}
	if c.LLM.BaseURL == "" {
		return fmt.Errorf("config: LLM_BASE_URL is required")
	}
	if err := c.validateWeb(); err != nil {
		return err
	}
	return nil
}

// validateWeb 校验 Web 相关配置（安全默认值：simple 模式缺失口令哈希时启动失败）。
func (c *Config) validateWeb() error {
	switch c.Web.AuthMode {
	case WebAuthModeSimple:
		// 纯后端部署（无 Web）时不强制要求口令哈希。
		if c.Web.DisableWeb {
			return nil
		}
		if c.Web.AdminUser == "" {
			return fmt.Errorf("config: WEB_ADMIN_USER must not be empty")
		}
		if c.Web.AdminPasswordHash == "" {
			return fmt.Errorf("config: WEB_ADMIN_PASSWORD_HASH is required when WEB_AUTH_MODE=simple " +
				"(generate with: htpasswd -bnBC 10 \"\" '<password>' | tr -d ':\\n')")
		}
		return nil
	case WebAuthModeOff:
		return nil
	default:
		return fmt.Errorf("config: WEB_AUTH_MODE must be \"simple\" or \"off\", got %q", c.Web.AuthMode)
	}
}

// splitCSV 将逗号分隔的配置拆为去空格后的列表。
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func getDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
