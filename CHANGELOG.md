# 变更日志

本项目的显著变更记录于此。格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### 变更

- 项目更名：`opamp-backend` → **Cadenza**（模块路径 `github.com/chengyifei1991-ai/cadenza`，二进制 `cadenza`）。
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
