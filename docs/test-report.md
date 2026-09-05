# OpAMP 统一管控后端 — 端到端测试报告

> 日期：2026-08-16 ｜ 环境：WSL2 + Rancher Desktop（containerd v2.3.2）+ otelcol-contrib **v0.156.0** + opampsupervisor v0.156.0

## 一、测试结论

**全部 22 项测试用例通过（P0–P5）**，核心链路完整验证：

```
服务端（容器）下发配置 ──OpAMP WS──▶ opampsupervisor 接收并应用
  ──▶ 写 effective.yaml ──▶ 重启 Collector 子进程 ──▶ 新配置生效
  ──▶ 上报 effective config ──▶ 服务端可见（batch timeout: 500ms ✅）
```

## 二、测试结果明细

### Phase 0：环境就绪（5/5 ✅）

| # | 用例 | 结果 |
|---|---|---|
| P0-1 | containerd v2.3.2 + nerdctl v2.2.2 | ✅ |
| P0-2 | 服务端容器运行（host 网络） | ✅ |
| P0-3 | 容器内三入口自检 | ✅ REST/MCP/OpAMP 认证 |
| P0-4 | Windows localhost:8080 可达 | ✅ |
| P0-5 | 沙箱→Windows 转发 192.168.143.1:8080 | ✅（需 --noproxy，环境代理干扰） |

### Phase 1：Collector 接入（3/3 ✅）

| # | 用例 | 结果 |
|---|---|---|
| P1-1 | opamp extension 配置 + `otelcol-contrib validate` | ✅ |
| P1-2 | 重启 collector | ✅ |
| P1-3 | 服务端看到 Collector（uid/hostname/version=0.156.0） | ✅ |

### Phase 2：状态上报（3/3 ✅）

| # | 用例 | 结果 |
|---|---|---|
| P2-1 | effective config 全量上报 | ✅（7.6KB） |
| P2-2 | hostname/version 提取 | ✅ Thinkpad-cyf / 0.156.0 |
| P2-3 | last_seen 实时更新 | ✅ |

### Phase 3：配置下发（4/4 ✅）

| # | 用例 | 结果 |
|---|---|---|
| P3-1 | 审批拦截（REQUIRE_APPROVAL=true） | ✅ 返回"禁止直接下发" |
| P3-2 | 下发配置（batch timeout 200→500ms） | ✅ "已下发到 1 个 Collector" |
| P3-3 | Collector 生效确认 | ✅ **effective config 显示 timeout: 500ms** |
| P3-4 | 非法配置拒绝 | ✅ "YAML 语法错误" isError |

### Phase 4：MCP 端到端（5/5 ✅）

| # | 用例 | 结果 |
|---|---|---|
| P4-1 | initialize（serverInfo + tools capabilities） | ✅ |
| P4-2 | list_collectors | ✅ 返回 2 个 Collector |
| P4-3 | get_collector_config | ✅ 返回 effective config |
| P4-4 | 审批流程（upgrade→pending→approve→done） | ✅ 状态机完整流转 |
| P4-5 | list_pending_tasks | ✅ |

### Phase 5：故障与边界（5/5 ✅）

| # | 用例 | 结果 |
|---|---|---|
| P5-1 | LLM 故障隔离（占位 key） | ✅ generate 失败报错，管控面 200 不受影响 |
| P5-2 | 无凭证 OpAMP 连接 | ✅ 401 |
| P5-3 | 错误 token | ✅ 401 |
| P5-4 | 离线检测（停 90s+） | ✅ 两个 Collector 均标记 offline |
| P5-5 | 审计留痕 | ✅ 5 条记录（generate/apply 含配置哈希） |

## 三、测试中发现并解决的问题

### 1. RemoteConfig 缺 config_hash（已修复 ✅）
- **现象**：Collector 收到 RemoteConfig 后 effective config 变空、不应用
- **根因**：OpAMP 协议规定 `AgentRemoteConfig.config_hash` **MUST always be set**（服务器支持远程配置时），我们未设置
- **修复**：`internal/opampserver/server.go` 的 `remoteConfigOf()` 计算 sha256 填入 `ConfigHash`
- **验证**：修复后下发 → effective config 更新为 500ms

### 2. opampextension 不应用 RemoteConfig（架构认知，记录 ✅）
- **结论**：`opampextension v0.156.0` 的 `onMessage` 仅处理 AgentIdentification/CustomMessage，**不应用 RemoteConfig**（设计使然，该 extension 是为 Supervisor 设计）
- **正确架构**：`opampsupervisor`（独立发布：`cmd/opampsupervisor/v0.156.0` tag）作为 OpAMP 客户端接收配置 → 写 effective.yaml → 管理 Collector 子进程生命周期
- **已落地**：下载 supervisor 官方二进制（22MB），验证完整闭环

### 3. 环境代理干扰（规避 ✅）
- 沙箱 `http_proxy=192.168.3.25:7897` 不可达，导致 curl/python/supervisor 访问本机及局域网目标失败
- **规避**：curl `--noproxy '*'`、python `ProxyHandler({})`、supervisor `env -u http_proxy`

### 4. 遗留观察（非阻塞）
- Collector health 上报被判为 `unhealthy`：opampextension 上报 `ComponentHealth.Healthy=false` 时服务端标记 unhealthy（服务端判定策略待优化：health 未上报/未知时应显示 `unknown` 而非 unhealthy）
- 端口映射仅 Windows localhost 可达：Rancher Desktop 的转发绑定 Windows 侧；WSL 内访问需经 192.168.143.1（Windows 虚拟网关）

## 四、环境快照（可复现）

| 组件 | 版本/位置 |
|---|---|
| 服务端容器 | `cadenza:0.1.0`（containerd, host 网络, REQUIRE_APPROVAL=false） |
| Collector | `/usr/bin/otelcol-contrib` v0.156.0（systemd 已停，由 supervisor 管理） |
| Supervisor | `.build/opampsupervisor` v0.156.0 + `.build/supervisor.yaml` |
| 服务端地址 | `ws://192.168.143.1:8080/v1/opamp`（Windows 转发） |
| 认证 | `Authorization: Bearer container-smoke-token` |

## 五、后续建议

1. 服务端 health 判定策略优化（unknown vs unhealthy）
2. 生产部署：supervisor 用 systemd 管理 + storage 目录持久化
3. 真实 LLM key 接入后补测 generate_config 成功路径（P5-1 仅验证故障路径）
4. `upgrade_collector` 包分发（PackagesAvailable）需 Collector 侧能力配合
