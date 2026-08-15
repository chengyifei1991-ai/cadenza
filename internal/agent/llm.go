// Package agent 实现智能体编排层：LLM 稳定性包装、OpAMP 管控工具集、
// 对话生成/自动优化编排（设计方案 v3 第 4 节）。
package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chengyifei1991-ai/opamp-backend/internal/config"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/failover"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
)

// ErrCircuitOpen 表示熔断器处于打开状态（LLM 链路不可用）。
var ErrCircuitOpen = errors.New("agent: LLM 熔断器已打开，链路暂不可用")

// NewStableModel 构建 LLM 稳定性装饰器链（由外到内）：
//
//	cache → circuit → retry → failover → openai(主/备/本地)
//
// 全部实现 model.Model 接口，可整体注入 llmagent 或编排循环。
func NewStableModel(cfg config.LLMConfig) (model.Model, error) {
	var candidates []model.Model
	main := openai.New(cfg.Model,
		openai.WithBaseURL(cfg.BaseURL),
		openai.WithAPIKey(cfg.APIKey),
		openai.WithHTTPClientOptions(model.WithHTTPClientTimeout(cfg.Timeout)),
	)
	candidates = append(candidates, main)

	if cfg.BackupBaseURL != "" && cfg.BackupAPIKey != "" {
		name := cfg.BackupModel
		if name == "" {
			name = cfg.Model
		}
		backup := openai.New(name,
			openai.WithBaseURL(cfg.BackupBaseURL),
			openai.WithAPIKey(cfg.BackupAPIKey),
			openai.WithHTTPClientOptions(model.WithHTTPClientTimeout(cfg.Timeout)),
		)
		candidates = append(candidates, backup)
	}
	if cfg.LocalBaseURL != "" {
		name := cfg.LocalModel
		if name == "" {
			name = "local-model"
		}
		local := openai.New(name,
			openai.WithBaseURL(cfg.LocalBaseURL),
			openai.WithHTTPClientOptions(model.WithHTTPClientTimeout(cfg.Timeout)),
		)
		candidates = append(candidates, local)
	}

	var m model.Model
	if len(candidates) == 1 {
		m = candidates[0]
	} else {
		fo, err := failover.New(failover.WithCandidates(candidates...))
		if err != nil {
			return nil, fmt.Errorf("agent: 创建 failover 失败: %w", err)
		}
		m = fo
	}

	m = &retryModel{inner: m, maxRetries: cfg.Retry, baseDelay: cfg.RetryBase}
	m = &circuitModel{
		inner:           m,
		threshold:       cfg.CircuitThreshold,
		cooldown:        cfg.CircuitCooldown,
		failureObserver: func(error) {},
	}
	if cfg.CacheTTL > 0 {
		m = &cacheModel{inner: m, ttl: cfg.CacheTTL}
	}
	return m, nil
}

// retryModel 对可重试错误执行指数退避重试。
type retryModel struct {
	inner      model.Model
	maxRetries int
	baseDelay  time.Duration
}

// GenerateContent 实现 model.Model，重试仅针对首响应前的失败。
func (r *retryModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	delay := r.baseDelay
	var lastErr error
	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		ch, err := r.inner.GenerateContent(ctx, request)
		if err != nil {
			lastErr = err
			if !retryableError(err) {
				return nil, err
			}
			if attempt < r.maxRetries {
				if !sleepCtx(ctx, delay) {
					return nil, ctx.Err()
				}
				delay *= 2
				continue
			}
			return nil, fmt.Errorf("agent: LLM 重试 %d 次后仍失败: %w", r.maxRetries, lastErr)
		}
		first, ok := <-ch
		if !ok {
			return nil, errors.New("agent: LLM 响应流提前关闭")
		}
		if first.Error != nil {
			drainStream(ch)
			if retryableError(errors.New(first.Error.Message)) && attempt < r.maxRetries {
				if !sleepCtx(ctx, delay) {
					return nil, ctx.Err()
				}
				delay *= 2
				continue
			}
			return singleStream(first), nil
		}
		return prependStream(first, ch), nil
	}
	return nil, lastErr
}

// Info 实现 model.Model。
func (r *retryModel) Info() model.Info { return r.inner.Info() }

// circuitModel 实现熔断：连续失败达到阈值后打开，冷却后放行试探请求。
type circuitModel struct {
	inner           model.Model
	threshold       int
	cooldown        time.Duration
	failureObserver func(error)

	mu            sync.Mutex
	failures      int
	open          bool
	cooldownUntil time.Time
}

// GenerateContent 实现 model.Model。
func (c *circuitModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	c.mu.Lock()
	if c.open {
		if time.Now().After(c.cooldownUntil) {
			// half-open：冷却结束，放行一个试探请求。
			c.open = false
			c.failures = 0
		} else {
			c.mu.Unlock()
			return nil, ErrCircuitOpen
		}
	}
	c.mu.Unlock()

	ch, err := c.inner.GenerateContent(ctx, request)
	if err != nil {
		c.recordFailure(err)
		return nil, err
	}
	first, ok := <-ch
	if !ok {
		c.recordFailure(errors.New("agent: 响应流提前关闭"))
		return nil, errors.New("agent: 响应流提前关闭")
	}
	if first.Error != nil {
		c.recordFailure(errors.New(first.Error.Message))
	} else {
		c.recordSuccess()
	}
	return prependStream(first, ch), nil
}

func (c *circuitModel) recordFailure(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures++
	if c.failures >= c.threshold {
		c.open = true
		c.cooldownUntil = time.Now().Add(c.cooldown)
	}
	if c.failureObserver != nil {
		c.failureObserver(err)
	}
}

func (c *circuitModel) recordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.open {
		// half-open 试探成功，关闭熔断器。
		c.open = false
	}
	c.failures = 0
}

// Info 实现 model.Model。
func (c *circuitModel) Info() model.Info { return c.inner.Info() }

// cacheModel 对完整（非流式）成功响应做 TTL 缓存，key 为消息摘要。
type cacheModel struct {
	inner model.Model
	ttl   time.Duration

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	resp     *model.Response
	expireAt time.Time
}

// GenerateContent 实现 model.Model。
func (c *cacheModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	if c.cache == nil {
		c.mu.Lock()
		c.cache = make(map[string]cacheEntry)
		c.mu.Unlock()
	}
	key, err := cacheKey(request)
	if err != nil {
		return c.inner.GenerateContent(ctx, request)
	}
	if entry, ok := c.get(key); ok {
		return singleStream(cloneResponse(entry.resp)), nil
	}
	ch, err := c.inner.GenerateContent(ctx, request)
	if err != nil {
		return nil, err
	}
	// 仅缓存单响应（非流式）成功结果。
	first, ok := <-ch
	if !ok {
		return nil, errors.New("agent: 响应流提前关闭")
	}
	if first.Error == nil {
		rest := drainRest(ch)
		if len(rest) == 0 {
			c.put(key, cloneResponse(first))
			return singleStream(first), nil
		}
		return prependAll(first, rest), nil
	}
	return prependStream(first, ch), nil
}

// Info 实现 model.Model。
func (c *cacheModel) Info() model.Info { return c.inner.Info() }

func (c *cacheModel) get(key string) (cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cache[key]
	if !ok {
		return cacheEntry{}, false
	}
	if time.Now().After(e.expireAt) {
		delete(c.cache, key)
		return cacheEntry{}, false
	}
	return e, true
}

func (c *cacheModel) put(key string, resp *model.Response) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[key] = cacheEntry{resp: resp, expireAt: time.Now().Add(c.ttl)}
}

// retryableError 判定错误是否值得重试（网络/限流/服务端错误）。
func retryableError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, keyword := range []string{
		"timeout", "connection", "temporary", "rate limit", "429",
		"500", "502", "503", "504", "overloaded", "unavailable",
	} {
		if strings.Contains(msg, keyword) {
			return true
		}
	}
	return false
}

// cacheKey 计算请求消息的摘要（含工具声明，确保结果一致）。
func cacheKey(request *model.Request) (string, error) {
	payload, err := json.Marshal(struct {
		Messages []model.Message `json:"messages"`
	}{
		Messages: request.Messages,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

// singleStream 返回只含一个响应的流。
func singleStream(resp *model.Response) <-chan *model.Response {
	ch := make(chan *model.Response, 1)
	ch <- resp
	close(ch)
	return ch
}

// prependStream 将 first 与 rest 合并为一个流。
func prependStream(first *model.Response, rest <-chan *model.Response) <-chan *model.Response {
	out := make(chan *model.Response, 8)
	go func() {
		out <- first
		for r := range rest {
			out <- r
		}
		close(out)
	}()
	return out
}

// prependAll 将 first 与已收集的 rest 合并为一个流。
func prependAll(first *model.Response, rest []*model.Response) <-chan *model.Response {
	out := make(chan *model.Response, 8)
	go func() {
		out <- first
		for _, r := range rest {
			out <- r
		}
		close(out)
	}()
	return out
}

// drainStream 读光流中的剩余响应，避免 goroutine 泄漏。
func drainStream(ch <-chan *model.Response) {
	for range ch {
	}
}

// drainRest 读光流并返回剩余响应列表。
func drainRest(ch <-chan *model.Response) []*model.Response {
	var out []*model.Response
	for r := range ch {
		out = append(out, r)
	}
	return out
}

// cloneResponse 深拷贝响应（避免缓存被调用方修改）。
func cloneResponse(r *model.Response) *model.Response {
	if r == nil {
		return nil
	}
	cp := *r
	cp.Choices = append([]model.Choice(nil), r.Choices...)
	if r.Error != nil {
		e := *r.Error
		cp.Error = &e
	}
	return &cp
}

// sleepCtx 支持上下文取消的睡眠。
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
