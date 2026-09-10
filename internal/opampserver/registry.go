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
	"github.com/open-telemetry/opamp-go/protobufs"
	"github.com/open-telemetry/opamp-go/server/types"
)

// Registry 维护 Collector 的内存注册表（含活跃连接），
// 状态变更即时持久化到 Store。
type Registry struct {
	mu         sync.RWMutex
	collectors map[string]*store.Collector
	conns      map[string]types.Connection
	// caps 记录各 Collector 上报的能力位（AgentCapabilities bitmask）。
	caps map[string]uint64
	// repHash 记录各 Collector 最近一次 APPLIED 回报的远端配置哈希（hex）。
	repHash map[string]string
	// repEff 记录各 Collector 最近一次**上报**的 effective config 内容。
	// 与 collectors[uid].EffectiveConfig 区分：后者会被服务端下发的"意图值"覆盖，
	// 本字段只反映 Agent 真实上报，用于下发生效确认。
	repEff map[string]string
	st     store.Store
	// offlineAfter 是判定 Collector 离线的未上报时长阈值。
	offlineAfter time.Duration
}

// NewRegistry 创建注册表。offlineAfter 是判定 Collector 离线的
// 未上报时长阈值。
func NewRegistry(st store.Store, offlineAfter time.Duration) *Registry {
	return &Registry{
		collectors:   make(map[string]*store.Collector),
		conns:        make(map[string]types.Connection),
		caps:         make(map[string]uint64),
		repHash:      make(map[string]string),
		repEff:       make(map[string]string),
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
	// 连接断开后能力位与上报哈希随之失效，避免给旧连接下发/确认。
	delete(r.caps, uid)
	delete(r.repHash, uid)
	delete(r.repEff, uid)
}

// Online 返回该 Collector 当前是否有活跃连接。
func (r *Registry) Online(uid string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.conns[uid]
	return ok
}

// SetCapabilities 记录 Collector 上报的能力位（AgentCapabilities bitmask）。
func (r *Registry) SetCapabilities(uid string, caps uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.caps[uid] = caps
}

// AcceptsRestartCommand 返回 Collector 是否声明支持重启命令
// （AgentCapabilities_AcceptsRestartCommand = 1024）。
func (r *Registry) AcceptsRestartCommand(uid string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.caps[uid]&uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRestartCommand) != 0
}

// SetReportedHash 记录 Collector 对远端配置的 ack 哈希（RemoteConfigStatus
// APPLIED 时回报的 LastRemoteConfigHash，hex）。空串表示清除（应用失败）。
func (r *Registry) SetReportedHash(uid, hashHex string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if hashHex == "" {
		delete(r.repHash, uid)
		return
	}
	r.repHash[uid] = hashHex
}

// ReportedHash 返回 Collector 最近一次 APPLIED 回报的远端配置哈希（hex）与是否存在。
func (r *Registry) ReportedHash(uid string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.repHash[uid]
	return h, ok
}

// SetReportedEffective 记录 Collector **上报**的 effective config 内容
// （服务端下发的意图值不写入本字段，保证确认逻辑读到的永远是 Agent 实况）。
func (r *Registry) SetReportedEffective(uid, content string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.repEff[uid] = content
}

// ReportedEffective 返回 Collector 最近上报的 effective config 内容与是否存在。
func (r *Registry) ReportedEffective(uid string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.repEff[uid]
	return c, ok
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
