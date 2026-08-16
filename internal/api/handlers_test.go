package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chengyifei1991-ai/opamp-backend/internal/agent"
	"github.com/chengyifei1991-ai/opamp-backend/internal/config"
	"github.com/chengyifei1991-ai/opamp-backend/internal/opampserver"
	"github.com/chengyifei1991-ai/opamp-backend/internal/store"
	"github.com/chengyifei1991-ai/opamp-backend/internal/task"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// nopWriter 丢弃日志输出。
type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

// newTestHandlers 构建测试用 Handlers。
func newTestHandlers(t *testing.T) *Handlers {
	t.Helper()
	st, err := store.New("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("store.New 失败: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	logger := slog.New(slog.NewTextHandler(nopWriter{}, nil))

	taskSvc := task.NewService(st)
	registry := opampserver.NewRegistry(st, time.Minute)
	opampSrv, err := opampserver.NewServer(logger, st, registry, "")
	if err != nil {
		t.Fatalf("opampserver.NewServer 失败: %v", err)
	}
	fake := &fakeModel{seq: []agentCallResult{{resp: fakeResp("已创建生成任务，请审批")}}}
	deps := &agent.Deps{
		Store: st, Tasks: taskSvc, OpAMP: opampSrv, Model: fake,
		Config: &config.Config{RequireApproval: true, OtelcolBin: ""}, Logger: logger,
	}
	return NewHandlers(st, taskSvc, agent.NewOrchestrator(deps), deps, logger)
}

// fakeModel 与 agent 包测试相同的可编程模型（此处独立实现避免跨包依赖）。
type agentCallResult struct {
	resp *model.Response
	err  error
}

type fakeModel struct {
	seq       []agentCallResult
	callCount int
}

func (f *fakeModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	i := f.callCount
	f.callCount++
	idx := i
	if idx >= len(f.seq) {
		if len(f.seq) == 0 {
			return nil, errFakeExhausted
		}
		idx = len(f.seq) - 1
	}
	if f.seq[idx].err != nil {
		return nil, f.seq[idx].err
	}
	ch := make(chan *model.Response, 1)
	ch <- f.seq[idx].resp
	close(ch)
	return ch, nil
}

func (f *fakeModel) Info() model.Info { return model.Info{Name: "fake"} }

var errFakeExhausted = context.DeadlineExceeded

func fakeResp(content string) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}},
		Done:    true,
	}
}

// doJSON 发起 JSON 请求并返回响应。
func doJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("编码请求失败: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestSessionsAndChat 验证会话创建与对话接口。
func TestSessionsAndChat(t *testing.T) {
	h := newTestHandlers(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sessions", h.CreateSession)
	mux.HandleFunc("/api/v1/chat", h.Chat)

	rec := doJSON(t, mux, http.MethodPost, "/api/v1/sessions", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("创建会话 status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var sess struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); err != nil || sess.SessionID == "" {
		t.Fatalf("解析会话响应失败: %v, body = %s", err, rec.Body.String())
	}

	rec = doJSON(t, mux, http.MethodPost, "/api/v1/chat", map[string]any{
		"session_id": sess.SessionID, "message": "给 gateway 加 tail sampling",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("chat status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var chatResp struct {
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &chatResp); err != nil {
		t.Fatalf("解析 chat 响应失败: %v", err)
	}
	if chatResp.Reply == "" {
		t.Errorf("chat 回复为空")
	}
}

// TestChatEmptyMessage 验证空消息返回 400。
func TestChatEmptyMessage(t *testing.T) {
	h := newTestHandlers(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/chat", h.Chat)
	rec := doJSON(t, mux, http.MethodPost, "/api/v1/chat", map[string]any{"message": ""})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("空消息 status = %d, want 400", rec.Code)
	}
}

// TestListCollectorsAndAudit 验证 Collector 与审计查询接口。
func TestListCollectorsAndAudit(t *testing.T) {
	h := newTestHandlers(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/collectors", h.ListCollectors)
	mux.HandleFunc("/api/v1/audit", h.ListAudit)
	mux.HandleFunc("/api/v1/tasks", h.ListTasks)

	// 写入一个 Collector。
	ctx := context.Background()
	if err := h.store.UpsertCollector(ctx, &store.Collector{
		InstanceUID: "uid-1", Hostname: "node-1", LastSeenAt: time.Now().UTC(),
		Status: store.CollectorStatusHealthy,
	}); err != nil {
		t.Fatalf("UpsertCollector 失败: %v", err)
	}

	rec := doJSON(t, mux, http.MethodGet, "/api/v1/collectors", nil)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("uid-1")) {
		t.Errorf("collectors 响应异常: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, mux, http.MethodGet, "/api/v1/audit?since=0", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("audit status = %d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodGet, "/api/v1/tasks?status=done", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("tasks status = %d", rec.Code)
	}
}

// TestListTasksPagination 表驱动测试任务列表的分页/裸数组/非法参数三种模式。
func TestListTasksPagination(t *testing.T) {
	h := newTestHandlers(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tasks", h.ListTasks)
	ctx := context.Background()
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		if err := h.store.CreateTask(ctx, &store.Task{
			ID: "pgt-" + string(rune('a'+i)), Type: store.TaskTypeGenerate,
			Status: store.TaskStatusDone, RequireApproval: true,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateTask(%d) 失败: %v", i, err)
		}
	}
	tests := []struct {
		name     string
		path     string
		wantCode int
		check    func(t *testing.T, body []byte)
	}{
		{
			name: "无分页参数返回裸数组", path: "/api/v1/tasks",
			wantCode: http.StatusOK,
			check: func(t *testing.T, body []byte) {
				if bytes.HasPrefix(bytes.TrimSpace(body), []byte("[")) {
					return
				}
				t.Errorf("无分页参数应返回裸数组，got: %s", body[:min(len(body), 80)])
			},
		},
		{
			name: "分页返回结构化", path: "/api/v1/tasks?page=1&page_size=2",
			wantCode: http.StatusOK,
			check: func(t *testing.T, body []byte) {
				var resp struct {
					Items    []store.Task `json:"items"`
					Total    int64        `json:"total"`
					Page     int          `json:"page"`
					PageSize int          `json:"page_size"`
				}
				if err := json.Unmarshal(body, &resp); err != nil {
					t.Fatalf("解析失败: %v, body: %s", err, body[:min(len(body), 120)])
				}
				if resp.Total != 3 || resp.Page != 1 || resp.PageSize != 2 || len(resp.Items) != 2 {
					t.Errorf("分页响应不符: %+v", resp)
				}
			},
		},
		{
			name: "仅 page_size 也触发分页", path: "/api/v1/tasks?page_size=10",
			wantCode: http.StatusOK,
			check: func(t *testing.T, body []byte) {
				var resp pageResponse[store.Task]
				if err := json.Unmarshal(body, &resp); err != nil {
					t.Fatalf("应返回分页结构: %v", err)
				}
				if resp.Total != 3 || resp.Page != 1 || resp.PageSize != 10 {
					t.Errorf("分页响应不符: %+v", resp)
				}
			},
		},
		{
			name: "非法 page 返回 400", path: "/api/v1/tasks?page=abc",
			wantCode: http.StatusBadRequest,
		},
		{
			name: "非法 page_size 返回 400", path: "/api/v1/tasks?page_size=0",
			wantCode: http.StatusBadRequest,
		},
		{
			name: "page_size 超上限返回 400", path: "/api/v1/tasks?page_size=999",
			wantCode: http.StatusBadRequest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doJSON(t, mux, http.MethodGet, tt.path, nil)
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d, body: %s", rec.Code, tt.wantCode, rec.Body.String()[:min(rec.Body.Len(), 120)])
			}
			if tt.check != nil {
				tt.check(t, rec.Body.Bytes())
			}
		})
	}
}

// TestPageParams 表驱动测试分页参数解析边界。
func TestPageParams(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		wantEnabled bool
		wantPage    int
		wantSize    int
		wantErr     bool
	}{
		{"无参数", "", false, 0, 0, false},
		{"仅 page", "page=3", true, 3, 20, false},
		{"仅 page_size", "page_size=50", true, 1, 50, false},
		{"两者齐全", "page=2&page_size=10", true, 2, 10, false},
		{"page 非法", "page=0", true, 0, 0, true},
		{"page 非数字", "page=x", true, 0, 0, true},
		{"page_size 非法", "page_size=-1", true, 0, 0, true},
		{"page_size 超上限", "page_size=101", true, 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?"+tt.query, nil)
			page, size, enabled, err := pageParams(r)
			if enabled != tt.wantEnabled {
				t.Errorf("enabled = %v, want %v", enabled, tt.wantEnabled)
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if enabled && (page != tt.wantPage || size != tt.wantSize) {
				t.Errorf("page=%d size=%d, want %d/%d", page, size, tt.wantPage, tt.wantSize)
			}
		})
	}
}
