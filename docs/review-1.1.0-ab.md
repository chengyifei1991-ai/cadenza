# 1.1.0（a/b + c 前置）评审记录

> 状态：评审完成，结论=**有条件通过：1 项必须修复（P1 时间过滤漏行）、1 项建议修复（P2 MCP 401 缺 WWW-Authenticate）**，其余为排期/一致性项
> 日期：2026-09-11 ｜ 对象：提交 `2b5f0a1`（1.1.0-a）、`760dfa8`（1.1.0-b + c 前置）
> 关联：[design-1.1.0.md](./design-1.1.0.md) §3.1–§3.3、[product-plan.md](./product-plan.md) §6.1
> 方法：逐提交静态走查 + 边界反例实测（临时测试取证后删除）+ 既有门禁复跑 + 与设计文档逐条对照

## 0. 结论摘要

1.1.0-a（MCP 鉴权）与 1.1.0-b（任务↔会话硬绑定）实现与设计一致，增量向后兼容；
1.1.0-c 的**任务侧**（TaskFilter + HTTP 参数）已提前落地并可用。

但本轮走查发现 **1 个真实缺陷**：`since/until` 时间过滤用 `RFC3339Nano` 文本直接比较，
而该格式的**小数秒长度可变**，导致边界处**静默漏行**（双向，已实测复现）。
另发现 MCP 401 缺少 `WWW-Authenticate` 头，真实 MCP 客户端无法完成鉴权发现。

## 1. 自动化验证（评审时复跑）

| 层 | 结果 |
|---|---|
| `go vet` / `go test ./...` | ✅ 干净 / 全绿 |
| 黑盒 E2E（simple / off） | ✅ **98** / **87**，0 失败（含本轮新增会话绑定与筛选断言） |
| 前端 typecheck + Vitest | ✅ 通过（32 项）；本轮已接入 CI `test` job |
| 真机门禁（真实 otelcol-contrib） | ✅ **17/17**（下发生效/端口迁移/回滚/审计） |

## 2. 与设计文档逐条对照

| 设计节 | 交付 | 状态 |
|---|---|---|
| §3.1 MCP token（D1） | `MCP_AUTH_TOKEN` + 常量时间比较 + `system/info.mcp_auth` + compose/.env/README | ✅ 完成 |
| §3.2 任务↔会话绑定（D2） | `Task.SessionID` + 幂等迁移/索引 + ctx 下传 + REST `session_id` + `/sessions/{id}/tasks` + 前端联动 | ✅ 完成（遗留见 F-4/F-5） |
| §3.3 任务侧筛选（D4） | `TaskFilter`（status/type/target/session_id/since/until）+ HTTP 参数 + 400 校验 | ⚠️ 完成但含 F-1 缺陷 |
| §3.3 审计筛选 / 会话消息分页 | —— | ⬜ 未开始（c 剩余；注意 F-8 语义陷阱） |
| §3.4 / §3.5 / §6.2 | —— | ⬜ d / e / SSE 未开始（符合排期） |

## 3. 发现与处置建议

| # | 发现 | 级别 | 证据 | 建议 |
|---|---|---|---|---|
| **F-1** | `since/until` 文本比较在**小数秒长度不一致**时静默漏行：`since=…T08:00:00Z` 漏掉 `…T08:00:00.5Z`（实测命中 1，应 2）；`until=…T08:00:00.2Z` 漏掉 `…T08:00:00Z`（实测命中 1，应 2） | **P1（必须修，阻塞 1.1.0 定版）** | 临时表驱动测试取证：`RFC3339Nano` 变长小数秒 + 字典序比较（`'Z'(0x5A) > '.'(0x2E)`） | 二选一：(A) 秒级半开区间 `substr(created_at,1,19) >= ?` 且 `substr(created_at,1,19) < nextSec(until)`——跨 SQLite/MySQL 纯字符串、确定、上界按秒向上取整（实测修正 since 方向）；(B) 新增可排序 `created_at_ms` 列（最精确，迁移+回填成本高）。**并补边界回归测试**（当前 e2e 从未触碰小数秒边界，属门禁盲区） |
| **F-2** | MCP 401 缺 `WWW-Authenticate` 头（实测响应仅 `Content-Type/Date/Content-Length`） | P2 | `curl -D -` 响应头 | 加 `WWW-Authenticate: Bearer realm="cadenza-mcp"`（必要时 `error="invalid_token"`），否则 MCP 客户端（Claude Desktop/SDK）无法做鉴权发现，只能笼统失败 |
| **F-3** | 非法枚举静默返回空：`?status=bogus` / `?type=bogus` → 200 `[]`，与时间参数 400 的处理不一致 | P3 | 实测 http=200 | 与时间参数对齐：未知枚举返回 400 并在错误里给出合法取值，避免"筛无结果"被误读为"确无数据" |
| **F-4** | 助手页只**挂载时**读一次 `?session=`，URL → state 无同步（浏览器前进/后退或在已挂载状态下切换参数不会切会话） | P3 | `useState(searchParams.get("session"))` + 仅 `currentId → URL` 单向 effect | 增加 `useEffect([searchParams])` 反方向同步（注意避免与现有 effect 互相触发，用值比较做幂等） |
| **F-5** | demo 种子任务未绑定 demo 会话（`SessionID` 为空），演示模式看不到"本会话任务"，ui-e2e 也未覆盖该路径 | P3 | `internal/demo/demo.go`：`sess` 已建但任务未回写 | Seed 时把 generate/optimize 演示任务绑定到 demo 会话，顺带覆盖 UI 路径 |
| **F-6** | `/sessions/{id}/tasks` 用 `GetSession` 校验存在性，会**全量加载该会话消息**（长会话 O(messages) 开销） | P3 | `sessions.go` 调用 `GetSession` | 改用轻量存在性查询（`EXISTS`/`COUNT`）或在 Store 增加 `SessionExists` |
| **F-7** | e2e 新增 section 编号 "16" 排在 "14" 之前（沿用既有 15 的做法） | P4（可读性） | `tests/e2e.sh` | 下次整理时统一重排编号 |
| **F-8** | `ListAudit` 现有 `since` 实为**审计行 id 游标**（`WHERE id > ?`），而设计 §3.3 计划把 `since` 语义升级为时间；若直接按 RFC3339/Unix 秒解析会把 id 当时间，静默错筛 | P2（c 阶段陷阱，未实现前无线上影响） | `sqlstore.go` ListAudit + handlers `Sscanf("%d")` | c 阶段新增 `from`/`to`（或 `since_time`）时间参数，**保留** `since` 的 id 游标语义；在文档与测试中显式固化两种语义 |

## 4. 安全与兼容性核对（未发现新问题）

- **MCP 守卫覆盖**：`/mcp` 精确路径挂中间件，`/mcp/`、`//mcp` 不落在守卫外；CORS 预检（OPTIONS）先于鉴权返回 204，符合语义。
- **无自锁**：内置 AI 助手走进程内工具调用，不经 `/mcp`，配置 token 后助手仍可用。
- **不泄漏**：token 仅进日志判定，不落库/不回显；`system/info` 只暴露布尔。
- **迁移安全**：`ensureColumn` + 索引幂等；**旧库任务可读**（`session_id` 为空）已有测试覆盖；`UpdateTask` 全部调用点均为"先 `GetTask` 后写"，不会清空 `session_id`（逐点核对 6 处）。
- **向后兼容**：`ListTasks` 签名变更仅限模块内部接口；外对 HTTP 保持"无分页参数 = 裸数组、有参数 = 信封"的既有约定。

## 5. 结论与建议

- **结论**：有条件通过。F-1 属正确性缺陷且落在已交付能力上，建议**修复后再推进 c/d/e 并定版 1.1.0**；F-1 的修复应同时把"小数秒边界"固化为回归用例（本仓库"SOP：修复固化为门禁"）。
- F-2 成本极低（1 行响应头 + 断言），建议一并修；F-3/F-4/F-5/F-6 可作为 c 阶段顺手项；F-8 必须在 c 设计审计筛选时显式决策。
