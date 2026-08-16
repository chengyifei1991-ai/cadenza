package api

import (
	"log/slog"
	"net/http"

	"github.com/chengyifei1991-ai/opamp-backend/internal/mcp"
	"github.com/chengyifei1991-ai/opamp-backend/internal/opampserver"
	"github.com/open-telemetry/opamp-go/server"
)

// Router 装配全部 HTTP 路由。
type Router struct {
	opampHandler http.HandlerFunc
	connCtx      server.ConnContext
	mcpHandler   http.Handler
	handlers     *Handlers
	logger       *slog.Logger
}

// NewRouter 创建路由：opamp 提供 OpAMP handler 与 ConnContext。
func NewRouter(opamp *opampserver.Server, mcpSrv *mcp.Server, handlers *Handlers, logger *slog.Logger) (*Router, error) {
	handler, connCtx := opamp.Handler()
	return &Router{
		opampHandler: handler,
		connCtx:      connCtx,
		mcpHandler:   mcpSrv.Handler(),
		handlers:     handlers,
		logger:       logger,
	}, nil
}

// ConnContext 返回 OpAMP 连接上下文钩子，由 http.Server 装配。
func (r *Router) ConnContext() server.ConnContext { return r.connCtx }

// Handler 返回装配好的根 http.Handler。
func (r *Router) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/opamp", r.opampHandler)
	mux.Handle("/mcp", r.mcpHandler)
	mux.HandleFunc("/api/v1/sessions", r.handlers.CreateSession)
	mux.HandleFunc("/api/v1/chat", r.handlers.Chat)
	mux.HandleFunc("/api/v1/tasks/rollback", r.handlers.RollbackTask)
	mux.HandleFunc("/api/v1/tasks", r.handlers.ListTasks)
	mux.HandleFunc("/api/v1/tasks/", r.taskAction)
	mux.HandleFunc("/api/v1/collectors", r.handlers.ListCollectors)
	mux.HandleFunc("/api/v1/collectors/", r.collectorAction)
	mux.HandleFunc("/api/v1/audit", r.handlers.ListAudit)
	return logMiddleware(r.logger, mux)
}

// taskAction 分发 /api/v1/tasks/{id}[/approve|reject]。
func (r *Router) taskAction(w http.ResponseWriter, req *http.Request) {
	parts := splitPath(req.URL.Path)
	// 模式：/api/v1/tasks/{id} 或 /api/v1/tasks/{id}/{action}
	if len(parts) < 4 {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	id := parts[3]
	action := ""
	if len(parts) >= 5 {
		action = parts[4]
	}
	switch action {
	case "":
		r.handlers.GetTask(w, req, id)
	case "approve":
		r.handlers.ApproveTask(w, req, id)
	case "reject":
		r.handlers.RejectTask(w, req, id)
	default:
		writeError(w, http.StatusNotFound, "未知操作")
	}
}

// collectorAction 分发 /api/v1/collectors/{uid}/versions。
func (r *Router) collectorAction(w http.ResponseWriter, req *http.Request) {
	parts := splitPath(req.URL.Path)
	// 模式：/api/v1/collectors/{uid}/versions
	if len(parts) == 5 && parts[4] == "versions" {
		r.handlers.ListVersions(w, req, parts[3])
		return
	}
	writeError(w, http.StatusNotFound, "未知路径")
}

// logMiddleware 记录请求日志。
func logMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info("http", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

// splitPath 将路径按 '/' 拆分为非空段。
func splitPath(p string) []string {
	var parts []string
	start := 0
	for i := 0; i <= len(p); i++ {
		if i == len(p) || p[i] == '/' {
			if i > start {
				parts = append(parts, p[start:i])
			}
			start = i + 1
		}
	}
	return parts
}
