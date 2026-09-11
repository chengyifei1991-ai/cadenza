# Cadenza

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![All Contributors](https://img.shields.io/badge/all_contributors-1-orange.svg?style=flat-square)](#contributors-✨)

> ⚠️ **非官方项目声明**：Cadenza 是社区第三方项目，与 [OpenTelemetry](https://opentelemetry.io/)、[CNCF](https://www.cncf.io/) 及官方 [opamp-go](https://github.com/open-telemetry/opamp-go) 项目**无隶属关系**，亦非其官方背书产品。`OpAMP`（Open Agent Management Protocol）在此仅作协议兼容性的描述性引用。

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
│               cadenza（单进程）                 │
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
# 构建（先构建前端并嵌入，见下方"Web 控制台"；跳过则内嵌为占位页）
go build -o bin/cadenza ./cmd/server

# 生成管理员口令哈希（Web 登录用，安全默认值要求设置）
export WEB_ADMIN_PASSWORD_HASH=$(htpasswd -bnBC 10 "" '你的密码' | tr -d ':\n')

# 运行（最小配置：SQLite + 任意 LLM key + Web 登录）
export LLM_API_KEY=sk-xxx
export DB_DRIVER=sqlite
export DB_SQLITE_PATH=./data/opamp.db   # 首次运行请先 mkdir -p data
export HTTP_ADDR=:8080
export DEMO_MODE=true                   # 可选：空库注入演示数据（开箱体验）
./bin/cadenza
```

启动后访问 **http://localhost:8080**（Web 控制台，默认 `admin` + 你的密码；`DEMO_MODE=true` 时含演示 Collector/任务/审计）。

### 🌐 Web 控制台

- 前端位于 [`web/`](./web/)（React + TypeScript + Vite + Ant Design），构建产物通过
  `npm --prefix web run build && node web/scripts/embed.mjs` 同步至
  `internal/webui/static`，由 `go:embed` 打进单二进制（生产部署无需额外静态服务）。
- 开发联调：`go run ./cmd/server`（:8080）+ `npm --prefix web run dev`（:5173，
  Vite 已配置 `/api` 代理到 8080，同源免 CORS）。
- 鉴权：默认 `WEB_AUTH_MODE=simple`（单管理员登录）；`WEB_AUTH_MODE=off` 免登，
  仅限本地/演示环境。纯后端部署（无 Web）可设 `DISABLE_WEB=true`。
- **容器化（前后端分离）**：见 [`docker-compose.ga.yml`](./docker-compose.ga.yml)
  （backend 容器 + nginx 托管 `web/dist` 并反代 `/api /mcp /v1/opamp /healthz`；
  `npm --prefix web run build` 后 `docker compose -f docker-compose.ga.yml up -d --build`，
  浏览器访问 http://localhost:8081）。
- 页面路线图与 PRD 见 [`docs/product-plan.md`](./docs/product-plan.md) 与
  [`docs/web-frontend-prd.md`](./docs/web-frontend-prd.md)。

## ⚙️ 配置（环境变量）

见 [.env.example](./.env.example)，关键项：

| 变量 | 默认 | 说明 |
|---|---|---|
| `HTTP_ADDR` | `:8080` | 主 HTTP 监听地址 |
| `DB_DRIVER` / `DB_DSN` / `DB_SQLITE_PATH` | `sqlite` | 存储：`mysql`（生产）或 `sqlite`（开发） |
| `OPAMP_AUTH_TOKEN` | 空（放行） | Collector 接入认证 Bearer token |
| `MCP_AUTH_TOKEN` | 空（不启用） | `/mcp` 端点 Bearer token；**公网部署必须配置**（为空时启动告警） |
| `OTELCOL_BIN` / `STRICT_VALIDATE` | `/usr/local/bin/otelcol-contrib` / `false` | otelcol-contrib v0.156.0 深度校验 |
| `LLM_BASE_URL` / `LLM_API_KEY` / `LLM_MODEL` | DeepSeek | 主模型（OpenAI 兼容） |
| `LLM_BACKUP_*` | 空 | 备用模型（failover 第二候选） |
| `LLM_LOCAL_*` | 空 | 本地兜底（第三候选，如 Ollama） |
| `LLM_TIMEOUT` / `LLM_RETRY` / `LLM_CIRCUIT_*` / `LLM_CACHE_TTL` | 60s / 3 / 5 / 30s / 10m | LLM 稳定性参数 |
| `REQUIRE_APPROVAL` | `true` | 生成/优化任务是否强制审批 |
| `WEB_AUTH_MODE` / `WEB_ADMIN_USER` / `WEB_ADMIN_PASSWORD_HASH` | `simple` / `admin` / 必填 | Web 单管理员登录（simple 模式缺失口令哈希将拒绝启动） |
| `DISABLE_WEB` / `WEB_DIR` | `false` / 空 | 关闭 Web 静态路由 / 覆盖静态资源目录 |
| `CORS_ALLOWED_ORIGINS` | 空（同源） | 允许的跨域 Origin（生产建议保持同源） |
| `DEMO_MODE` | `false` | `true` 时空库注入演示数据（开箱体验） |

## 🔌 Collector 接入

Collector 侧使用 opamp-go client 或任何 OpAMP 实现连接：

```yaml
# collector 配置文件（opamp extension）
extensions:
  opamp:
    server:
      ws:
        endpoint: ws://<cadenza-host>:8080/v1/opamp
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
| GET | `/healthz` | 健康检查（公开，含 `db` 状态） |
| POST | `/api/v1/auth/login` / `/logout` | Web 登录 / 登出（Cookie 会话） |
| GET | `/api/v1/auth/me` | 当前登录用户与会话校验 |
| GET | `/api/v1/system/info` | 系统信息（版本 / 鉴权模式 / 演示模式，公开） |
| POST | `/api/v1/sessions` | 创建会话 |
| GET | `/api/v1/sessions?page=&page_size=` | 会话列表（含摘要：条数 / 首条消息 / 最近消息） |
| GET | `/api/v1/sessions/{id}` | 会话详情（含全部消息） |
| POST | `/api/v1/chat` | 对话（`{session_id, message}`） |
| GET | `/api/v1/tasks?status=&page=&page_size=` | 任务列表（支持分页） |
| GET | `/api/v1/tasks/{id}` | 任务详情 |
| POST | `/api/v1/tasks/apply` | 配置编辑器"保存并下发" `{collector_instance_uid, yaml, note?}` |
| POST | `/api/v1/tasks/rollback` | 创建回滚任务 `{collector_instance_uid, version_id}` |
| POST | `/api/v1/tasks/{id}/approve` | 审批并下发（审批人=当前登录用户） |
| POST | `/api/v1/tasks/{id}/reject` | 拒绝（需 reason） |
| GET | `/api/v1/collectors?page=&page_size=` | Collector 列表（支持分页） |
| GET | `/api/v1/collectors/{uid}/versions?page=&page_size=` | 版本历史（每次下发自动快照） |
| GET | `/api/v1/audit?since=&page=&page_size=` | 审计日志（支持分页） |
| GET | `/api/v1/stats` | 仪表盘聚合（Collector/任务按状态计数、会话总数） |

> **分页说明**：`page`（≥1，默认 1）、`page_size`（1~100，默认 20）。携带任一
> 分页参数时返回 `{items, total, page, page_size}`（`total` 为过滤后总数，供前端
> 渲染总页数）；不带分页参数时返回裸数组，向后兼容 MCP 与现有调用方。
>
> **鉴权说明**：`/api/v1/*` 默认受 Web 登录保护（`WEB_AUTH_MODE=simple`），
> 白名单外的写操作（审批/拒绝/下发）操作者绑定当前登录用户。`/mcp` 与 `/v1/opamp`
> 属协议链路，暂不受 Web 登录影响（MCP 鉴权规划见 docs/product-plan.md §8）。

## 🧪 测试

```bash
go test ./...   # 全量表驱动测试（store/task/validator/agent/api/opampserver/config/demo）
go vet ./...
# Web（可选）：npm --prefix web install && npm --prefix web run build
```

## 📁 目录结构

```
cmd/server/           入口（装配 + 优雅关闭 + Web 静态挂载）
internal/
  config/             环境变量配置加载（含 Web/鉴权/演示）
  store/              数据模型 + Store 接口（SQLite/MySQL 双实现）
  task/               任务状态机（含审批）
  validator/          两级配置校验
  opampserver/        OpAMP 服务器封装 + Collector 注册表
  agent/              LLM 稳定性包装 + 9 工具 + 对话编排
  mcp/                MCP Server（trpc-mcp-go）
  api/                REST handlers + 鉴权 + 静态资源 + 路由装配
  webui/              go:embed 内嵌 Web 产物（static/ 占位常驻）
  demo/               演示数据注入（DEMO_MODE）
  ulid/ version/      共享小工具
web/                  前端（React+TS+Vite+AntD，构建产物 embed 进 webui/static）
docs/                 产品规划/设计决策/PRD（product-plan、design-web-p0、web-frontend-prd）
```

## 👥 贡献者

感谢所有为本项目做出贡献的人（[emoji key](https://allcontributors.org/docs/en/emoji-key)）：

<!-- ALL-CONTRIBUTORS-LIST:START - Do not remove or modify this section -->
<!-- prettier-ignore-start -->
<!-- markdownlint-disable -->
<table>
  <tbody>
    <tr>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/chengyifei1991"><img src="https://avatars.githubusercontent.com/chengyifei1991?s=100&v=4" width="100px;" alt="chengyifei1991"/><br /><sub><b>chengyifei</b></sub></a><br /><a href="#code-chengyifei1991" title="Code">💻</a> <a href="#doc-chengyifei1991" title="Documentation">📖</a> <a href="#infra-chengyifei1991" title="Infrastructure">🚇</a> <a href="#review-chengyifei1991" title="Reviewed Pull Requests">👀</a> <a href="#maintenance-chengyifei1991" title="Maintenance">🚧</a></td>
    </tr>
  </tbody>
</table>
<!-- markdownlint-restore -->
<!-- prettier-ignore-end -->
<!-- ALL-CONTRIBUTORS-LIST:END -->

本项目遵循 [all-contributors](https://allcontributors.org) 规范，欢迎任何形式的贡献！参与方式见 [CONTRIBUTING.md](./CONTRIBUTING.md)。
