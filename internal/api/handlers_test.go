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
