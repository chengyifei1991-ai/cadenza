# OpAMP 统一管控后端 — 设计决策记录（v1）

> 日期：2026-08-15 ｜ 状态：已实现 ｜ 关联：docs/ProjectCharer.md

## 决策背景

基于 opamp-go 构建 OpAMP 服务器管理 OTel Collector 集群，同时实现 MCP 服务与
智能体管理（非仅 Web + 服务端）。核心功能：配置下发、自动优化配置、对话生成配置、
（可选）更新关联 Collector 版本。

## 关键决策

### D1：Agent 框架选型 → tRPC-Agent-Go（v1.11.1）

- 对比 AgenticGoKit：腾讯官方维护、生产级、MCP 集成开箱即用（`tool/mcp`）、中文生态好。
- 实证：根模块**不依赖 trpc-go**，可独立使用，无"全家桶"顾虑。
- LLM 接入用 `model/openai`，**支持自定义 BaseURL/APIKey**（`WithBaseURL` / `WithAPIKey`），
  DeepSeek/通义/Ollama 均可直连。

### D2：MCP 定位 → 对外工具 + 内置对话 Agent 双出口

- MCP Server 由 trpc-mcp-go 提供（`NewServer` + `RegisterTool` + `Handler()` 挂主 HTTP 端口 `/mcp`，
  支持 streamable HTTP/SSE/stdio）。
- 9 个工具与内置对话 Agent **共用同一套 tool 实现**（trpc-agent-go `tool.Tool` 抽象），无双份逻辑。

### D3：智能体管理 → 会话 + 任务 + 审批（单审批人，预留扩展）

- `Approvers []string` 预留多人扩展；当前单审批人通过/拒绝。
- 状态机：`pending → generating → validating → awaiting_approval → applying → done`，
  任意非终态可 `failed`（可重试），`awaiting_approval → rejected`。

### D4：存储 → 生产 MySQL + 开发 SQLite（Store 接口双实现）

- `DB_DRIVER=mysql|sqlite` 环境变量切换，零代码改动。
- SQLite 用 modernc.org/sqlite（**纯 Go 无 CGO**）。

### D5：配置校验 → 两级

1. yaml.v3 语法 + 必需结构（service.pipelines）；
2. `otelcol-contrib validate` 深度校验，**版本锁定 v0.156.0**（`OTELCOL_BIN` 环境变量），
   二进制缺失自动降级并告警；`STRICT_VALIDATE=true` 时失败强制阻断。

### D6：LLM 稳定性 → 装饰器链（全部实现 model.Model 接口）

```
cache → circuit → retry → failover → openai(主/备/本地)
```

- 超时：`WithHTTPClientOptions(WithHTTPClientTimeout)`；
- 重试：指数退避（1s/2s/4s），可重试错误（网络/429/5xx）自动重试 ≤3 次；
- 熔断：连续失败 ≥5 次熔断 30s（half-open 试探）；
- failover：DeepSeek 主 → 备用 key/模型 → 本地 Ollama（可选）；
- 缓存：同请求 TTL 缓存（sha256 key）；
- **故障隔离**：LLM 故障只影响 generate/optimize，配置下发/状态查询不受影响。

### D7：OpAMP 下发策略 → WS 主动推送 + HTTP 随轮询

- WebSocket 连接：`conn.Send()` 即时推送；
- HTTP 拉取连接：记录 pending，下次上报随 `ServerToAgent.RemoteConfig` 返回；
- Collector 离线：配置排队，恢复后随首次上报下发。

## 风险与后续项

1. `otelcol-contrib v0.156.0` 与集群自定义组件不一致会误报 → 校验失败默认仅告警。
2. `upgrade_collector` 当前为任务化审批占位，`PackagesAvailable` 包分发为协议 Beta 能力，
   需 Collector 侧配合实现。
3. 多实例部署需将注册表/审批状态共享（当前单实例 + SQLite/MySQL）。
4. 配置生成 LLM 输出不可控 → 双校验 + 审批 + diff 展示三道防线。
