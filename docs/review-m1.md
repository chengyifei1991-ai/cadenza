# M1 里程碑评审记录（Web 控制台骨架）

> 状态：评审完成，结论=**有条件通过（无阻断项，建议修复 1 中 + 2 低后再进 M2）**
> 日期：2026-09-05 ｜ 评审对象：提交 `d1bd102`（M1）及其配套 `0dc45ce`（E2E+修复）
> 关联：[product-plan.md](./product-plan.md) §6、[web-frontend-prd.md](./web-frontend-prd.md) §10

## 0. 评审方法

- 验收对照：按 product-plan M1 里程碑与 PRD §5.1-5.4/§3 契约清单逐项核对。
- 自动门禁：gofmt / go vet / go test（8 包）/ web `tsc` / E2E（simple 86、off 75）。
- 静态走查：前端 1029 行（17 个文件）逐文件代码走查 + 与后端 JSON 契约对齐核对。
- 已知边界：E2E 覆盖 API 与静态层；**React 运行时交互无自动化**（M4 Playwright 计划内），
  故 UI 行为结论基于静态走查 + 类型/构建保证，并已列入 §4 风险。

## 1. 验收对照表

| PRD 章节 / 里程碑信号 | 交付 | 证据 |
|---|---|---|
| §5.1 登录页（登录拦截、redirect、off 免登） | ✅ | E2E §1/§2/§14 + 后端 401 语义；LoginPage/RedirectIfAuthed |
| §5.2 主布局与导航（侧栏、鉴权守卫、版本/模式展示） | ✅ | AppShell + RequireAuth；设置页展示版本与模式 |
| §5.3 Dashboard 骨架（M1 挂 stats、待审批入口、空态引导） | ✅ | /stats 卡片 + 演示模式横幅（M4 图表另行排期） |
| §5.4 Collectors 列表（分页/状态筛选/搜索/状态映射/详情入口） | ✅ | 服务端分页 + 客户端筛选 + 30s 轮询 + 100 行截断提示 |
| §3 契约打通（collectors/versions/tasks/stats/sessions/auth） | ✅ | 类型化 client；新增 `GET /api/v1/collectors/{uid}` 详情端点（原 PRD 标 ⬜，M1 已提前落实，本记录同步修订） |
| M1 信号：真实 Collector 在线/离线/健康可见 | ✅ | E2E §4 + 演示数据；StatusTag 映射唯一源 |
| M1 信号：登录拦截生效 | ✅ | E2E §1/§14 |

## 2. 质量门禁结果（全部通过）

gofmt clean ｜ go vet clean ｜ go test 8/8 ok ｜ web tsc ok ｜ E2E simple 86/86、off 75/75 ｜ git 与 origin/dev 同步（0 ahead）。

## 3. 代码走查发现（按严重度）

### [中] F-M1-1 鉴权引导在网络故障时 fail-open 且误标"免登模式"
- 位置：`web/src/app/auth.tsx`（AuthProvider `.catch` 分支）。
- 现象：bootstrap 阶段 `/auth/me` 抛非 401 错误（后端未就绪/网络抖动）时，代码把
  `mode` 置为 `off`、`user` 置为 `user` → UI 直接进入、无登录墙、顶栏显示"免登模式"，
  而真实后端实为 `simple`（后续请求全 401，各页报错但无登录引导）。属**误导性降级**。
- 建议：bootstrap 仅当 `/auth/me` 明确返回 401 才判定"未登录(simple)"；网络/5xx 错误应进入
  AuthGate 的"加载失败+重试"态（可参考 `/system/info` 判定模式），绝不 silent 放行为 off。

### [低] F-M1-2 off 模式下仍展示"退出登录"
- 位置：`AppShell` 用户下拉、`SettingsPage`。
- 建议：`mode === "off"` 时隐藏退出项（或改为"仅登录保护模式可退出"提示）。

### [低] F-M1-3 Sider 窄屏收起后无法展开
- 位置：`AppShell` Sider（`breakpoint="lg" collapsedWidth={0}`，未设 collapsible）。
- 建议：加 `collapsible` 与 zero-width trigger（桌面优先目标下为 P2，可在 M2 布局打磨一并处理）。

### [低] F-M1-4 已知功能简化点（规划内，非缺陷）
- Collector 列表搜索/状态筛为客户端过滤当前页（前 100 条截断有提示）；详情页版本历史仅速览前
  10 条无翻页。两者后端化/翻页按规划归 M2（任务/审计/分组面）与后续版本，评审记录不阻塞。

## 4. 测试覆盖缺口（建议排期）

| 缺口 | 现状 | 建议 |
|---|---|---|
| 前端单测为零 | 仅 tsc + 构建 | M2 引入 Vitest，优先覆盖纯逻辑：status 映射、time 工具、ApiError 契约解析、Auth reducer |
| React 交互无自动化 | E2E 只到 API/静态层 | Playwright 冒烟按 PRD 仍排 M4；若 M2 交互复杂度上升可提前到 M2 末 |
| 包体积 | 单 chunk 721KB（gzip 231KB） | M2 引入路由级 `React.lazy` + manualChunks 拆 antd/echarts 等 |

## 5. 评审结论

- **无阻断/高危问题**：鉴权后端语义、审批绑定、演示链路、静态服务与契约均经自动化验证。
- **建议处理 3 项**（1 中 + 2 低）后进入 M2：F-M1-1（推荐修复，涉及登录体验与模式展示可信度）、
  F-M1-2/F-M1-3（低成本顺手修复）。
- 前端单测与拆包建议排入 M2 起步项（非 M1 阻塞）。

## 6. 本记录对规划文档的同步修订

- `web-frontend-prd.md §3`：`GET /api/v1/collectors/{uid}` 状态由 ⬜（M2 评估）改为 ✅（M1 已实现）。
