# 安全策略

## 支持的版本

| 版本 | 支持状态 |
|---|---|
| `main`（开发分支） | ✅ 安全修复持续合入 |
| 最新发布 tag | ✅ |

## 报告漏洞

若发现安全漏洞，**请勿公开提交 Issue**。请通过以下任一方式私下报告：

1. **首选**：GitHub 的 [Private Vulnerability Reporting](https://github.com/chengyifei1991-ai/cadenza/security/advisories/new)（本仓库 Security → Advisories）。
2. 或通过 GitHub 私信联系维护者（见 [GOVERNANCE.md](./GOVERNANCE.md#维护者)）。

请在报告中包含：受影响版本、复现步骤、潜在影响，以及可选的缓解建议。我们会在确认后尽快响应，并在修复发布后给予致谢（如你希望）。

## 安全关注点（本项目特有）

Cadenza 直接管控 Collector 集群，请特别关注以下场景：

- **接入认证**：`OPAMP_AUTH_TOKEN` 为空时对任何来源放行，生产环境务必设置强 token 并置于 TLS 之后。
- **LLM 输出**：对话生成配置经两级校验（yaml.v3 + otelcol-contrib）后仍需审批才能下发；`REQUIRE_APPROVAL=false` 会关闭审批，仅在受信环境使用。
- **密钥管理**：`LLM_API_KEY` 等敏感项仅通过环境变量注入，不得写入仓库（参考 `.env.example`，勿提交真实 `.env`）。
- **审计**：审批/下发动作全程留痕（`/api/v1/audit`），生产环境建议接入只读审计视图。

## 披露流程

1. 报告 → 2. 初步确认（目标 3 个工作日内回应）→ 3. 修复并测试 → 4. 发布安全版本/补丁 → 5. 公开通告并致谢。
