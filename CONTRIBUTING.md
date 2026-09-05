# 贡献指南

感谢你对 Cadenza 的兴趣！本文档说明如何搭建开发环境、提交改动与协作。

## 一、开发环境

- Go ≥ 1.26（`go.mod` 声明 `go 1.26.6`）
- （可选）`otelcol-contrib` v0.156.0，用于启用深度配置校验（`STRICT_VALIDATE=true` 时）

```bash
git clone https://github.com/chengyifei1991-ai/cadenza.git
cd cadenza

go mod download          # 拉取依赖
go build ./...           # 编译
go test ./...            # 全量表驱动测试
go vet ./...             # 静态检查
gofmt -l cmd internal    # 检查格式（应为空输出）
```

## 二、提交规范（DCO 签章）

本项目采用 [Developer Certificate of Origin (DCO)](https://developercertificate.org/)，
无需签署 CLA。每个提交请带上签名：

```bash
git commit -s -m "feat: 新增 xxx"
```

`-s` 会在提交尾部追加 `Signed-off-by: 你的名字 <你的邮箱>`，表示你确认拥有该代码的贡献权。
提交信息建议遵循 [Conventional Commits](https://www.conventionalcommits.org/)：
`feat:` / `fix:` / `docs:` / `refactor:` / `test:` / `chore:`。

## 三、提交流程

1. **先开 Issue 讨论**（Bug 报告或功能提议），说明动机与方案。
2. Fork 仓库，从 `main` 切出特性分支（`feat/xxx` 或 `fix/xxx`）。
3. 编写实现与测试；涉及行为变更时补充/更新 `docs/` 下的设计与测试文档。
4. 本地跑通 `go test ./... && go vet ./...`。
5. 提交 Pull Request，填写模板；维护者会评审并可能要求修改。

## 四、代码风格

- 遵循 Go 官方风格，用 `gofmt` 与 `go vet` 把关。
- 新增/修改导出符号请写注释（`// FuncName ...`）。
- 每个源文件保留顶部 SPDX 头：
  ```go
  // SPDX-License-Identifier: Apache-2.0
  // Copyright 2026 chengyifei1991
  ```
- 存储/任务/校验等核心逻辑改动需补充表驱动测试（参考现有 `*_test.go`）。

## 五、目录速览

见 [README.md](./README.md) 的「目录结构」；设计决策见 [docs/](./docs/)。
