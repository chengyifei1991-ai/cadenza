# OpAMP 统一管控后端 — 端到端测试计划

> 日期：2026-08-16 ｜ 环境：WSL2 + Rancher Desktop（containerd 运行时）+ otelcol-contrib v0.156.0

## 环境拓扑

```
┌─ Windows 主机 ─────────────────────────────────────────────┐
│  Rancher Desktop（containerd v2.3.2 + nerdctl）            │
│    └─ 容器 cadenza:0.1.0（服务端 :8080）             │
│       localhost:8080 转发（Windows 侧可达 ✅）             │
└──────────────┬─────────────────────────────────────────────┘
               │ 192.168.143.1:8080（WSL 虚拟网关转发，已验证 200）
┌──────────────▼─────────────────────────────────────────────┐
│ WSL2 发行版 unbuntu26.04（沙箱）                           │
│    └─ otelcol-contrib v0.156.0（systemd 运行中，:4317 等）  │
│       opamp extension → ws://192.168.143.1:8080/v1/opamp   │
└────────────────────────────────────────────────────────────┘
```

## 测试对象

| 对象 | 版本/标识 | 位置 |
|---|---|---|
| 服务端容器 | `cadenza:0.1.0` | Rancher Desktop containerd |
| Collector | otelcol-contrib **0.156.0** | 沙箱 `/usr/bin/otelcol-contrib` |
| 服务端访问地址（沙箱视角） | `http://192.168.143.1:8080` | Windows 转发 |

## 测试用例

### Phase 0：前置条件（已完成 ✅）

| # | 用例 | 预期 | 状态 |
|---|---|---|---|
| P0-1 | containerd 运行时可用（nerdctl version） | Server: containerd v2.x | ✅ |
| P0-2 | 服务端容器运行 | `nerdctl ps` Up + 日志"服务已启动" | ✅ |
| P0-3 | 容器内三入口自检（REST/MCP/OpAMP 认证） | 200/initialize 返回/401 认证 | ✅ |
| P0-4 | Windows localhost:8080 可达 | 200 | ✅（用户确认） |
| P0-5 | 沙箱→Windows 转发（192.168.143.1:8080） | 200 | ✅ |

### Phase 1：Collector 接入（opamp extension）

| # | 用例 | 步骤 | 预期 |
|---|---|---|---|
| P1-1 | collector 配置添加 opamp extension | 修改 `/etc/otelcol-contrib/config.yaml`，添加 `opamp` 扩展指向 `ws://192.168.143.1:8080/v1/opamp` + Bearer token + instance_uid | 配置合法（`otelcol-contrib validate` 通过） |
| P1-2 | 重启 collector | `systemctl restart otelcol-contrib` | 服务 active，无 opamp 连接错误 |
| P1-3 | 服务端看到 Collector | `GET /api/v1/collectors` | 返回该 collector（instance_uid/version=0.156.0/status=healthy） |

### Phase 2：状态上报

| # | 用例 | 预期 |
|---|---|---|
| P2-1 | effective config 上报 | collector 的 EffectiveConfig 非空（含 receivers/exporters） |
| P2-2 | hostname/version 提取 | Hostname=Thinkpad-cyf、Version=0.156.0 |
| P2-3 | last_seen 更新 | LastSeenAt 为最近时间 |

### Phase 3：配置下发（真链路）

| # | 用例 | 步骤 | 预期 |
|---|---|---|---|
| P3-1 | 审批拦截 | 服务端 REQUIRE_APPROVAL=true 时调 apply_config | 返回"审批已开启"错误 |
| P3-2 | 下发配置 | 重建容器 `REQUIRE_APPROVAL=false`，MCP apply_config 下发新配置（如修改 batch timeout） | 返回"已下发到 N 个 Collector" |
| P3-3 | collector 生效确认 | `GET /api/v1/collectors` 查 effective config | 内容变为新配置（批次配置变化） |
| P3-4 | 非法配置拒绝 | apply_config 传非法 YAML | 校验失败，返回错误，不下发 |

### Phase 4：MCP 工具端到端

| # | 用例 | 预期 |
|---|---|---|
| P4-1 | MCP initialize | serverInfo + tools capabilities |
| P4-2 | list_collectors 工具 | 返回集群状态（含本 collector） |
| P4-3 | get_collector_config | 返回 effective config |
| P4-4 | 审批流程（approve/reject） | 任务状态机流转（用 generate_config 创建的失败任务或直接创建任务验证） |
| P4-5 | list_pending_tasks | 列出待审批任务 |

### Phase 5：故障与边界

| # | 用例 | 预期 |
|---|---|---|
| P5-1 | LLM 故障隔离 | 占位 LLM key 时 generate_config 失败，但 list_collectors / REST 管控面正常 |
| P5-2 | 无凭证 OpAMP 连接拒绝 | 401 |
| P5-3 | 错误 token 拒绝 | 401 |
| P5-4 | 离线检测 | 停止 collector 后（>90s）状态变 offline |
| P5-5 | 审计留痕 | `/api/v1/audit` 有生成/下发/审批记录 |

## 环境变量基线（服务端容器）

```
LLM_API_KEY=sk-placeholder（测试故障隔离用；真实对话生成需替换）
OPAMP_AUTH_TOKEN=container-smoke-token
REQUIRE_APPROVAL=true（P3-2 重建时改为 false）
```

## 备注

- 对话生成配置（generate_config 成功路径）依赖真实 LLM key，本计划以故障隔离路径覆盖；真实 key 提供后补测成功路径。
- otelcol-contrib 深度校验在容器内未配置二进制，自动降级 yaml 校验（P3-4 用 yaml 级校验验证拒绝）。
