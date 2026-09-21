# 1.1.0（c + d）评审记录

> 状态：评审完成，结论=**通过（无阻塞缺陷）；1 项建议在 e 阶段前修正（F-9 基准来源），其余为一致性/体验项**
> 日期：2026-09-11 ｜ 对象：提交 `acf1b12`（1.1.0-c）、`20e52cb`（1.1.0-d，含前置修复 `f2208c3` 之后的实现）
> 关联：[design-1.1.0.md](./design-1.1.0.md) §3.3–§3.4、[review-1.1.0-ab.md](./review-1.1.0-ab.md)
> 方法：逐提交走查 + 关键路径实测（e2e/真机门禁复跑）+ 语义边界推演（时间精度、游标、基准来源）

## 0. 结论摘要

1.1.0-c（后端筛选与分页）与 1.1.0-d（任务级 diff 服务端化）按设计 §3.3/§3.4 交付，
实现路径与既有约定一致（裸数组/分页信封、错误码语义、Store 过滤结构体）。

上一轮评审的 F-1 缺陷已修复并固化为回归门禁（秒级半开区间 + 边界用例）；
F-8 语义陷阱被显式规避：审计 `since` **保持审计行 id 游标**语义，时间过滤新增 `from`/`to`。

本轮新发现 1 项值得在 e 阶段前处理的问题：**diff 基准来源不确定**（意图值与 Agent 上报值混用）。

## 1. 自动化验证（评审时复跑）

| 层 | 结果 |
|---|---|
| `go vet` / `go test ./...` | ✅ 干净 / 全绿（新增 diff 11 组黄金样例、store 基准快照与迁移、api diff/消息/审计端点、agent 工具） |
| 黑盒 e2e（simple / off） | ✅ **121** / **110**（新增第 17 节审计筛选与消息分页、第 18 节任务级 diff） |
| 真机门禁（真实 otelcol-contrib） | ✅ **17/17** |
| 前端 typecheck + Vitest | ✅ 通过（32 项；CI 已门禁） |

## 2. 与设计文档逐条对照

| 设计节 | 交付 | 状态 |
|---|---|---|
| §3.3 任务侧筛选（D4） | `TaskFilter`（status/type/target/session_id/since/until）+ 400 校验 + 前端服务端筛选 | ✅ |
| §3.3 审计筛选 | `AuditFilter`（actor/action/subject/from/to）+ 保留 `since` id 游标 + 动作枚举校验 | ✅ |
| §3.3 会话消息分页 | `ListMessages` keyset（尾部窗口/before_id/after_id）+ `GET /sessions/{id}/messages` + 助手页分页 UI | ✅（UI 见 F-12） |
| §3.4 diff 服务端化 | `internal/diff`（stdlib）+ `Task.BaseYAML` 快照 + `GET /tasks/{id}/diff` + MCP `get_task_diff` + UI 优先消费 | ✅（基准来源见 F-9） |
| §3.5 埋点量化（e） | —— | ⬜ 未开始（下一阶段） |
| §6.2 SSE | —— | ⬜ 按设计延后 |

## 3. 发现与处置建议

| # | 发现 | 级别 | 证据 | 建议 |
|---|---|---|---|---|
| **F-9** | **diff 基准来源不确定**：`BaseYAML` 取的是存储层 `collectors.effective_config`，而该字段被两处写入——下发时 `recordConfigVersion` 写入**服务端意图值**、Agent 上报时 `onMessage` 覆盖为**实况值**。因此同一任务在不同时刻创建，基准可能是"上一次下发的配置"而非"Collector 正在跑的配置"，diff 语义会漂移 | P2（建议 e 阶段前修正） | `internal/agent/tools.go` recordConfigVersion 与 `internal/opampserver/server.go:190` 双写点 | 基准优先取 `registry.ReportedEffective(uid)`（1.0.1 已引入的"仅 Agent 上报"通道），回退存储值；并在 diff 响应里加 `base_source: reported|intent|none` 明示来源 |
| **F-10** | diff 端点与 MCP 工具会返回**基准配置原文**：服务端意图值是用户提交的原始 YAML，可能含密钥/Token（Agent 上报值经 otelcol configopaque 脱敏，但意图值不脱敏） | P3（既有暴露面延伸） | `GET /collectors/{uid}` 本就返回 effective_config；1.1.0-a 的 MCP token 已收敛 MCP 侧 | 文档中标注"配置可能含敏感值，MCP 端点务必启用 token"；后续可考虑对 `Authorization` 类字段做响应脱敏 |
| **F-11** | 消息端点 `before_id` 与 `after_id` 同时传入时 **after_id 优先且无提示** | P3 | `ListMessages` switch 顺序 | 二者互斥时返回 400（或在响应里回显生效游标） |
| **F-12** | 助手页"加载更早"通过**增大 limit**（50→100 后提示"仅显示最近 100 条"）实现，未使用已实现的 `before_id` keyset | P3（体验/能力未用满） | `AssistantPage.tsx` `setMsgLimit` | 后续接入 keyset 累积分页，解除 100 条上限 |
| **F-13** | 审计 `since` 由"非数字静默忽略"变为**严格 400** | P3（有意的兼容性收紧） | 上一轮 F-3 的对齐要求 | 已在 e2e 固化断言；建议在 CHANGELOG"变更"段落点明（下游若传垃圾值会立刻失败） |
| **F-14** | e2e section 编号顺序仍未整理（承接 F-7：16/17/18 位于 14 之前） | P4 | `tests/e2e.sh` | 一次性重排编号（纯可读性） |

## 4. 正确性复核（已逐点验证，未发现问题）

- **时间过滤**：任务与审计共用秒级半开区间 `[from, to+1s)`，`substr(created_at,1,19)` 纯字符串比较（SQLite/MySQL 通用）；上一轮 F-1 的两个反例均已转绿并固化为用例。
- **diff 算法**：LCS 动态规划 + 上下文合并；hunk 头遵循 git 惯例（`count==0` 时起始行取变更前一行，空侧为 0）；CRLF 归一化；大输入（>4M 单元格）降级为整块替换，耗时/内存有界。
- **排序确定性**：任务列表 `ORDER BY substr(created_at,1,19) DESC, id DESC`（ULID 单调兜底），修掉了同秒内因变长小数秒导致的非时间序。
- **迁移安全**：`base_yaml` / `session_id` 均走 `ensureColumn` 幂等迁移；旧库行可读（新列为空串），有专门测试。
- **契约兼容**：无分页参数仍返回裸数组；`limit`/游标越界与非法枚举一律 400；diff 端点用 409 表达"无可比较内容"而非模糊 200。
- **工具面同步**：MCP 工具数 9→10，README 表格与计数同步（"版本/计数是全局事实"纪律）。

## 5. 结论与建议

- **结论**：c、d 通过。无阻塞缺陷；F-9 属语义准确性问题，建议在 e（埋点）之前顺手修正（实现成本低：基准来源改为上报通道优先 + 响应加 `base_source`），并可顺带补 F-11 的游标互斥校验。
- 其余 F-10/F-12/F-13/F-14 登记为后续项（F-12 需前端分页改造，建议与 SSE 评估一并排期）。
