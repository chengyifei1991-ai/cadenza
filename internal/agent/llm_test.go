package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// callResult 是一次模型调用的预设结果。
type callResult struct {
	resp *model.Response
	err  error
}

// fakeModel 是可编程的测试模型。调用次数超过预设序列时
// 循环使用最后一个结果（便于测试重试耗尽场景）。
type fakeModel struct {
	seq       []callResult
	callCount int32
}

func (f *fakeModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	i := int(atomic.AddInt32(&f.callCount, 1)) - 1
	idx := i
	if idx >= len(f.seq) {
		if len(f.seq) == 0 {
			return nil, errors.New("fakeModel: 无预设结果")
		}
		idx = len(f.seq) - 1
	}
	r := f.seq[idx]
	if r.err != nil {
		return nil, r.err
	}
	ch := make(chan *model.Response, 1)
	ch <- r.resp
	close(ch)
	return ch, nil
}

func (f *fakeModel) Info() model.Info { return model.Info{Name: "fake", ContextWindow: 4096} }

func okResponse(content string) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}},
		Done:    true,
	}
}

func errResponse(msg string) *model.Response {
	return &model.Response{Error: &model.ResponseError{Message: msg}}
}

// TestRetryableError 表驱动测试错误可重试性判定。
func TestRetryableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"超时错误可重试", errors.New("context deadline exceeded: connection timeout"), true},
		{"连接拒绝可重试", errors.New("dial tcp: connection refused"), true},
		{"限流可重试", errors.New("rate limit exceeded (429)"), true},
		{"服务端 503 可重试", errors.New("upstream 503 service unavailable"), true},
		{"参数错误不可重试", errors.New("invalid request: bad schema"), false},
		{"认证失败不可重试", errors.New("401 unauthorized"), false},
		{"nil 不可重试", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retryableError(tt.err); got != tt.want {
				t.Errorf("retryableError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestStripCodeFence 表驱动测试代码围栏剥离。
func TestStripCodeFence(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"带围栏剥离", "```yaml\nreceivers: {}\n```", "receivers: {}"},
		{"无围栏原样", "receivers: {}", "receivers: {}"},
		{"含解释文本提取围栏 YAML", "好的，这是配置：\n```\nservice: {}\n```", "service: {}"},
		{"无围栏含解释保留全文", "这是配置：\nservice: {}", "这是配置：\nservice: {}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripCodeFence(tt.in); got != tt.want {
				t.Errorf("stripCodeFence(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestRetryModel 验证可重试错误触发指数退避重试。
func TestRetryModel(t *testing.T) {
	fake := &fakeModel{
		seq: []callResult{{err: errors.New("connection refused")}, {resp: okResponse("ok")}},
	}
	r := &retryModel{inner: fake, maxRetries: 3, baseDelay: time.Millisecond}
	ch, err := r.GenerateContent(context.Background(), model.NewRequest(nil))
	if err != nil {
		t.Fatalf("GenerateContent 失败: %v", err)
	}
	var content string
	for resp := range ch {
		if resp.Error != nil {
			t.Fatalf("响应错误: %v", resp.Error)
		}
		if len(resp.Choices) > 0 {
			content += resp.Choices[0].Message.Content
		}
	}
	if content != "ok" {
		t.Errorf("content = %q, want ok", content)
	}
	if fake.callCount != 2 {
		t.Errorf("callCount = %d, want 2（一次失败 + 一次成功）", fake.callCount)
	}
}

// TestRetryModelExhausted 验证重试耗尽后返回错误。
func TestRetryModelExhausted(t *testing.T) {
	fake := &fakeModel{seq: []callResult{{err: errors.New("connection refused")}}}
	r := &retryModel{inner: fake, maxRetries: 2, baseDelay: time.Millisecond}
	_, err := r.GenerateContent(context.Background(), model.NewRequest(nil))
	if err == nil {
		t.Fatalf("重试耗尽应返回错误")
	}
	if fake.callCount != 3 {
		t.Errorf("callCount = %d, want 3（1 次原始 + 2 次重试）", fake.callCount)
	}
}

// TestCircuitModel 验证熔断：连续失败达阈值后打开。
func TestCircuitModel(t *testing.T) {
	fake := &fakeModel{
		seq: []callResult{
			{resp: errResponse("rate limit")}, // 失败 1
			{resp: errResponse("rate limit")}, // 失败 2
		},
	}
	c := &circuitModel{inner: fake, threshold: 2, cooldown: 50 * time.Millisecond}
	ctx := context.Background()

	// 两次失败后熔断。
	for i := 0; i < 2; i++ {
		if _, err := c.GenerateContent(ctx, model.NewRequest(nil)); err != nil {
			t.Fatalf("第 %d 次不应返回系统级错误: %v", i+1, err)
		}
	}
	// 第三次应被熔断拦截。
	if _, err := c.GenerateContent(ctx, model.NewRequest(nil)); !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("熔断未生效，err = %v", err)
	}
}

// TestCacheModel 验证相同请求命中缓存（LLM 仅调用一次）。
func TestCacheModel(t *testing.T) {
	fake := &fakeModel{seq: []callResult{{resp: okResponse("result-a")}}}
	c := &cacheModel{inner: fake, ttl: time.Minute}
	ctx := context.Background()
	req := model.NewRequest([]model.Message{{Role: model.RoleUser, Content: "生成配置"}})

	for i := 0; i < 2; i++ {
		ch, err := c.GenerateContent(ctx, req)
		if err != nil {
			t.Fatalf("第 %d 次失败: %v", i+1, err)
		}
		var content string
		for resp := range ch {
			if resp.Error != nil {
				t.Fatalf("响应错误: %v", resp.Error)
			}
			if len(resp.Choices) > 0 {
				content += resp.Choices[0].Message.Content
			}
		}
		if content != "result-a" {
			t.Errorf("第 %d 次 content = %q", i+1, content)
		}
	}
	if fake.callCount != 1 {
		t.Errorf("callCount = %d, want 1（第二次应命中缓存）", fake.callCount)
	}
}
