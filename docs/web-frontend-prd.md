# Web 前端结构与页面 PRD

> 状态：草案 v0.1 ｜ 日期：2026-09-05 ｜ 关联：[product-plan.md](./product-plan.md)、[design-web-p0.md](./design-web-p0.md)
> 本文件只做结构设计与页面级需求描述，**不包含代码**；里程碑 M1–M4 依此验收。

---

## 1. 技术选型与理由（D4 定案展开）

| 领域 | 选型 | 理由 |
|---|---|---|
| 语言/构建 | **TypeScript(strict) + Vite + pnpm** | 单页后台 + 未来 embed 产物最小化 |
| UI 框架 | **React 18 + react-router（Hash 或 History 依 embed fallback 实现，默认 History）** | D4 拍板 |
| 组件库 | **Ant Design 5 + @ant-design/icons** | 表格/表单/审批流/Tag/Result 全覆盖，中文文档成熟 |
| 服务端状态 | **@tanstack/react-query** | 列表缓存、任务轮询（refetchInterval）、突变后失效（approve/chat 后刷新） |
| 客户端状态 | 轻量（context/auth 模块） | 仅登录态与全局提示，不引重型状态库 |
| 配置编辑器 | **Monaco Editor（yaml 语言）** | YAML 语法高亮 + 错误标记 + 内置 diff，编辑器与 diff 同一心智模型 |
| 图表 | **recharts**（Dashboard 极简图表） | 体积可控，AntD 体系内无强绑定 |
| API 契约 | **openapi-typescript 生成类型** + 手写薄 fetch 封装 | 契约先行防漂移（见 §3） |
| 测试 | Vitest（组件/工具）+ **Playwright**（M4 冒烟 E2E） | 见 §9 |
| 开发联调 | Vite proxy：`/api` → `:8080` | 同源联调，规避 CORS |

## 2. 仓库与构建布局（monorepo，D2/遗留待决 9.2 推荐）

```
cadenza/
  internal/webui/static/   ← 构建产物落地处（go:embed 源，占位文件常驻）
  web/
    src/
      app/         布局、路由、鉴权守卫、全局错误
      api/         契约类型（generated/）+ fetch 客户端 + react-query hooks
      features/    按域分页实现（auth/dashboard/collectors/tasks/audit/chat/settings）
      components/  跨域复用组件（StatusTag/PageTable/DiffView/EmptyState…）
      styles/ i18n/
    e2e/           Playwright 冒烟脚本
    package.json / vite.config.ts / tsconfig.json
```

构建链：`web: pnpm build` → 同步 `dist` → `internal/webui/static` → 根级 `go build` 产出含 UI 单二进制。
开发链：`go run ./cmd/server`（:8080）+ `pnpm dev`（:5173，proxy 到 8080）。

## 3. API 契约矩阵（前端依赖清单）

> 状态列：✅ 已有（v1 后端）｜🆕 M0 P0 新增（见 design-web-p0.md）｜⬜ V1.5+。

| 方法/路径 | 用途 | 状态 | 消费页面 | 刷新策略 |
|---|---|---|---|---|
| POST `/api/v1/auth/login`·`logout`·GET `auth/me` | 登录/登出/会话校验 | 🆕 | 登录、守卫 | 一次性 |
| GET `/api/v1/collectors` | 列表 | ✅ | Collectors、Dashboard | 30s 轮询 |
| GET `/api/v1/collectors/{uid}`（详情需补） | 单实例详情 | ⬜（详情信息可由列表+versions 拼装，M2 评估） | Collector 详情 | — |
| GET `/api/v1/collectors/{uid}/versions` | 版本历史 | ✅ | 版本历史、回滚 | 页面激活刷新 |
| POST `/api/v1/tasks/apply` | 编辑器保存下发 | 🆕 | 配置编辑器 | 突变后跳详情 |
| GET `/api/v1/tasks?status=` | 任务列表 | ✅ | 任务中心、Dashboard | 10s 轮询 |
| GET `/api/v1/tasks/{id}` | 任务详情 | ✅ | 任务详情 | 详情页 5s（awaiting/applying 时） |
| POST `/api/v1/tasks/{id}/approve`·`reject` | 审批/拒绝 | ✅ | 任务详情、待审批入口 | 突变后刷新 |
| POST `/api/v1/tasks/rollback` | 创建回滚任务 | ✅ | 版本历史 | 突变后跳任务详情 |
| GET `/api/v1/audit` | 审计日志 | ✅ | 审计 | 手动刷新/30s |
| POST `/api/v1/sessions` | 建会话 | ✅ | AI 助手 | — |
| GET `/api/v1/sessions`·`/sessions/{id}` | 列表/历史 | 🆕 | AI 助手 | chat 突变后失效 |
| POST `/api/v1/chat` | 对话（同步） | ✅ | AI 助手 | — |
| GET `/api/v1/stats` | 聚合计数 | 🆕 | Dashboard | 30s |
| GET `/healthz` | 探活 | 🆕 | （运维/容器） | — |

## 4. 错误与状态约定（全局一致）

| 信号 | 前端行为 |
|---|---|
| 401 | 登出并回登录页（守卫统一处理）；`auth/me` 401 = 未登录 |
| 503（chat）/ `智能体不可用` | AI 助手页局部降级横幅："智能体暂不可用，请稍后重试；不影响手动操作"，不阻断其余页面 |
| 4xx `{error}`（中文业务文案） | 就地提示 + 表单错误回填（校验错误列表分行展示） |
| 网络错误/超时 | 全局 message + 保留页面状态（react-query 重试退避） |
| 任务状态语义 | 状态机文案与颜色映射收敛为一个 `StatusTag` 组件 + 一张映射表（后端状态即事实来源） |

## 5. 页面级 PRD

> 每页统一要素：**目标 / 数据源 / 关键状态（空·载·错）/ 交互 / 验收要点**，不再逐页重复要素名。

### 5.1 登录页（M1）
- 目标：单管理员登录，成功后入 Dashboard。
- 交互：用户名/密码表单；登录成功写 Cookie；携带 redirect 回跳。
- 验收：错误凭据提示；已登录访问 /login 自动跳 Dashboard；`off` 模式隐藏登录页直接进入并在页脚显示"免登模式"徽标。

### 5.2 主布局与导航（M1，贯穿全站）
- 左侧导航（§product-plan 第 5 节页面地图）；顶部：当前用户、鉴权模式徽标、文档入口、后端版本。
- 验收：路由守卫（未登录重定向）；鉴权模式经 `/auth/me` 元信息下发，勿硬编码。

### 5.3 Dashboard（M4，骨架 M1 先挂 stats）
- 数据源：`/stats`、collectors/tasks 列表第一页。
- 内容：Collector 健康分布（healthy/unhealthy/offline/unknown 卡片+图表）；任务按状态聚合；
  **待审批任务直达入口**（数 > 0 时高亮）；最近 10 条任务/审计速览。
- 验收：数字与列表页口径一致（同一后端计数）；30s 自动刷新；空态引导"接入第一个 Collector"。

### 5.4 Collectors 列表（M1）
- 数据源：`GET /collectors`（分页信封）。
- 内容：instance_uid、hostname、version、status（StatusTag）、last_seen_at、group；筛：状态；搜：uid/主机名子串（前端过滤，V2 后端化）；行点击进详情。
- 验收：分页/筛选/空态；状态与颜色映射表唯一。

### 5.5 Collector 详情 + 版本历史（M2）
- 布局 Tabs：**概览**（status/版本/最近上报/当前生效配置摘要）｜**版本历史**（表格：版本 id、时间、validated 标记、操作=回滚）｜**当前配置**（编辑器只读模式 + YAML 高亮）。
- 回滚：选择版本 → 确认弹窗（展示差异概览：目标版本 vs 当前）→ `POST /tasks/rollback` → 跳任务详情。
- 验收：回滚按钮对当前生效版本禁用；确认弹窗措辞含"将重新下发历史配置"。

### 5.6 配置编辑器（M2，核心页）
- 数据源：Collector 当前配置装载入 Monaco（yaml 模式）；只读目标与可编辑副本分离。
- 交互：编辑 → 本地格式/语法预检 → **"保存并下发"** → `POST /tasks/apply`（note 可填）→ 校验错误行内标记或列表展示 → 成功跳任务详情等待审批。
- 验收：编辑-提交-审批-下发-回滚全链路可点通；服务端 400 校验错误能映射回 YAML 行号提示（尽力而为，错误列表兜底）；LLM 故障时本页**完全可用**（操作面与 AI 面解耦的实证）。

### 5.7 任务中心（M2）+ 任务详情
- 列表：状态筛选（含"待我审批"快捷）、类型 Tag（generate/optimize/apply/rollback/upgrade）、时间、目标；分页。
- 详情：元信息卡（状态机时间线、类型、目标、审批人/拒绝原因/错误/ModelUsed）；**变更内容区**：
  - generate/optimize/apply：`GeneratedYAML` 与目标当前配置的 **diff 视图**（Monaco diff，只读）；
  - rollback：回滚目标版本号与说明；
  - 操作区：`awaiting_approval` 且 `require_approval` → 通过（附审批人=当前用户）/ 拒绝（必填原因）。
- 验收：审批/拒绝后列表与详情即时刷新；`done`/`failed` 有终端可读态（含错误文案与"重试"入口 → 重新生成/再次提交）。

### 5.8 审计（M2）
- 数据源：`GET /audit`（since 时间过滤 + 分页）。
- 内容：时间、actor、action Tag、subject、detail；筛：动作类型。
- 验收：审计不提供编辑操作；过滤与分页可用。

### 5.9 AI 助手（M3）
- 布局：左侧会话列表（新会话 + 标题摘要 = 首条用户消息）｜右侧对话区。
- 数据源：`GET /sessions`、`GET /sessions/{id}`、`POST /chat`（同步，loading 态阻止连发）。
- 消息类型渲染：普通文本；**结构化结果卡片**（当对话产物落成任务时）——卡片含任务号/状态/变更内容入口，点击跳任务详情完成 diff 与审批（对话与任务联动）。
- LLM 降级：503 → 横幅 + 保留输入内容可重发。
- 验收：刷新后历史完整回读；生成→任务→审批闭环可在两页内完成；连发保护；空会话引导语。

### 5.10 设置 / 关于 + Onboarding（M4）
- 设置：鉴权模式（只读展示）、LLM 链路健康（主/备/本地模型与熔断状态——来自后端暴露的最简状态，M4 视后端成本以配置页静态展示兜底）、`DISABLE_WEB` 提示。
- Onboarding（首启 + 可手动重开）：步骤化引导——① 准备一个 Collector（附 opamp extension 片段与复制按钮）② 等首个实例出现在 Collectors ③ 试一次对话生成 ④ 走一次审批下发；配演示数据开关（`--demo`，遗留待决 9.3）。
- 验收：全新用户按脚本 ≤30 分钟跑通（M4 唯一门槛）。

## 6. 状态轮询策略表（react-query）

| 页面 | 查询 | refetchInterval | 说明 |
|---|---|---|---|
| Dashboard | stats / tasks 摘要 | 30s | 后台聚焦时；`document.visibilitychange` 恢复即刷新 |
| Collectors | 列表 | 30s | — |
| 任务中心 | 列表 | 10s | 有非终态任务时 5s，全终态停轮 |
| 任务详情 | 单任务 | 5s（awaiting/applying） | 终态停轮 |
| AI 助手 | 会话列表/详情 | 手动 + 突变失效 | chat 后本地乐观追加 |
| 审计 | 列表 | 30s 或手动 | — |

## 7. 复用组件清单

`StatusTag`（Collector/Task 状态映射唯一源）、`PageTable`（分页信封封装）、`DiffView`（Monaco diff）、
`YamlEditor`（Monaco + 只读/可编辑态）、`EmptyState`、`ErrorResult`、`ConfirmDialog`、`TimeAgo`、
`AuthGuard`、`PollBadge`（连接/刷新指示）。

## 8. 文案与主题
- 中文主文案（错误即后端 `{error}` 原文）；i18n 结构从第一天以 key 化引入，EN 文案 V1 GA 前补快速路径
  （登录/导航/核心动作），完整 EN 待 V2（遗留待决 9.4）。
- AntD 默认主题 + 品牌色微调；暗色模式 V2。

## 9. 质量门槛（每里程碑 Gate）
- `pnpm lint`（eslint+tsc strict）零错误；Vitest 覆盖状态映射/时间格式化/契约解析等纯逻辑。
- `pnpm build` 产物 + `go build` 单二进制可启动（CI 串联验证 embed 生效）。
- M4 前 Playwright 冒烟：登录 → 列表 → 编辑器提交 → 审批 → 回滚 + AI 助手历史回读，5 条主链路。
- 无 `console.log` 残留；基础 a11y（表单 label、键盘可达）。

## 10. 里程碑验收对照（M1–M4）

| 里程碑 | 本文件交付对照 |
|---|---|
| M1 | §5.1 登录、§5.2 布局、§5.4 列表、§5.3 骨架、§3 契约打通（🆕 端点依赖 M0 后端合入） |
| M2 | §5.5 详情/版本/回滚、§5.6 编辑器、§5.7 任务中心、§5.8 审计 |
| M3 | §5.9 AI 助手（依赖 P0-2 会话读接口） |
| M4 | §5.3 Dashboard 完整、§5.10 设置/Onboarding、§9 Playwright、演示数据 |

## 11. 实施前置依赖清单（M0 对齐）
1. design-web-p0.md 方案审核通过并合入（auth/sessions/apply/stats/static/CORS）。
2. 本 PRD 评审：页面清单与信息架构冻结。
3. OpenAPI 契约初版入库（handlers 对应关系人工核对），前端类型生成打通。
4. `web/` 骨架 PR（Vite+React+AntD+router+proxy+CI 占位）。
