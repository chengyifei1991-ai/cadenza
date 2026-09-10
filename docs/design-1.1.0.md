# Cadenza 1.1.0 设计（首个特性包）

> 状态：草案 v0.1 ｜ 日期：2026-09-09 ｜ 关联：[product-plan.md](./product-plan.md)（§6.1 backlog）、
> [ProjectCharter.md](./ProjectCharter.md)、[design-web-p0.md](./design-web-p0.md)、[design-v1.md](./design-v1.md)
>
> 范围：**1.1.0 特性包**（GA 后首个语义化版本）。全部改动为**增量、向后兼容**，
> 不破坏 1.0.0 对外协议面（既有端点语义不变，仅新增端点/参数/字段）。
> 按宪章两阶段流程：本文档评审通过后，才进入编码（每项带表驱动 Go 测试）。

---

## 1. 目标与范围

1.1.0 收敛自三个来源：① product-plan §6.1 backlog；② 1.0.0 阶段各里程碑评审遗留项；
③ 公网部署前必须解决的安全敞口。目标用户可见收益：

- **安全**：消除"/mcp 无鉴权、外部 LLM/IDE 可触发审批"的公开敞口。
- **链路可追踪**：任务与 AI 会话**硬绑定**，"我在这轮对话创建的任务"不再靠文本正则碰运气。
- **列表可用性**：任务/审计/会话消息的筛选与分页**后端化**，长数据不拖垮页面与接口。
- **可审计可量化**：任务级 diff 服务端化（含分组目标）、变更效率最小埋点。

**不做（见 §6 单独列）**：SSE 流式（默认延后）、RBAC/多租户（1.2.0）、分组管理 UI（1.2.0）。

## 2. 设计决策

| # | 决策点 | 结论 | 影响 |
|---|---|---|---|
| D1 | MCP 鉴权形态 | **可选 Bearer token**（`MCP_AUTH_TOKEN`，空 = 不启用保持现状）；常量时间比较；不引入用户/角色 | 中间件只包 `/mcp`；off 模式不影响；system/info 增加 `mcp_auth` 布尔 |
| D2 | 任务↔会话绑定方式 | `Task.SessionID` 列（幂等迁移）；会话上下文经 **ctx 下传**给 Agent 工具；REST 建任务支持可选 `session_id` | 工具层无签名破坏；MCP 直调无会话 → 空串（语义：非会话内发起） |
| D3 | 会话消息分页 | 新增 `GET /sessions/{id}/messages`（chat_messages 自增 id keyset）；`GET /sessions/{id}` 全量语义保留 | 不改旧端点；UI 长会话切分页读取 |
| D4 | 筛选后端化 | 任务/审计 Store 方法签名改为**可选过滤结构体**（零值 = 不过滤）；查询参数向后兼容 | 接口层封装过滤参数，Store 统一处理；前端过滤逻辑删除 |
| D5 | 任务级 diff 服务端化 | 分组任务落库时快照**基准配置**（组内代表实例 current 或模板）；后端用**自研行级 diff（stdlib，无新依赖）**产出 unified diff | 拒绝为 diff 引第三方依赖（宪章：未批准不引依赖）；jsdiff 前端渲染仍可用 |
| D6 | 效率埋点 | 新增 `task_events` 表（每次状态迁移落一条带时间戳）；`/api/v1/stats/ops` 聚合 | 全部状态变更收敛到 task.Service 单一入口落事件，避免散点打点 |
| D7 | SSE 流式 | **默认不进 1.1.0**：ReAct 循环下真流式需消息级切分，收益/成本比低；仅记设计备忘（§6.2） | backlog 内保持"可选"，不在本包承诺 |

## 3. 逐项设计

### 3.1 MCP 鉴权 token（P0，安全）

**现状**：`router.go` 中 `/mcp` 直接挂 `mcpSrv.Handler()`，无任何鉴权；外部 LLM/IDE
可经 MCP 触发 `approve_task`/`reject_task`（风险表已登记，1.0.0 以"可信网络"文档声明接受）。

**方案**：
- 配置：新增 `MCP_AUTH_TOKEN`（config 层，空默认）。启动时若为空，log **warning**
  （中文可读、内部日志英文惯例保留英文 message），提示公网部署风险；为 non-empty 时启用校验。
- 中间件：包在 `/mcp` handler 外，读取 `Authorization: Bearer <token>`，
  与配置值做 **crypto/subtle.ConstantTimeCompare**；不匹配返回 401 JSON（MCP 客户端可见错误）。
- `/api/v1/system/info` 响应增加 `"mcp_auth":true|false`（公开端点，供前端设置页展示）。

**测试**：表驱动——无 token 配置 / 缺失头 / 错误 token / 正确 token / 常量时间比较；中间件单测覆盖 200/401。
**验收**：设 `MCP_AUTH_TOKEN` 后未带 token 的 MCP initialize 返回 401；带正确 token 全工具可用；e2e.sh 增补两行断言。

### 3.2 任务 ↔ 会话硬绑定（P1）

**现状**：`Task` 无 `SessionID`；AI 助手"任务联动"靠回复文本正则提取任务 id（尽力而为）。
`AgentRun.SessionID` 已有会话维度，但 `generate_config`/`optimize_config` 工具建任务时拿不到会话。

**方案**：
- 模型与存储：`store.Task` 增 `SessionID string json:"session_id"`；`tasks` 表
  `ensureColumn("tasks","session_id","TEXT NOT NULL DEFAULT ''")` + `CREATE INDEX IF NOT EXISTS idx_tasks_session`；
  CreateTask/GetTask/ListTasks 的读写同步（sqlstore 结构体 scan 全字段）。
- 会话下传：`Orchestrator.Chat` 在 ReAct 循环前 `context.WithValue(ctx, sessionKey, sess.ID)`，
  循环内 `generate`/`callTool` 全部使用该派生 ctx；Agent 工具 `generateYAML`/`optimize` 建任务时
  从 ctx 读 session id 回写（无值 = 空串，MCP 直调兼容）。
- REST 入口：`POST /api/v1/tasks/rollback` 与 `POST /api/v1/tasks/apply` 请求体支持可选 `session_id`
  （前端"从此会话发起回滚/下发"场景回传）；沿用 `approverOf` 的宽松校验模式（空则忽略）。
- 读取面：任务详情 JSON 已含 `session_id`；新增 `GET /api/v1/sessions/{id}/tasks`（同 3.3 过滤器，`session_id` 等值）；
  AI 助手回复文本仍保留任务 id（兼容旧会话），前端任务卡片优先走 session 绑定。

**测试**：表驱动覆盖——Orchestrator ctx 下传后工具建任务 session 正确；MCP 直调为空串；
REST rollback/apply 带/不带 session_id；旧库（无该列）启动迁移成功且数据不丢。
**验收**：对话生成后，会话详情可见"本会话创建的任务"列表；任务详情可回跳所属会话。

### 3.3 后端筛选与分页（P1）

**现状**：`ListTasks(status,page,pageSize)`、`ListAudit(since,page,pageSize)` 单一维度；
`GET /sessions/{id}` 全量返回消息（长会话拖慢）。

**方案**：
- Store 层新增可选过滤结构体（保持零值=不过滤，全部追加，签名替换）：

  ```go
  type TaskFilter struct {
      Status   TaskStatus
      Type     TaskType
      Target   string // 匹配 target_instance_uid 或 target_group_id（任一非空即筛）
      SessionID string
      Since, Until time.Time // 按 created_at 区间
  }
  type AuditFilter struct {
      Actor, Action, Subject string
      Since, Until time.Time // 按 created_at 区间
  }
  // ListTasks(ctx, f TaskFilter, page, pageSize int)([]Task,int64,error)
  // ListAudit(ctx, f AuditFilter, page, pageSize int)([]AuditLog,int64,error)
  ```

  SQL 端 WHERE 动态拼接（参数化，禁止字符串注入）；MySQL/SQLite 双驱动共用。
- HTTP 层：`GET /api/v1/tasks?status=&type=&target=&session_id=&since=&until=&page=&page_size=`
  与 `GET /api/v1/audit?actor=&action=&subject=&since=&until=&page=&page_size=`；
  时间参数 `since`/`until` 接受 **RFC3339 或 Unix 秒（纯数字）**，共享解析器统一处理——
  审计页既有 `since=<unix 秒>` 调用保持兼容，tasks 无旧参可直接用 RFC3339。
- 会话消息分页：`GET /api/v1/sessions/{id}/messages?after_id=&before_id=&limit=`
  返回 `{items:[{id,role,content,created_at}],total}`，id 升序 keyset（chat_messages 自增 id 现成）；
  `GET /sessions/{id}` 行为不变（供一次性小会话）。

**测试**：Store 层表驱动（各过滤器单维度 + 组合 + 空结果 + 分页边界）；handler 层 400 参数校验；
旧调用（无任何过滤参数）返回与原语义一致。
**验收**：任务中心/审计页筛选全部走服务端（前端删除本地过滤）；1 万条消息会话详情接口 < 200ms。

### 3.4 任务级 diff 服务端化（P2）

**现状**：diff 仅前端 jsdiff（Web 页面内对单实例任务可用）；分组目标（generate/optimize
任务下发到组）无实例基准，diff 不可用；REST/MCP 消费方拿不到 diff。

**方案**：
- 分组基准快照：`generate`/`optimize` 工具落任务时，若 `TargetGroupID` 非空，取组内
  代表 Collector（`ListCollectors` 按组取最近上报一台）的 `EffectiveConfig` 作为
  `Task.BaseYAML`（任务列 `base_yaml`，幂等迁移）；无代表实例则空（diff 显示"无基准"）。
- 服务端 diff 实现：`internal/diff` 包——**stdlib 自研行级 diff**（Myers O(ND)，
  与 jsdiff 同族算法，输出 unified 文本）；不引第三方依赖。
  函数 `func Unified(a, b string) string`（带文件头行号、`+`/`-`/` ` 前缀，兼容 jsdiff 前端既有 DiffView 渲染）。
- 出口：`GET /api/v1/tasks/{id}/diff` 返回 `{base_yaml,generated_yaml,diff}`（任务无 generated 内容时 409/空体）；
  MCP 新增工具 `get_task_diff`（入参 task_id）同一实现。
- 前端：任务详情 diff 卡优先调用该端点（单实例任务也统一走后端），DiffView 组件不动。

**测试**：diff 包黄金样例表驱动（增删改/空/完全相同/尾换行边界——吸收 jsdiff 历史坑）；
diff 端点 handler 测试；MCP 工具注册测试。
**验收**：分组任务详情可见基准 vs 生成的 unified diff；e2e.sh 对分组任务增补 diff 断言。

### 3.5 变更效率埋点（P2）

**现状**：Task 仅 `created_at/updated_at`，无分步时间戳；效率类指标无从量化。

**方案**：
- 新表 `task_events`（initSchema 语句列表追加，`IF NOT EXISTS` 对旧库幂等）：

  ```sql
  CREATE TABLE IF NOT EXISTS task_events (
      id INTEGER PRIMARY KEY AUTOINCREMENT,   -- MySQL: BIGINT PRIMARY KEY AUTO_INCREMENT
      task_id TEXT NOT NULL,
      from_status TEXT NOT NULL DEFAULT '',
      to_status TEXT NOT NULL,
      created_at TEXT NOT NULL
  );
  CREATE INDEX IF NOT EXISTS idx_task_events_task ON task_events(task_id);
  ```

- 落点：task.Service 内**唯一状态迁移助手** `transition(ctx, task, to)`（现状各流转点收敛之），
  写库后追加事件；旧路径调用点逐一改走该助手（改动面小，见 §5 拆分）。
- 聚合：`GET /api/v1/stats/ops?window_days=7`（默认 7）返回：

  ```json
  {"window_days":7,
   "tasks":{"total":n,"done":n,"failed":n,"success_rate":0.0},
   "approval_wait_ms":{...p50...},
   "dispatch_ms":{...p50...},
   "rollback":{"count":n,"avg_duration_ms":0}}
  ```

  语义：审批等待 = create→approve 事件间隔；下发时长 = 审批/创建（免审批直达）→done；
  回滚平均时长 = rollback 任务 create→done。SQL 侧用 `strftime`(sqlite)/时间差函数(mysql)
  双驱动兼容（沿用现有驱动分支惯例）。

**测试**：transition 幂等/重复目标状态拒绝；事件落库表驱动；stats/ops 手工窗口数据校验 p50；
双驱动查询函数单测。
**验收**：演示库跑通一轮"生成→审批→下发→回滚"后 stats/ops 数值与直觉一致；Dashboard 后续版消费。

## 4. 数据模型与迁移汇总

| 变更 | 类型 | 兼容性 |
|---|---|---|
| `tasks` 增 `session_id`、`base_yaml` | ensureColumn 幂等 | 旧库自动加列，默认 '' |
| `tasks` 增 `idx_tasks_session` | 新索引（initSchema 追加） | 幂等 |
| 新表 `task_events` + 索引 | CREATE IF NOT EXISTS | 旧库启动即建 |
| `store.Task`/`SessionSummary` 等模型字段 | 纯增量 json | 旧客户端忽略新字段 |
| REST 查询参数 | 全部可选追加 | 旧请求零参数语义不变 |

Store 接口签名变更（TaskFilter/AuditFilter）为**内部接口**变更，同步 sqlstore + handlers + 既有测试；
对外 HTTP 契约只增不改。

## 5. 里程碑拆分（每步独立可测可发布）

| 步 | 内容 | 门禁 |
|---|---|---|
| **1.1.0-a（安全）** | 3.1 MCP token | go test + e2e.sh 增补通过 |
| **1.1.0-b（绑定）** | 3.2 会话绑定 + 会话任务列表 | 上述 + ui 手测/冒烟补会话联动 |
| **1.1.0-c（列表）** | 3.3 后端筛选分页 + 消息分页 | 表驱动 + 前端切服务端筛选 |
| **1.1.0-d（diff）** | 3.4 diff 服务端化 + MCP 工具 | diff 黄金样例 + e2e 分组 diff |
| **1.1.0-e（埋点）** | 3.5 task_events + stats/ops | 事件单测 + 手工窗口校验 |

步与步之间独立发版（1.1.0 内按小步打 tag 可选项），任一步失败不阻塞其余。

## 6. 不做 / 延后

### 6.1 明确不做（1.2.0+）
- RBAC/多租户/OAuth/SSO、分组管理 UI 与批量下发、审计导出、完整 EN、暗色模式（product-plan §6 映射 1.2.0）。
- 前端"状态流转时间线"展示：依赖 task_events 数据成熟后（1.2.0 UI 化）。

### 6.2 SSE 流式设计备忘（不进 1.1.0，除非专项批准）
现状 `model.GenerateContent` 已是 channel 流式；ReAct 循环按"整轮响应"推进，
若做逐 token 转发需把循环切成"首个无工具文本的增量推送 + 工具调用期不发"语义，涉及
handler/编排层较大改动与前端双模式（JSON/SSE）兼容。**建议**：1.1.0 保持 POST 全量；
1.2.0 评估 `POST /api/v1/chat/stream`（Accept: text/event-stream）时以本备忘为起点。

## 7. 风险与对策

| 风险 | 等级 | 对策 |
|---|---|---|
| `ensureColumn`/动态 WHERE 双驱动不一致 | 中 | 迁移与查询函数双驱动单测先行；e2e 只跑 sqlite，MySQL 以单测覆盖 |
| 会话下传 ctx 引入跨会话串扰 | 低 | ctx 键为包内私有类型；工具取不到值即空串（fail-safe） |
| 自研 diff 有边界缺陷 | 中 | 吸收 jsdiff 历史边界（尾换行/空文件）进黄金样例；前端仍可回退本地渲染 |
| task 状态改走 transition 遗漏散点 | 中 | 编码时 grep 全部 `Status =` 赋值点收敛；测试断言事件数与流转一致 |
| MCP token 空配置默认开放 | 中 | 维持 1.0.0 风险声明 + 启动 warning；compose 文档给示例；不强制（避免破坏开箱即用） |

## 8. 验收映射（评审后逐项对照）

| PRD/规划出处 | 设计节 | 可测验收 |
|---|---|---|
| §6.1 backlog · MCP 鉴权 token | 3.1 | 设 token → 未带 401 / 带对 200；e2e 断言 |
| §6.1 backlog · 任务↔会话回写绑定 | 3.2 | 会话详情任务列表一致；任务详情回跳会话 |
| §6.1 backlog · 后端筛选与分页 | 3.3 | 任务/审计组合筛选与消息 keyset 分页表驱动 + 前端无本地过滤 |
| §6.1 backlog · 任务级 diff 服务端化 | 3.4 | 分组任务 unified diff；MCP get_task_diff |
| §6.1 backlog · 埋点量化 | 3.5 | task_events 齐全；stats/ops 数值手验 |
| §6.1 backlog · 会话流式（可选） | §6.2 | 不进本包；延后评估 |
