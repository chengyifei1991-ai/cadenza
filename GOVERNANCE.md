# 治理模型

Cadenza 采用轻量、低门槛的治理结构，目标是让贡献者容易参与、让决策可追溯。

## 角色

| 角色 | 职责 |
|---|---|
| **维护者（Maintainers）** | 合并 PR、发布版本、管理 Issue 标签、执行行为准则、最终裁决技术分歧 |
| **贡献者（Contributors）** | 提交代码/文档/测试，参与 Issue 讨论与代码评审 |
| **使用者（Users）** | 提出需求与 Bug，参与社区讨论 |

## 维护者

- [chengyifei](https://github.com/chengyifei1991)（GitHub 账号：chengyifei1991）

成为维护者的路径：持续贡献若干高质量 PR、获得现有维护者认可后邀请加入。

## 决策机制

- **懒惰共识（Lazy Consensus）**：普通变更（Bug 修复、文档、小功能）由维护者评审通过即可合并；若无反对，默认通过。
- **RFC（重大变更）**：涉及架构、API 破坏性变更、新增核心依赖或安全模型的改动，先在 Issue 中提交设计提案（可放 `docs/`），经过讨论与维护者多数同意后实施。
- **无法达成共识**：由维护者集体裁决；仍僵持时由最资深维护者拍板，并在 PR 中记录理由。

## 行为准则

所有参与者须遵守 [CODE_OF_CONDUCT.md](./CODE_OF_CONDUCT.md)。

## 贡献与 DCO

贡献流程见 [CONTRIBUTING.md](./CONTRIBUTING.md)；本项目采用 DCO 签章（`git commit -s`），不要求 CLA。

## 许可证

本项目自有代码以 [Apache License 2.0](./LICENSE) 授权；所链接第三方组件的许可见 [THIRD_PARTY_NOTICES.md](./THIRD_PARTY_NOTICES.md)。对本项目源代码的贡献默认按 Apache-2.0 授权（与项目保持一致）。
