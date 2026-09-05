# 变更日志

本项目的显著变更记录于此。格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]


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
  Playwright UI 冒烟（V1 GA 门禁）；`cmd/cadenza-passwd` 口令哈希工具。

### 修复

- 静态资源 SPA fallback：缺失的带扩展名资源返回 404（避免误回退为 HTML）。
- 登录空凭据 400；审批/拒绝不存在的任务返回 404 中文（不再泄漏英文内部错误）。
- 容器健康检查改用公开 `/healthz`（原 `/api/v1/collectors` 在登录保护下会 401）。

### 变更

- 产品版本升至 **1.0.0**（V1 GA 基线）。

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
