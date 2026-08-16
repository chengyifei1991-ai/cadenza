# opamp-backend

基于 [opamp-go](https://github.com/open-telemetry/opamp-go) 构建的 OpAMP 统一管控后端：
管理 OpenTelemetry Collector 集群，提供**配置下发 / 自动优化 / 对话生成配置**，
并通过 **MCP 服务**与**内置对话 Agent** 双出口对外提供能力。

## ✨ 能力总览

| 能力 | 说明 |
|---|---|
| OpAMP Server | `/v1/opamp`，HTTP + WebSocket 双传输，接入认证、状态接收、配置主动下发 |
| 配置校验 | 两级：yaml.v3 结构校验 + `otelcol-contrib validate`（锁定 **v0.156.0**） |
| 对话生成配置 | 自然语言 → LLM 生成 YAML → 校验 → 审批 → 下发 |
| 自动优化配置 | 基于 Collector 上报状态分析并提议优化方案 |
| 版本升级 | `PackagesAvailable` 协议能力（Beta），任务化审批 |
| MCP Server | `/mcp`（streamable HTTP），9 个工具，外部 LLM/IDE 可直接调用 |
| REST API | `/api/v1/*`，Web 前端使用 |
| 审批闭环 | 会话 → 任务 → 审批 → 下发，全部审计留痕 |
| LLM 稳定性 | 超时 → 指数退避重试 → failover 多模型切换 → 熔断 → 缓存 → 故障隔离 |

## 🏗 架构

```
┌──────────────────────────────────────────────────────┐
│               opamp-backend（单进程）                 │
│  ┌─────────────┐  ┌──────────────┐  ┌──────────────┐ │
│  │  OpAMP Server│  │  MCP Server  │  │  REST/Web    │ │
│  │  (/v1/opamp) │  │  (/mcp)      │  │  (/api/v1/*) │ │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘ │
│         └──────────────┐  │  ┌──────────────┘         │
│                 ┌──────▼──▼──▼──────┐                 │
│                 │  Agent 编排层      │                 │
│                 │ (trpc-agent-go)   │                 │
│                 └──────┬────────────┘                 │
│          ┌─────────────┼─────────────┐                │
│    ┌─────▼─────┐ ┌─────▼─────┐ ┌─────▼─────┐         │
│    │ 配置管理   │ │ 任务/审批  │ │ Collector │         │
│    │(版本+校验) │ │ (状态机)   │ │ 注册表     │         │
│    └─────┬─────┘ └─────┬─────┘ └─────┬─────┘         │
│          └─────────────┴─────┬───────┘                │
│                       ┌──────▼──────┐                 │
│                       │ SQLite/MySQL│                 │
│                       └─────────────┘                 │
└──────────────────────────────────────────────────────┘
        ▲ WS/HTTP(OpAMP)      ▲ MCP(streamable HTTP)   ▲ HTTP JSON
   OTel Collector 集群     外部 LLM/IDE            浏览器
```

## 🚀 快速开始

```bash
# 构建
go build -o bin/opamp-server ./cmd/server

# 运行（最小配置：SQLite + 任意 LLM key）
export LLM_API_KEY=sk-xxx
export DB_DRIVER=sqlite
export DB_SQLITE_PATH=./data/opamp.db
export HTTP_ADDR=:8080
./bin/opamp-server
```

## ⚙️ 配置（环境变量）

见 [.env.example](./.env.example)，关键项：

| 变量 | 默认 | 说明 |
|---|---|---|
| `HTTP_ADDR` | `:8080` | 主 HTTP 监听地址 |
| `DB_DRIVER` / `DB_DSN` / `DB_SQLITE_PATH` | `sqlite` | 存储：`mysql`（生产）或 `sqlite`（开发） |
| `OPAMP_AUTH_TOKEN` | 空（放行） | Collector 接入认证 Bearer token |
| `OTELCOL_BIN` / `STRICT_VALIDATE` | `/usr/local/bin/otelcol-contrib` / `false` | otelcol-contrib v0.156.0 深度校验 |
| `LLM_BASE_URL` / `LLM_API_KEY` / `LLM_MODEL` | DeepSeek | 主模型（OpenAI 兼容） |
| `LLM_BACKUP_*` | 空 | 备用模型（failover 第二候选） |
| `LLM_LOCAL_*` | 空 | 本地兜底（第三候选，如 Ollama） |
| `LLM_TIMEOUT` / `LLM_RETRY` / `LLM_CIRCUIT_*` / `LLM_CACHE_TTL` | 60s / 3 / 5 / 30s / 10m | LLM 稳定性参数 |
| `REQUIRE_APPROVAL` | `true` | 生成/优化任务是否强制审批 |

## 🔌 Collector 接入

Collector 侧使用 opamp-go client 或任何 OpAMP 实现连接：

```yaml
# collector 配置文件（opamp extension）
extensions:
  opamp:
    server:
      ws:
        endpoint: ws://<opamp-backend-host>:8080/v1/opamp
    instance_uid: <32位hex>
    headers:
      Authorization: "Bearer <OPAMP_AUTH_TOKEN>"
```

WebSocket 连接支持配置主动即时推送；HTTP 拉取模式下，待下发配置会在 Collector 下一次上报时随响应返回。

## 🧰 MCP 工具

| 工具 | 说明 |
|---|---|
| `list_collectors` | 查询集群状态（可按分组过滤） |
| `get_collector_config` | 获取 Collector 当前生效配置 |
| `generate_config` | 对话生成配置 → 任务（审批） |
| `optimize_config` | 自动分析并提议优化方案 |
| `apply_config` | 直接下发（需全局审批关闭） |
| `upgrade_collector` | 创建版本升级任务（Beta） |
| `approve_task` / `reject_task` | 审批 / 拒绝 |
| `list_pending_tasks` | 列出待审批任务 |

MCP 端点：`http://<host>:8080/mcp`（streamable HTTP，`Accept: application/json, text/event-stream`）。

## 📡 REST API

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/v1/sessions` | 创建会话 |
| POST | `/api/v1/chat` | 对话（`{session_id, message}`） |
| GET | `/api/v1/tasks?status=&page=&page_size=` | 任务列表（支持分页） |
| GET | `/api/v1/tasks/{id}` | 任务详情 |
| POST | `/api/v1/tasks/rollback` | 创建回滚任务 `{collector_instance_uid, version_id}` |
| POST | `/api/v1/tasks/{id}/approve` | 审批并下发 |
| POST | `/api/v1/tasks/{id}/reject` | 拒绝 |
| GET | `/api/v1/collectors?page=&page_size=` | Collector 列表（支持分页） |
| GET | `/api/v1/collectors/{uid}/versions?page=&page_size=` | 版本历史（每次下发自动快照） |
| GET | `/api/v1/audit?since=&page=&page_size=` | 审计日志（支持分页） |

> **分页说明**：`page`（≥1，默认 1）、`page_size`（1~100，默认 20）。携带任一
> 分页参数时返回 `{items, total, page, page_size}`（`total` 为过滤后总数，供前端
> 渲染总页数）；不带分页参数时返回裸数组，向后兼容 MCP 与现有调用方。

## 🧪 测试

```bash
go test ./...   # 全量表驱动测试（store/task/validator/agent/api/opampserver/config）
go vet ./...
```

## 📁 目录结构

```
cmd/server/           入口（装配 + 优雅关闭）
internal/
  config/             环境变量配置加载
  store/              数据模型 + Store 接口（SQLite/MySQL 双实现）
  task/               任务状态机（含审批）
  validator/          两级配置校验
  opampserver/        OpAMP 服务器封装 + Collector 注册表
  agent/              LLM 稳定性包装 + 9 工具 + 对话编排
  mcp/                MCP Server（trpc-mcp-go）
  api/                REST handlers + 路由装配
docs/                 项目宪章与设计决策
```
