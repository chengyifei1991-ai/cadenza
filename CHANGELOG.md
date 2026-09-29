# 变更日志

本项目的显著变更记录于此。格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### 修复

- **SQLite 忙锁导致任务卡死（严重）**：并发写入（HTTP 处理器 + OpAMP 状态上报 + 1.1.0-e 新增的
  埋点事件）触发 `database is locked (SQLITE_BUSY)`，审批请求返回 400 且任务**永久停在 applying**。
  根因：`busy_timeout`/`journal_mode` 是**每连接**属性，仅在 open 后 Exec 一次只对池中一条连接生效；
  现状改为把 pragma 写进 DSN（连接池每条新连接均生效）并限制 SQLite 连接数，写路径增加忙锁有限重试；
  同时补齐兜底：完成任务状态写入失败时尽力标记 `failed`，不再让任务停留在中间态。


### 变更

- **参数校验收紧（1.1.0-c）**：任务列表 `status`/`type` 非法枚举、审计 `action` 非法枚举、
  时间参数非法、`since` 非整数一律返回 400（此前部分场景静默忽略或返回空结果）；
  审计 `since` 保持"审计行 id 游标"语义，时间区间请用新增的 `from`/`to`。
- **基准来源标注（1.1.0-d）**：任务级 diff 的 `base_yaml` 优先取 Agent 权威上报值并新增
  `base_source`（`reported`/`store`/空）字段，避免把服务端下发意图值误认作"正在运行的配置"。


### 新增

- **任务 ↔ 会话硬绑定（1.1.0-b）**：`Task` 新增 `session_id`（旧库幂等迁移 + 索引）——
  Orchestrator 在 ReAct 循环前把会话 ID 注入 ctx，Agent 工具创建任务时回写；REST
  `apply`/`rollback` 支持可选 `session_id`；新增 `GET /api/v1/sessions/{id}/tasks` 与
  `GET /api/v1/tasks?session_id=`，前端助手页改为"会话绑定任务优先 + 文本正则兜底"，
  任务详情可回跳所属会话（`/assistant?session=<id>`）。
- **任务列表后端筛选（1.1.0-c 前置）**：`TaskFilter` 化 `Store.ListTasks`，支持
  `status`/`type`/`target`/`session_id`/`since`/`until`（时间接受 RFC3339 或 Unix 秒）；
  非法时间参数返回 400。
- **前端单测接入 CI 门禁**：新增 typecheck + `vitest run` 步骤；修复 `AssistantPage.send()`
  未捕获拒绝（fire-and-forget 调用产生的 2 处 unhandled rejection，此前 `npm test` 退出码为 1）。

- **变更效率埋点（1.1.0-e）**：新增 `task_events` 表（幂等建表 + 索引），任务状态机在
  `task.Service` 的每个迁移点落事件（创建/审批/拒绝/状态推进，best effort 不阻塞主流程）；
  新增 `GET /api/v1/stats/ops?window_days=7`（1~90）：窗口内任务终态分布与**下发成功率**、
  **审批等待**与**下发时长**的 count/avg/p50/p90、**回滚**次数与平均时长；演示库注入成对的
  迁移事件，开箱即可看到指标。占位量级访问不产生额外查询（窗口过滤走在同一个索引上）。
- **任务级 diff 服务端化（1.1.0-d）**：新增 `internal/diff`（stdlib LCS 实现的 unified 行级
  diff，无第三方依赖，大输入自动降级）；`Task.BaseYAML` 基准快照（generate/optimize 落库时取
  目标分组代表实例的生效配置，REST apply 取目标当前生效配置，跨时间可复现）；
  新增 `GET /api/v1/tasks/{id}/diff`（无生成配置 409、未知任务 404）与 MCP 工具 `get_task_diff`
  （工具数 9 → 10，README 同步）；前端任务详情改为优先消费服务端 diff（分组目标也能看差异，
  `DiffView` 支持预计算结果），旧的前端本地对比路径保留为回退。
- **MCP 鉴权 token（1.1.0-a）**：新增 `MCP_AUTH_TOKEN` 配置——非空时 `/mcp` 要求
  `Authorization: Bearer <token>`（常量时间比较，未认证返回 401 中文错误）；为空时保持 1.0 的
  开箱即用行为并在启动日志告警。`/api/v1/system/info` 新增 `mcp_auth` 标记供前端设置页展示；
  compose/.env.example/README 同步该变量。


## [1.0.1] - 2026-09-10

### 新增

- **下发生效确认**（`已下发 ≠ 已生效`）：审批下发后等待 Agent 的生效回报——标准客户端走
  `RemoteConfigStatus(APPLIED)` 哈希比对，opampextension 这类只上报展开后 effective 的客户端
  走"推送内容 ⊆ 上报内容"语义子集判定；超时在任务上告警"已下发但未收到生效确认"，不再静默成功。
- **重启命令补发**：首段未确认且 Agent 声明 `AcceptsRestartCommand` 时，按协议先发 RemoteConfig、
  再**单独**补发 `RestartCommand`（同消息带 Command 时客户端会忽略 RemoteConfig）。
- **真机门禁**：`tests/real-collector-gate.sh` + `tests/supervisor-fixture`（opamp-go 实现的最小
  supervisor），用真实 otelcol-contrib 断言"进程级换配置"：重启、端口迁移、effective.yaml 重写、回滚、审计。

### 修复

- OpAMP 服务端在缺少上报时置 `ReportFullState` 标志：修复 Collector 重连后只发增量状态导致
  effective config 缺失、状态被判为 unknown。
- 配置深度校验在内容含 opamp 时附带 `extension.opampextension.RemoteRestarts` 特性门，
  避免声明 `accepts_restart_command` 的合法配置被误判为非法。


## [1.0.0] - 2026-09-09

### 新增

- Web 前端阶段（单二进制内嵌控制台，`go:embed`）：M0–M4 里程碑落地。
  - 单管理员登录（`WEB_AUTH_MODE=simple`，bcrypt + HttpOnly 会话）；免登（`off`）仅限本地/演示。
  - 页面：登录/仪表盘（聚合统计 + 状态分布 + 最近任务）/Collectors 列表与详情（版本历史+回滚确认 diff）/
    配置编辑器（js-yaml 语法预检 → 保存并下发）/任务中心与任务详情（变更 diff、审批/拒绝、任务联动）/
    审计日志/AI 助手（会话历史、对话、LLM 降级重发）/首次引导（Onboarding）与设置。
  - REST 配套：`GET /api/v1/collectors/{uid}`、会话列表/详情、`/api/v1/stats`、`/tasks/apply`、
    `/auth/*`、`/system/info`、`/healthz`。
  - 演示数据（`DEMO_MODE=true`）、`CORS_ALLOWED_ORIGINS`、`WEB_DIR/DISABLE_WEB`。
- 可复用测试体系：E2E 黑盒回归（`tests/e2e.sh`，simple/off 双模式）+ 前端 Vitest（纯函数 + DOM）+
  Playwright UI 冒烟（1.0.0 GA 门禁）；`cmd/cadenza-passwd` 口令哈希工具。

### 修复

- 静态资源 SPA fallback：缺失的带扩展名资源返回 404（避免误回退为 HTML）。
- 登录空凭据 400；审批/拒绝不存在的任务返回 404 中文（不再泄漏英文内部错误）。
- 容器健康检查改用公开 `/healthz`（原 `/api/v1/collectors` 在登录保护下会 401）。
- 生产浏览器白屏：前端改单包构建（自定义分包破坏 React 互操作），并加启动自诊断/错误边界/反代超时硬化。

### 变更

- 产品正式发布 **v1.0.0（GA）**：此前以 `v1.0.0-rc.1` 预发布基线收敛，ui-e2e 全绿门禁通过后转为正式版；Docker 镜像 tag 同步为 `cadenza:1.0.0`。

- 开源合规补全：填充 LICENSE 版权声明、新增 `THIRD_PARTY_NOTICES.md` 与 `licenses/`、源码加 SPDX 头、Docker 镜像随附许可文件。
- 新增治理文档：`CONTRIBUTING.md`、`CODE_OF_CONDUCT.md`、`SECURITY.md`、`GOVERNANCE.md`。
- README/宪章顶部增加"非官方 OpenTelemetry/CNCF 项目"免责声明。

## [0.1.0] - 2026-08-16

### 新增

- OpAMP 统一管控后端：HTTP + WebSocket 双传输、接入认证、状态接收、配置主动下发。
- 两级配置校验：yaml.v3 结构校验 + `otelcol-contrib` v0.156.0 深度校验。
- 对话生成配置：自然语言 → LLM 生成 YAML → 校验 → 审批 → 下发。
- 自动优化配置：基于 Collector 上报状态分析并提议优化方案。
- 版本升级：`PackagesAvailable` 协议能力（Beta），任务化审批。
- MCP Server（`/mcp`，9 个工具）与 REST API（`/api/v1/*`）。
- 审批闭环与会话 → 任务 → 审批 → 下发全链路审计。
- LLM 稳定性：超时 → 指数退避重试 → failover 多模型切换 → 熔断 → 缓存 → 故障隔离。
- 配置回滚闭环与存储层分页重构。
