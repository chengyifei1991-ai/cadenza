# GitOps 可选模式设计（配置版本由 git 托管）

> 状态：**已交付**（G-a ~ G-d 全部落地并通过门禁）｜ 日期：2026-09-29
>
> 交付摘要：`internal/gitsource` 只读封装 + `CONFIG_SOURCE=git` fail-closed + `git/status|commits|file`
> + apply/rollback 的 git 溯源（tasks 三列 + 审计） + MCP `get_git_config` + 前端「Git 历史」抽屉
> （按提交下发/回退/查看） + 任务详情 git 溯源展示；
> 门禁：单测全绿、`tests/gitops-e2e.sh` 18/18（已入 CI）、`tests/gitops-real-gate.sh` 12/12（真机）。
> 关联：[design-1.1.0.md](./design-1.1.0.md)、[review-1.1.0-final.md](./review-1.1.0-final.md)、[product-plan.md](./product-plan.md)
> 背景：用户提出"版本回退与记录不必都做进软件，应交给 git；软件只记录回退/升级这类动作数据"。
> 决策：**不删除现有内置能力**，而是新增 `CONFIG_SOURCE=git` 这一**可选模式**（默认仍是内置模式，行为与 1.0/1.1 完全一致）。

---

## 1. 目标与非目标

**目标**
- 提供可选 GitOps 模式：配置的**版本权威在 git 仓库**，Cadenza 负责"读取 → 校验 → 审批 → 下发 → 生效确认 → 审计"，并在任务/审计上记录 git 溯源（commit sha、文件路径、作者、提交信息）。
- 该模式下"回退"= 选择历史 commit → 重新下发该 commit 的配置内容（Cadenza 不拥有版本历史，只执行下发动作）。
- 内置模式（默认）不受影响：既有版本历史 Tab、回滚按钮、`/tasks/rollback`、版本快照表全部保留。

**非目标**
- 不做 git 仓库的写入/提交（Cadenza 只读，不动用户的仓库）。
- 不做 MR/PR 流程、不做多仓库聚合、不做分支策略管理。
- 不在本阶段引入远程仓库凭据管理（见 §5 决策 D2）。

## 2. 模式与配置项

| 配置 | 默认 | 说明 |
|---|---|---|
| `CONFIG_SOURCE` | `builtin` | `builtin`（内置版本快照 + 回滚，现状）或 `git`（GitOps 可选模式） |
| `GIT_REPO_DIR` | 空 | GitOps 模式下必填：本地 git 仓库绝对路径（只读使用） |
| `GIT_CONFIG_PATHSPEC` | 空 | 仓库内配置文件路径/glob（如 `collectors/*.yaml`）；用于按 Collector 匹配配置 |
| `GIT_REF` | `HEAD` | 默认读取的 ref（分支/tag/commit）；也可由请求指定 |

启动校验：`CONFIG_SOURCE=git` 时 `GIT_REPO_DIR` 必须存在且是 git 仓库，否则**fail-closed**（拒绝启动并给出中文原因），避免"以为在看 git，实际在用内置"。

## 3. 核心设计

### 3.1 git 访问（零新依赖）
- 调用系统 `git` CLI（与现有对 `otelcol-contrib` 的做法一致），不引入 Go git 库。
- 只读命令：`git -C <dir> rev-parse --verify <ref>`、`git -C <dir> log --format=...`、`git -C <dir> show <ref>:<path>`。
- 安全约束：`pathspec` 与 `ref` 做白名单校验（禁止 `..`、绝对路径、`:` 注入）；命令以 `exec.CommandContext` 调用并设超时。
- 结果做**短 TTL 缓存**（默认 10s，可配），避免每次页面轮询都打 git。

### 3.2 数据模型（幂等迁移）
`tasks` 表新增三列（内置模式留空，互不影响）：

| 列 | 说明 |
|---|---|
| `git_commit` | 该任务下发的 commit sha（GitOps 模式） |
| `git_path` | 配置在仓库中的文件路径 |
| `git_ref` | 请求/配置解析到的 ref（分支或 tag，便于回溯来源） |

审计 `detail` 追加 `commit=<sha> path=<p>`；`AuditAction` 复用 `apply/rollback/upgrade`，不新增枚举（回退在 GitOps 下就是一次 apply，来源是历史 commit）。

### 3.3 流水线（GitOps 模式）

```
读取文件(ref) → 两级校验(yaml + otelcol) → 记录 commit/path/ref
   → 任务(awaiting_approval) → 审批 → 下发(OpAMP) → 生效确认(1.0.x 语义)
   → 写审计(含 git 溯源) + 写内置版本快照("下发记录"，仍保留)
```

**回退**：`POST /api/v1/tasks/rollback` 在 GitOps 模式下语义改为"按 commit 回退"——请求带 `git_commit`（或复用 `version_id` 映射到该版本的 commit），Cadenza 读该 commit 的文件内容作为待下发配置。**仍然写一条内置版本快照**作为下发记录（保证 UI 历史与审计一致），但**版本权威仍是 git**（UI 明示"来源：git <sha>"）。

### 3.4 REST / MCP 增量（全部为新增，旧调用不受影响）

| 端点/工具 | 说明 |
|---|---|
| `GET /api/v1/git/status` | 仓库状态：当前 ref、HEAD sha、最近提交时间、是否干净（只读） |
| `GET /api/v1/git/commits?path=&limit=` | 该配置文件的历史提交（sha/作者/时间/标题） |
| `GET /api/v1/git/file?ref=&path=` | 指定 ref 的文件内容（配置编辑器"从 git 载入"） |
| `POST /api/v1/tasks/apply` | 请求体新增可选 `git_ref`（指定要下发的 ref；缺省用 `GIT_REF`） |
| `POST /api/v1/tasks/rollback` | GitOps 模式下新增可选 `git_commit`（按 commit 回退） |
| `GET /api/v1/system/info` | 新增 `config_source`（`builtin`/`git`）与 `git_enabled` |
| MCP `get_git_config`（新工具） | 读取当前 ref 的配置内容（10 → 11 个工具，README 同步） |

### 3.5 前端
- 设置/关于页显示"配置来源：内置 / git"。
- GitOps 模式下，Collector 详情「版本历史」Tab 增加一列 **Git 提交**（sha 短号 + 提交标题，悬浮看全文），并提供「从 git 载入配置」按钮进入编辑器。
- 编辑器的"保存并下发"在 GitOps 模式下会显示将要下发的 **ref/commit**，避免"以为在下发工作区"。
- 内置模式 UI 不变。

## 4. 门禁与验收（沿用"缺陷固化为门禁"）

| 层 | 用例 |
|---|---|
| 单测 | git 封装（临时仓库 fixture：log/show/rev-parse、pathspec 白名单、超时与错误映射）；任务 git 字段读写与迁移；`CONFIG_SOURCE=git` 缺 `GIT_REPO_DIR` 时 fail-closed |
| 黑盒 e2e | 新增一节：以临时 git 仓库启动 git 模式实例 → `git/status`、`git/commits`、`git/file` → 提交带 `git_ref` 的 apply → 审批 → 任务/审计含 commit；`rollback` 按 commit 回退 |
| 真机门禁 | 复用现有脚本，加一个"git 模式"变体：真实 collector 收到 git 来源的配置并真实生效（端口迁移），回退到历史 commit 后再次生效 |
| 兼容 | 内置模式全部既有断言必须不变（默认路径零回归） |

## 5. 待确认决策（评审点）

- **D1 仓库接入方式**：本地路径（`GIT_REPO_DIR`，零凭据、易验证）**vs** 远程 URL + token（更贴近生产，但需要凭据管理）。建议先做本地路径，远程作为后续项。
- **D2 凭据与安全**：本阶段不引入远程凭据；只读、无写回；`pathspec/ref` 白名单 + 超时。
- **D3 GitOps 模式下是否仍写内置版本快照**：建议**仍写**（作为"下发记录"与审计一致），但在 UI 标注"来源：git"，版本权威仍在 git。
- **D4 回退语义**：`rollback` 在 git 模式下按 commit 回退（读历史 commit 的文件内容重新下发），而不是复用内置版本 id；两者可共存（有 git_commit 用 git，否则走内置版本）。

## 6. 里程碑拆分（可独立交付）

| 步 | 内容 | 门禁 |
|---|---|---|
| G-a | git 只读封装 + 配置项 + fail-closed + 单测 | go test |
| G-b | 任务/审计 git 溯源字段 + apply 的 `git_ref` + `/git/*` 端点 | 单测 + e2e 新节 |
| G-c | GitOps 回退（按 commit）+ 前端 Git 历史列/载入按钮 | e2e + 前端用例 |
| G-d | 真机门禁 git 变体（真实生效 + 按 commit 回退） | 真机门禁 |
