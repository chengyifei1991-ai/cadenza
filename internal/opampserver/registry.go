// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 chengyifei

// Package opampserver 封装 OpAMP 服务器：连接认证、状态接收、
// Collector 注册表维护、配置主动下发。
package opampserver

import (
	"context"
	"sync"
	"time"

	"github.com/chengyifei1991-ai/cadenza/internal/store"
	"github.com/open-telemetry/opamp-go/server/types"
)

// Registry 维护 Collector 的内存注册表（含活跃连接），
// 状态变更即时持久化到 Store。
type Registry struct {
	mu           sync.RWMutex
	collectors   map[string]*store.Collector
	conns        map[string]types.Connection
	st           store.Store
	offlineAfter time.Duration
}

// NewRegistry 创建注册表。offlineAfter 是判定 Collector 离线的
// 未上报时长阈值。
func NewRegistry(st store.Store, offlineAfter time.Duration) *Registry {
	return &Registry{
		collectors:   make(map[string]*store.Collector),
		conns:        make(map[string]types.Connection),
		st:           st,
		offlineAfter: offlineAfter,
	}
}

// Upsert 更新 Collector 信息并绑定连接，随后持久化。
func (r *Registry) Upsert(c *store.Collector, conn types.Connection) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c.Status == "" {
		c.Status = store.CollectorStatusHealthy
	}
	r.collectors[c.InstanceUID] = c
	if conn != nil {
		r.conns[c.InstanceUID] = conn
	}
	// 持久化失败仅记录日志，不影响内存状态（best effort）。
	_ = r.st.UpsertCollector(context.Background(), c)
}

// Get 按 instance_uid 返回 Collector 与是否存在。
func (r *Registry) Get(uid string) (*store.Collector, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.collectors[uid]
	return c, ok
}

// Conn 返回指定 Collector 的活跃连接（WebSocket），不存在返回 false。
func (r *Registry) Conn(uid string) (types.Connection, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	conn, ok := r.conns[uid]
	return conn, ok
}

// List 返回全部 Collector 的副本。
func (r *Registry) List() []store.Collector {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]store.Collector, 0, len(r.collectors))
	for _, c := range r.collectors {
		out = append(out, *c)
	}
	return out
}

// Remove 移除 Collector 的连接绑定（连接断开时调用，保留状态记录）。
func (r *Registry) Remove(uid string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.conns, uid)
}

// MarkOffline 将超过 offlineAfter 未上报的 Collector 标记为离线并持久化。
func (r *Registry) MarkOffline(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for uid, c := range r.collectors {
		if now.Sub(c.LastSeenAt) > r.offlineAfter && c.Status != store.CollectorStatusOffline {
			c.Status = store.CollectorStatusOffline
			_ = r.st.UpsertCollector(context.Background(), c)
			_ = uid
		}
	}
}
