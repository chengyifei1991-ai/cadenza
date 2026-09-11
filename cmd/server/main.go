// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

// cadenza 入口：装配 OpAMP Server、MCP Server、Agent 编排层、REST API
// 与内嵌 Web 控制台（前端阶段 v2），启动统一 HTTP 服务（设计方案 v3 / design-web-p0）。
package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/agent"
	"github.com/chengyifei1991-ai/cadenza/internal/api"
	"github.com/chengyifei1991-ai/cadenza/internal/config"
	"github.com/chengyifei1991-ai/cadenza/internal/demo"
	"github.com/chengyifei1991-ai/cadenza/internal/mcp"
	"github.com/chengyifei1991-ai/cadenza/internal/opampserver"
	"github.com/chengyifei1991-ai/cadenza/internal/store"
	"github.com/chengyifei1991-ai/cadenza/internal/task"
	"github.com/chengyifei1991-ai/cadenza/internal/version"
	"github.com/chengyifei1991-ai/cadenza/internal/webui"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("服务启动失败", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// 存储层。
	dsn := cfg.DBDSN
	if cfg.DBDriver == "sqlite" {
		dsn = cfg.DBSQLitePath
	}
	st, err := store.New(cfg.DBDriver, dsn)
	if err != nil {
		return err
	}
	defer st.Close()

	// 演示数据（DEMO_MODE=true 且库为空时注入）。
	if cfg.Web.DemoMode {
		if err := demo.Seed(context.Background(), st, logger); err != nil {
			logger.Warn("演示数据注入失败", "error", err)
		}
	}

	// 任务服务与 Collector 注册表。
	taskSvc := task.NewService(st)
	registry := opampserver.NewRegistry(st, 90*time.Second)

	// OpAMP 服务器。
	opampSrv, err := opampserver.NewServer(logger, st, registry, cfg.OpAMPAuthToken)
	if err != nil {
		return err
	}

	// LLM 稳定性装饰器链。
	llmModel, err := agent.NewStableModel(cfg.LLM)
	if err != nil {
		return err
	}
	deps := &agent.Deps{
		Store:    st,
		Tasks:    taskSvc,
		OpAMP:    opampSrv,
		Registry: registry,
		Model:    llmModel,
		Config:   cfg,
		Logger:   logger,
	}
	orch := agent.NewOrchestrator(deps)

	// MCP Server（工具与内置 Agent 共用同一套实现）。
	mcpSrv, err := mcp.NewServer("cadenza", version.Version, "/mcp", collectCallable(deps), logger)
	if err != nil {
		return err
	}

	// Web 管理面鉴权。
	authMgr, err := api.NewAuthManager(cfg, logger)
	if err != nil {
		return err
	}

	if cfg.MCPAuthToken == "" {
		logger.Warn("MCP 端点未配置鉴权 token（MCP_AUTH_TOKEN），外部 LLM/IDE 可调用 /mcp 触发审批；公网部署前请配置")
	}

	// REST API 路由。
	handlers := api.NewHandlers(st, taskSvc, orch, deps, logger)
	router, err := api.NewRouter(opampSrv, mcpSrv, handlers, api.RouterOptions{
		Auth:         authMgr,
		WebHandler:   buildWebHandler(cfg, logger),
		CORSOrigins:  cfg.Web.CORSAllowedOrigins,
		DemoMode:     cfg.Web.DemoMode,
		MCPAuthToken: cfg.MCPAuthToken,
		Logger:       logger,
	})
	if err != nil {
		return err
	}

	httpSrv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      router.Handler(),
		ConnContext:  router.ConnContext(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// 后台任务：定期标记离线 Collector。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go offlineLoop(ctx, registry)

	// 启动 HTTP 服务。
	errCh := make(chan error, 1)
	go func() {
		logger.Info("服务已启动", "addr", cfg.HTTPAddr,
			"opamp", "/v1/opamp", "mcp", "/mcp", "rest", "/api/v1", "db", cfg.DBDriver,
			"web_auth", cfg.Web.AuthMode, "demo", cfg.Web.DemoMode)
		if cfg.Web.DisableWeb {
			logger.Info("Web 控制台已禁用（DISABLE_WEB=true），仅提供 OpAMP/MCP/REST")
		} else {
			logger.Info("Web 控制台已启用", "url", "http://"+cfg.HTTPAddr)
		}
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		logger.Info("收到退出信号，正在优雅关闭")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	}
}

// buildWebHandler 构建 Web 静态资源处理器：优先磁盘目录（WEB_DIR 覆盖），
// 否则使用 go:embed 内嵌资源；DISABLE_WEB=true 时返回 nil（不注册静态路由）。
func buildWebHandler(cfg *config.Config, logger *slog.Logger) http.Handler {
	if cfg.Web.DisableWeb {
		return nil
	}
	var root fs.FS
	if cfg.Web.Dir != "" {
		root = os.DirFS(cfg.Web.Dir)
		logger.Info("Web 静态资源使用磁盘目录（WEB_DIR）", "dir", cfg.Web.Dir)
	} else {
		embedded, err := webui.Embedded()
		if err != nil {
			logger.Warn("读取内嵌 Web 资源失败，Web 路由不可用", "error", err)
			return nil
		}
		root = embedded
	}
	return api.SPAHandler(root)
}

// offlineLoop 定期将超时未上报的 Collector 标记为离线。
func offlineLoop(ctx context.Context, registry *opampserver.Registry) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			registry.MarkOffline(time.Now())
		}
	}
}

// collectCallable 收集 Deps 构建的全部可调用工具（MCP 注册用）。
func collectCallable(deps *agent.Deps) []tool.CallableTool {
	var out []tool.CallableTool
	for _, t := range agent.NewTools(deps) {
		if ct, ok := t.(tool.CallableTool); ok {
			out = append(out, ct)
		}
	}
	return out
}
