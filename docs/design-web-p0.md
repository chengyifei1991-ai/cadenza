# 设计方案：Web 前端配套后端缺口（P0 六项）

> 状态：方案待审核 ｜ 日期：2026-09-05 ｜ 关联：[product-plan.md](./product-plan.md) M0
> 说明：本方案仅新增/调整后端能力，**不含任何代码实现**（遵循宪章阶段一规则）。

---

## 0. 背景与现状

后端已完成 OpAMP / MCP / REST / Agent 全链路（design-v1），但按产品规划进入 Web 前端阶段时，
存在六项契约缺口（P0），不补齐则前端无法正式立项。以下逐项给出设计，全部改动收敛在
`cmd/server`、`internal/config`、`internal/api`、`internal/store` 四个包内，不触碰 OpAMP 协议链路。

**现状事实（已核实）**：
- 路由仅挂 `/v1/opamp`、`/mcp`、`/api/v1/*`，无静态资源服务（main.go/router.go）。
- REST 无鉴权；`approve/reject` 的审批人来自请求体自由字符串，默认 `"user"`（handlers.go）。
- 会话仅 `POST /sessions` 建、`POST /chat` 问；**无会话列表与历史读接口**；`chat_sessions` 表仅
  `id, created_at`（sqlstore.go）。
- Store 层已具备分组 CRUD（`UpsertGroup/GetGroup/ListGroups`），但无 REST 暴露与使用场景（V2 用）。
- 无 CORS、无 `/healthz`、无聚合统计端点。
- `apply` 类动作仅存在于 MCP 工具与对话编排，无显式携带 YAML 的 REST 入口。

---

## P0-1 前端静态资源挂载（单二进制内嵌）

### 功能概述
使 `./bin/cadenza` 一个二进制即提供完整 Web 控制台：打包内嵌 `web/` 构建产物，并对
SPA 路由做 fallback；非 API 前缀请求回退到 `index.html`。

### 核心设计
- `internal/webui` 包：`//go:embed static`（目录常驻仓库，含占位 `index.html` 保证编译期存在）。
  构建流程：`pnpm build` 产出 `web/dist` → 构建脚本同步至 `internal/webui/static` → `go build`。
- 路由顺序（router.go 装配处新增）：先精确匹配现有协议路由，再 `/api/`、`/mcp`、`/v1/opamp` 前缀
  直通后端，其余路径 → 静态文件（命中文件返回，否则回退 `index.html`，支持 History 路由）。
- 环境变量：`WEB_DIR`（可选，指向磁盘静态目录覆盖内嵌资源，供不重新编译的定制场景）；
  `DISABLE_WEB=true`（纯后端部署：不注册静态路由，/ 返回 404）。

### 新增依赖评估
无第三方依赖（标准库 `embed` / `net/http`）。

### 潜在风险与替代
- 风险：`go:embed` 目录在源码仓库与构建环境不一致导致旧资源打包 → 对策：CI 步骤强制先构建前端
  再 `go build`；本地 dev 一律走 Vite proxy，不依赖内嵌资源。
- 替代：生成式 `embed.go`（构建期写文件）——复杂度高，不采用。

---

## P0-2 会话读接口（AI 助手页底座）

### 功能概述
AI 助手页需要：会话列表（含摘要）、单个会话历史回读。当前只能建/问，刷新即丢上下文。

### 核心数据模型
- 复用 `ChatSession` / `ChatMessage`，**不改表结构**：列表摘要（首条用户消息、末条时间、条数）由
  `chat_messages` 聚合子查询得出；会话按最近消息 `id` 降序排列。
- Store 接口新增：
  ```go
  // ListSessions 分页返回会话摘要列表（含 last_message_at / message_count / 首条用户消息预览）。
  ListSessions(ctx, page, pageSize) (items []SessionSummary, total int64, err error)
  ```
  `SessionSummary` = 现有 ChatSession 字段 + 上述三派生字段（新模型仅用于列表展示，不入库）。

### API 设计
| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/sessions?page=&page_size=` | 会话列表（沿用分页信封）；不带分页参数返回裸数组（向后兼容约定） |
| GET | `/api/v1/sessions/{id}` | 会话详情（含 messages，映射现有 GetSession） |
| DELETE | `/api/v1/sessions/{id}` | （可选）删除会话及其消息 |

### 核心流程
前端进入 AI 助手页 → GET 会话列表（react-query 缓存）→ 选中会话 → GET 详情渲染历史 →
POST `/chat` 后本地追加新消息并对该会话失效重取。

### 与 Collector 交互方式
无直接交互；任务联动通过任务 ID 关联（P0-6/现有任务接口）。

### 新增依赖评估
无。

### 潜在风险与替代
- 会话无标题字段：以首条用户消息截断（≤50 字）作列表标题，属展示层约定，不落库。
- 消息量增长：单会话详情建议 MVP 先全量返回；超长会话分页留 V1.5。

---

## P0-3 单管理员登录与鉴权（管理面安全基线）

### 功能概述
Web 管理面默认需登录；审批/下发等写操作的"操作者"绑定当前登录用户，替代请求体自由填 approver。

### 核心设计
- 配置（config.go 新增）：
  - `WEB_AUTH_MODE` ∈ `simple`（默认）/ `off`。`off` 为演示/本地免登（文档显著警告）。
  - `WEB_ADMIN_USER`（默认 `admin`）/ `WEB_ADMIN_PASSWORD_HASH`（bcrypt）。`simple` 模式且未配置
    密码时**启动报错（fail-closed）**，错误信息给出配置指引（待决问题 9.1 的推荐解）。
- 会话：登录成功签发随机 token（`crypto/rand`，无第三方），HttpOnly + SameSite=Lax Cookie
  （同源部署，无 CORS 通配），服务端内存 TTL 会话表（默认 24h）。重启即失效——V1 单实例可接受，
  已在规划风险中声明。
- 中间件挂载范围：**仅 `/api/v1/*`**（含 approve/reject）。`/mcp`、`/v1/opamp` 不挂（协议链路，
  MCP 鉴权 V1.5 专项，见风险）。
- 操作者绑定：approve/reject handler 从 `Context` 取当前登录用户写入 `Task.Approver` / 审计 `Actor`；
  请求体 `approver` 字段降级为忽略（保留兼容不报错），审计 Actor 由 `user` 变为真实账号。

### API 设计
| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/v1/auth/login` | `{username, password}` → 200 + Set-Cookie；错误统一 401 |
| POST | `/api/v1/auth/logout` | 注销并清 Cookie |
| GET | `/api/v1/auth/me` | 当前用户（供前端启动时校验会话） |

### 核心流程
前端启动 → `/auth/me` 401 → 跳登录页；登录成功写 Cookie；react-query 统一错误处理对 401 登出回登录页。

### 与 Collector 交互方式
无。

### 新增依赖评估
- `golang.org/x/crypto`（bcrypt 校验管理员口令）：广泛使用、纯 Go 无 CGO、仅此一处使用，
  是唯一新增外部依赖。
- token 生成用标准库 `crypto/rand`。

### 潜在风险与替代
- 内存会话表多实例不一致 → V1 单实例约束；V3 联邦化时改为 DB 表。
- 登录暴力破解 → MVP 阈值低（不做限流），依赖部署侧（内网/反代限流）；文档说明。
- CSRF → 同源 + HttpOnly + SameSite=Lax 已足够，不引入额外依赖。
- **MCP `approve_task` 无鉴权**：MVP 接受并文档声明"请部署于可信网络"，V1.5 补 MCP token
  （复用 OPAMP_AUTH_TOKEN 同型机制）。

---

## P0-4 CORS 与健康检查

### 功能概述
开发期（Vite 5173 → Go 8080）与容器探活需要：CORS 策略与 `/healthz`。主推开发走 Vite proxy
（同源，天然无 CORS）；CORS 作为可选项兜底分离部署。

### 设计
- 配置 `CORS_ALLOWED_ORIGINS`（逗号分隔，默认空 = 不开启跨域；`*` 需显式设置并禁止携带凭证）。
  中间件仅对匹配 Origin 回写 CORS 头；鉴权走 Cookie 时不支持跨域（文档说明）。
- `GET /healthz` → `200 {"status":"ok","db":"up|down"}`（db ping 不阻塞返回，仅作为负载字段）；
  挂载于所有鉴权之外。`/readyz` 不进 V1。

### 新增依赖评估
无（手写中间件，标准库）。

### 潜在风险与替代
- CORS + Cookie 凭证组合易误配 → 文档明确：生产同源部署，CORS 仅服务端到端开发调试。

---

## P0-5 聚合统计端点（仪表盘底座）

### 功能概述
Dashboard 需要各状态计数，避免前端拉全表自拼（会随数据量劣化）。

### API 设计
`GET /api/v1/stats`（需登录）：
```jsonc
{
  "collectors": {"total":0,"healthy":0,"unhealthy":0,"offline":0,"unknown":0},
  "tasks":     {"total":0,"pending":0,"generating":0,"validating":0,
                "awaiting_approval":0,"applying":0,"done":0,"rejected":0,"failed":0},
  "sessions_total": 0
}
```
- Store 新增 `CountCollectorsByStatus` / `CountTasksByStatus` / `CountSessions`（COUNT + GROUP BY，
  单次查询各一组）；SQLite/MySQL 双实现同一 SQL（ANSI COUNT/GROUP BY，兼容）。

### 核心流程
Dashboard 挂载即取；结合任务中心"待审批"高亮；react-query 30s 轮询（M2 后与任务中心共用节奏）。

### 新增依赖评估 / 潜在风险
无第三方依赖。计数口径需在双驱动上以表驱动测试锁定（total=各状态之和的断言）。

---

## P0-6 显式"创建 apply 任务"REST 接口（配置编辑器保存下发）

### 功能概述
配置编辑器"编辑 YAML → 校验 → 保存下发"需要一个显式入口（现状 apply 仅在 MCP/对话中）。
generate/optimize 仍走 `/chat`（其任务在会话上下文生成），本接口只承接"手写/编辑后直接提交"。

### API 设计
`POST /api/v1/tasks/apply`（需登录）
```jsonc
{ "collector_instance_uid": "…", "yaml": "…", "note": "可选变更说明" }
```
- 服务端流程完全复用回滚任务的既有链路：**两级校验**（yaml.v3 结构 → `otelcol-contrib validate`，
  `STRICT_VALIDATE=false` 时失败仅告警仍可提交）→ 创建 `apply` 任务，状态 `awaiting_approval`
  （受全局 `REQUIRE_APPROVAL` 控制；关闭时仍走 pending→applying 状态机以便审计）→ 返回任务对象。
- 校验失败返回 400，`{error}` 携带结构化错误列表（沿用 validator.Result.Errors，前端可分行渲染）。

### 核心流程
编辑器点"保存并下发" → POST → 200 返回任务 → 前端跳转任务详情等待审批/下发 → 轮询至 done 或失败。

### 新增依赖评估 / 潜在风险
无新依赖。风险：直接 apply 绕过对话语义但审计仍在（AppendAudit apply 动作 + Actor=当前用户）；
与 MCP `apply_config` 行为对齐，避免两套下发语义分叉——实现时统一走 task.Service。

---

## 统一改动影响面（M0 合并落地）

| 包 | 改动 |
|---|---|
| `cmd/server/main.go` | 装配静态 handler、鉴权中间件、CORS；日志打印 Web 入口与鉴权模式 |
| `internal/config` | 新增 `WEB_DIR/DISABLE_WEB/WEB_AUTH_MODE/WEB_ADMIN_USER/WEB_ADMIN_PASSWORD_HASH/CORS_ALLOWED_ORIGINS` |
| `internal/api` | 新 handler 文件（auth/static/stats/sessions/apply）+ 路由注册 + 中间件 |
| `internal/store` | 接口新增 `ListSessions/CountCollectorsByStatus/CountTasksByStatus/CountSessions` + 双实现 + 表驱动测试 |
| `internal/webui` | 新增 embed 包（占位资源常驻） |
| 构建 | 前端产物同步进 `internal/webui/static`（CI 编排，见 PRD） |

## 实施顺序建议（M0 一周内）

1. P0-1 静态挂载 + 前端骨架空壳（先打通"二进制内含可访问页面"）
2. P0-3 登录/中间件（安全基线上墙，后序接口直接挂鉴权）
3. P0-2 会话读接口（AI 助手数据底座）
4. P0-6 apply 接口（编辑器依赖）
5. P0-5 stats（仪表盘依赖，可就绪后置）
6. P0-4 CORS + /healthz（开发期便利，可最早）

每项独立成 PR、带表驱动测试，评审后合入 `dev`。

---

## 方案末确认

请审核此方案，回复"通过"或提出修改意见。在收到"通过"前，我不会编写任何代码。
