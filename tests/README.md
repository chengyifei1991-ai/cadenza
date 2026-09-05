# Cadenza E2E 回归套件

`tests/e2e.sh` 是**黑盒可复用**的端到端回归套件：启动真实单二进制（或指向外部实例），
基于演示库覆盖 HTTP 全链路，任何一次代码改动后都可重跑，作为 CI 门禁。

## 运行

```bash
# 本地（自动启动实例：simple 鉴权 + DEMO_MODE，端口 18762）
go build -o bin/cadenza ./cmd/server
go build -o bin/cadenza-passwd ./cmd/cadenza-passwd
./tests/e2e.sh

# 对已在运行的实例执行（跳过自启）
CADENZA_BASE_URL=http://127.0.0.1:8080 ./tests/e2e.sh

# 免登模式实例
E2E_AUTH=off ./tests/e2e.sh
```

依赖：`bash`、`curl`、`python3`（JSON 断言）；`./bin/cadenza` 与
`./bin/cadenza-passwd`（或外部 `CADENZA_BASE_URL`）。

## 环境变量

| 变量 | 默认 | 说明 |
|---|---|---|
| `CADENZA_BIN` | `./bin/cadenza` | 被测服务端二进制 |
| `PASSWD_BIN` | `./bin/cadenza-passwd` | bcrypt 哈希生成工具 |
| `CADENZA_BASE_URL` | 空 | 非空时跳过自启，直接对既有实例断言 |
| `E2E_AUTH` | `simple` | `simple`（登录保护）或 `off`（免登） |
| `E2E_ADMIN_USER` / `E2E_ADMIN_PASS` | `admin` / `Admin@12345` | 登录凭据 |
| `E2E_PORT` | `18762` | 自启实例端口 |
| `E2E_CORS_ORIGIN` | `http://allowed.example:5173` | 白名单 Origin（自启时注入） |

失败不中断：一次运行可看到全部问题；末尾汇总，任一失败退出码为 1（可作门禁）。

## 用例设计（测试视角）

套件按**覆盖矩阵**组织 section（新增用例就近追加）：

| Section | 视角 | 代表性断言 |
|---|---|---|
| 0-2 | 公开端点 / 认证正向与**错误与边界**（错口令 401、空凭据 400、非法 JSON 400、登出失效） | 状态码 + 错误文案 |
| 3 | 演示数据完整性（数量基线：5 Collector / 1 会话 / 4 审计 / 1 待审批） | JSON 字段 |
| 4 | Collector 列表/详情/版本（存在与不存在、404） | 详情字段 |
| 5 | 会话与对话（空消息 400、未知会话 404） | 状态码 |
| 6-10 | **状态机闭环**：apply 校验（缺字段 400 / 未知目标 404 / 非法 YAML 400 / 合法 201）→ 审批（approver 绑定登录用户）→ 回滚（版本数 +1、不存在的版本 400）→ 拒绝（done 拒绝被状态机拦截、reason 回读） | 任务状态字段 + 计数 |
| 7 | **分页/参数边界**（page=0、page_size=0/101、非数字 → 400） | 状态码 |
| 11 | 审计留痕递增 | 计数 |
| 12 | 静态资源与 SPA（根/前端路由回退 200；**缺失资源带扩展名 → 404**，防止误回退） | HTML/状态码 |
| 13 | 未知 API JSON 404 与 **CORS**（未授权 Origin 无头、授权预检 204、回写 ACAO） | 响应头 |
| 14 | 登出后会话失效 | 状态码 |

新增用例三步：起名 `check_xxx` → 用 `req` + `check_code`/`check_contains`/
`check_json_eq` 断言 → 本地跑通后按 CI 同样跑 `E2E_AUTH=simple|off` 两遍。

## 回归历史

套件上线首跑即发现 3 个缺陷（均已修复并各自附带单测回归）：

1. **登录空凭据返回 401**（应为 400 参数错误）→ `internal/api/auth.go`
2. **审批/拒绝不存在的任务返回 400 且泄漏英文内部错误**
   `store: record not found`（应 404 中文）→ `internal/api/handlers.go`
3. **缺失静态资源被 SPA fallback 吞掉返回 200 HTML**（浏览器 MIME 错误）
   → `internal/api/static.go`：末段带扩展名且未命中 → 404

## CI

`.github/workflows/ci.yml` 的 `e2e` job 在构建（含前端 embed）后以
`E2E_AUTH=simple` 与 `E2E_AUTH=off` 各跑一遍，作为合并门禁。
