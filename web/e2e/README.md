# UI E2E（Playwright，V1 GA 门禁）

真实浏览器冒烟：登录 → 仪表盘 → Collectors 详情/版本历史 → 配置编辑器 → 审批闭环 → AI 助手。

```bash
# 前置：单二进制已含最新 UI（构建/embed 见仓库根），并已构建口令工具
go build -o bin/cadenza ./cmd/server
go build -o bin/cadenza-passwd ./cmd/cadenza-passwd

# 安装浏览器与系统依赖（需要可写 apt/网络；GitHub runner 可执行）
cd web && npx playwright install --with-deps chromium

# 运行（自动以 登录保护+演示库 启动被测实例，端口 18770）
cd web && npm run test:e2e
```

- 配置：`web/e2e/playwright.config.ts`（webServer 启动 `bin/cadenza`，simple 鉴权 + DEMO_MODE）。
- 用例：`web/e2e/specs/v1-ga.spec.ts`（5 条主链路）。
- **本地限制**：无系统库（`libnspr4` 等）且 apt 只读的环境无法启动浏览器，请在有系统库的环境或 CI 中运行
  （CI：`.github/workflows/ci.yml` 的 `ui-e2e` job 自动 `install --with-deps`）。
- 首跑若因 antd 文案/结构微调出现选择器差异，请以实际 DOM 为准修正（trace 已开 `retain-on-failure`）。
