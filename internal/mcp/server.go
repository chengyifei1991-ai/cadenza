// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

// Package mcp 基于 trpc-mcp-go 构建 MCP Server，将 Agent 工具集
// 暴露为 MCP 工具（stdio/SSE/streamable HTTP，随主 HTTP 端口挂载）。
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	trpcagenttool "trpc.group/trpc-go/trpc-agent-go/tool"
	mcpgo "trpc.group/trpc-go/trpc-mcp-go"
)

// Server 是 MCP Server 的封装。
type Server struct {
	srv *mcpgo.Server
}

// NewServer 创建 MCP Server 并注册全部工具。
// path 是 HTTP 前缀（如 "/mcp"）。
func NewServer(name, version, path string, tools []trpcagenttool.CallableTool, logger *slog.Logger) (*Server, error) {
	srv := mcpgo.NewServer(name, version,
		mcpgo.WithServerPath(path),
		mcpgo.WithServerLogger(slogAdapter{logger: logger}),
	)
	for _, t := range tools {
		mt, handler, err := toMCPTool(t)
		if err != nil {
			return nil, fmt.Errorf("mcp: 转换工具 %q 失败: %w", t.Declaration().Name, err)
		}
		srv.RegisterTool(mt, handler)
	}
	return &Server{srv: srv}, nil
}

// Handler 返回可挂载到主 HTTP 服务器的 MCP handler。
func (s *Server) Handler() http.Handler {
	return s.srv.Handler()
}

// toMCPTool 将 trpc-agent-go 的 CallableTool 适配为 trpc-mcp-go 的
// Tool + toolHandler。
func toMCPTool(t trpcagenttool.CallableTool) (*mcpgo.Tool, func(context.Context, *mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error), error) {
	decl := t.Declaration()
	mt := mcpgo.NewTool(decl.Name, mcpgo.WithDescription(decl.Description))
	if decl.InputSchema != nil {
		mt.InputSchema = convertSchema(decl.InputSchema)
	}
	handler := func(ctx context.Context, req *mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args, err := json.Marshal(req.Params.Arguments)
		if err != nil {
			return mcpgo.NewErrorResult(fmt.Sprintf("参数序列化失败: %v", err)), nil
		}
		result, err := t.Call(ctx, args)
		if err != nil {
			return mcpgo.NewErrorResult(err.Error()), nil
		}
		payload, err := json.Marshal(result)
		if err != nil {
			return mcpgo.NewErrorResult(fmt.Sprintf("结果序列化失败: %v", err)), nil
		}
		return mcpgo.NewTextResult(string(payload)), nil
	}
	return mt, handler, nil
}

// convertSchema 将 trpc-agent-go 的 tool.Schema 转换为 openapi3.Schema。
func convertSchema(s *trpcagenttool.Schema) *openapi3.Schema {
	types := openapi3.Types{}
	switch s.Type {
	case "object":
		types = openapi3.Types{openapi3.TypeObject}
	case "string":
		types = openapi3.Types{openapi3.TypeString}
	case "array":
		types = openapi3.Types{openapi3.TypeArray}
	case "number":
		types = openapi3.Types{openapi3.TypeNumber}
	case "integer":
		types = openapi3.Types{openapi3.TypeInteger}
	case "boolean":
		types = openapi3.Types{openapi3.TypeBoolean}
	}
	sch := &openapi3.Schema{
		Type:        &types,
		Description: s.Description,
		Required:    s.Required,
	}
	if s.Type == "object" && len(s.Properties) > 0 {
		sch.Properties = openapi3.Schemas{}
		for name, prop := range s.Properties {
			sch.Properties[name] = &openapi3.SchemaRef{Value: convertSchema(prop)}
		}
	}
	if s.Type == "array" && s.Items != nil {
		sch.Items = &openapi3.SchemaRef{Value: convertSchema(s.Items)}
	}
	return sch
}

// slogAdapter 将 *slog.Logger 适配为 trpc-mcp-go 的 Logger 接口。
type slogAdapter struct {
	logger *slog.Logger
}

// Debug 实现 Logger 接口。
func (a slogAdapter) Debug(args ...any) {
	if a.logger != nil {
		a.logger.Debug(fmt.Sprint(args...))
	}
}

// Debugf 实现 Logger 接口。
func (a slogAdapter) Debugf(format string, v ...any) {
	if a.logger != nil {
		a.logger.Debug(fmt.Sprintf(format, v...))
	}
}

// Info 实现 Logger 接口。
func (a slogAdapter) Info(args ...any) {
	if a.logger != nil {
		a.logger.Info(fmt.Sprint(args...))
	}
}

// Infof 实现 Logger 接口。
func (a slogAdapter) Infof(format string, v ...any) {
	if a.logger != nil {
		a.logger.Info(fmt.Sprintf(format, v...))
	}
}

// Warn 实现 Logger 接口。
func (a slogAdapter) Warn(args ...any) {
	if a.logger != nil {
		a.logger.Warn(fmt.Sprint(args...))
	}
}

// Warnf 实现 Logger 接口。
func (a slogAdapter) Warnf(format string, v ...any) {
	if a.logger != nil {
		a.logger.Warn(fmt.Sprintf(format, v...))
	}
}

// Error 实现 Logger 接口。
func (a slogAdapter) Error(args ...any) {
	if a.logger != nil {
		a.logger.Error(fmt.Sprint(args...))
	}
}

// Errorf 实现 Logger 接口。
func (a slogAdapter) Errorf(format string, v ...any) {
	if a.logger != nil {
		a.logger.Error(fmt.Sprintf(format, v...))
	}
}

// Fatal 实现 Logger 接口。
func (a slogAdapter) Fatal(args ...any) {
	if a.logger != nil {
		a.logger.Error(fmt.Sprint(args...))
	}
}

// Fatalf 实现 Logger 接口。
func (a slogAdapter) Fatalf(format string, v ...any) {
	if a.logger != nil {
		a.logger.Error(fmt.Sprintf(format, v...))
	}
}
