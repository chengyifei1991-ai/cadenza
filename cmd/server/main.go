// opamp-backend 入口：装配 OpAMP Server、MCP Server、Agent 编排层
// 与 REST API，启动统一 HTTP 服务（设计方案 v3）。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chengyifei1991-ai/opamp-backend/internal/agent"
	"github.com/chengyifei1991-ai/opamp-backend/internal/api"
	"github.com/chengyifei1991-ai/opamp-backend/internal/config"
	"github.com/chengyifei1991-ai/opamp-backend/internal/mcp"
	"github.com/chengyifei1991-ai/opamp-backend/internal/opampserver"
	"github.com/chengyifei1991-ai/opamp-backend/internal/store"
	"github.com/chengyifei1991-ai/opamp-backend/internal/task"
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
	mcpSrv, err := mcp.NewServer("opamp-backend", "0.1.0", "/mcp", collectCallable(deps), logger)
	if err != nil {
		return err
	}

	// REST API 路由。
	handlers := api.NewHandlers(st, taskSvc, orch, deps, logger)
	router, err := api.NewRouter(opampSrv, mcpSrv, handlers, logger)
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
			"opamp", "/v1/opamp", "mcp", "/mcp", "rest", "/api/v1", "db", cfg.DBDriver)
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
