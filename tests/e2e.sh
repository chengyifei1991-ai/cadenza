#!/usr/bin/env bash
# Cadenza 可复用 E2E 回归套件（黑盒：真实二进制 + 演示库，覆盖 HTTP 全链路）。
#
# 用法：
#   ./tests/e2e.sh                          # 自动启动 ./bin/cadenza（simple 鉴权 + demo）
#   CADENZA_BASE_URL=http://x:8080 ./tests/e2e.sh   # 对已在运行的实例执行（跳过自启）
#   E2E_AUTH=off ./tests/e2e.sh             # 免登模式实例
#   CADENZA_BIN=/path/to/cadenza ./tests/e2e.sh
#
# 前置：./bin/cadenza 与 ./bin/cadenza-passwd（go build -o bin/... ./cmd/...）；curl、python3。
# 约定：失败不中断（便于一次看到全部问题），最后汇总；有任一失败即退出码 1（可作 CI 门禁）。
# 新增用例：在对应 section() 中追加 check_* 即可。JSON 断言：
#   check_json_eq 'EXPR' '期望值' '用例描述'，EXPR 形如 ['items'][0]['id']。
set -u

CADENZA_BIN=${CADENZA_BIN:-./bin/cadenza}
PASSWD_BIN=${PASSWD_BIN:-./bin/cadenza-passwd}
E2E_AUTH=${E2E_AUTH:-simple}
ADMIN_USER=${E2E_ADMIN_USER:-admin}
ADMIN_PASS=${E2E_ADMIN_PASS:-Admin@12345}
CORS_ORIGIN=${E2E_CORS_ORIGIN:-http://allowed.example:5173}
WORK="$(mktemp -d)"
SERVER_PID=""
BASE=""
PASSED=0
FAILED=0
declare -a FAILURES=()

cleanup() {
  if [ -n "$SERVER_PID" ]; then kill "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null; fi
  rm -rf "$WORK"
}
trap cleanup EXIT

log() { printf '%s\n' "$*"; }
pass() { PASSED=$((PASSED + 1)); }
fail() {
  FAILED=$((FAILED + 1))
  FAILURES+=("$1")
  log "  ✗ $1"
}

BODY="$WORK/body"
CJ="$WORK/jar.txt"

start_server() {
  if [ -n "${CADENZA_BASE_URL:-}" ]; then
    BASE="${CADENZA_BASE_URL%/}"
    log "== 使用外部实例: $BASE（E2E_AUTH=$E2E_AUTH）"
    return 0
  fi
  if [ ! -x "$CADENZA_BIN" ]; then
    log "错误: 找不到 $CADENZA_BIN；请先 go build -o bin/cadenza ./cmd/server，或设置 CADENZA_BASE_URL"
    exit 2
  fi
  if [ ! -x "$PASSWD_BIN" ]; then
    log "错误: 找不到 $PASSWD_BIN；请先 go build -o bin/cadenza-passwd ./cmd/cadenza-passwd"
    exit 2
  fi
  PORT=${E2E_PORT:-18762}
  BASE="http://127.0.0.1:$PORT"
  hash=$(printf '%s\n' "$ADMIN_PASS" | "$PASSWD_BIN")
  local auth_env=()
  if [ "$E2E_AUTH" = "simple" ]; then
    auth_env=(WEB_AUTH_MODE=simple WEB_ADMIN_USER="$ADMIN_USER" WEB_ADMIN_PASSWORD_HASH="$hash")
  else
    auth_env=(WEB_AUTH_MODE=off)
  fi
  env DB_DRIVER=sqlite DB_SQLITE_PATH="$WORK/e2e.db" HTTP_ADDR="127.0.0.1:$PORT" \
    DEMO_MODE=true REQUIRE_APPROVAL=true LLM_API_KEY=e2e-not-used \
    CORS_ALLOWED_ORIGINS="$CORS_ORIGIN" "${auth_env[@]}" \
    "$CADENZA_BIN" >"$WORK/server.log" 2>&1 &
  SERVER_PID=$!
  for _ in $(seq 1 80); do
    if curl -sf "$BASE/healthz" >/dev/null 2>&1; then return 0; fi
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
      log "错误: 服务器启动失败（日志见 $WORK/server.log）"
      tail -20 "$WORK/server.log"
      exit 2
    fi
    sleep 0.25
  done
  log "错误: 服务器启动超时"; tail -20 "$WORK/server.log"; exit 2
}

# ---- HTTP 原语 ----
HTTP_CODE=""
req() { # req METHOD PATH [--data JSON] [--cookie FILE] [--origin ORIGIN]
  local method=$1 path=$2; shift 2
  local curl_args=(-s -o "$BODY" -w '%{http_code}' -X "$method" "$BASE$path")
  while [ $# -gt 0 ]; do
    case "$1" in
      --data) curl_args+=(-H 'Content-Type: application/json' -d "$2"); shift 2 ;;
      --cookie) curl_args+=(-b "$2"); shift 2 ;;
      --cookie-jar) curl_args+=(-c "$2"); shift 2 ;;
      --origin) curl_args+=(-H "Origin: $2"); shift 2 ;;
      *) curl_args+=("$1"); shift ;;
    esac
  done
  HTTP_CODE=$(curl "${curl_args[@]}")
}

jget() { # jget 'EXPR'：从 $BODY 取 JSON 值
  python3 -c "
import sys, json
try:
    d = json.load(open('$BODY'))
    v = d$1
    print(v if not isinstance(v, (dict, list)) else json.dumps(v, ensure_ascii=False))
except Exception:
    print('')
"
}

check_code() { # check_code '用例描述' '期望 HTTP 码'
  local desc=$1 want=$2
  if [ "$HTTP_CODE" = "$want" ]; then pass; else
    fail "$desc (HTTP $HTTP_CODE, want $want; body: $(head -c 200 "$BODY" | tr '\n' ' '))"
  fi
}
check_contains() { # check_contains '用例描述' '期望子串'
  if grep -q -- "$2" "$BODY"; then pass; else
    fail "$1 (body 缺少 \"$2\": $(head -c 200 "$BODY" | tr '\n' ' '))"
  fi
}
check_json_eq() { # check_json_eq 'EXPR' '期望值' '用例描述'
  local got; got=$(jget "$1")
  if [ "$got" = "$2" ]; then pass; else
    fail "$3 (jget $1 = \"$got\", want \"$2\")"
  fi
}

section() { log ""; log "===== $1 ====="; }

# ============ 0. 冒烟启动 ============
start_server

section "0. 基础端点（公开）"
req GET /healthz
check_code "healthz 200" 200
check_contains "healthz 含 ok" '"status":"ok"'
req GET /api/v1/system/info
check_code "system/info 200（公开）" 200
check_json_eq "['demo_mode']" "True" "system/info demo_mode=true"
if [ "$E2E_AUTH" = "simple" ]; then
  check_contains "system/info 标记 simple" '"auth_mode":"simple"'
else
  check_contains "system/info 标记 off" '"auth_mode":"off"'
fi
check_contains "system/info 含版本" '"version":'

section "1. 鉴权"
if [ "$E2E_AUTH" = "simple" ]; then
  req GET /api/v1/collectors
  check_code "未登录访问受保护端点 → 401" 401
  req GET /api/v1/auth/me
  check_code "me 未登录 → 401" 401
  req POST /api/v1/auth/login --data '{"username":"admin","password":"wrong"}'
  check_code "错误口令 → 401" 401
  req POST /api/v1/auth/login --data "{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}" --cookie-jar "$CJ"
  check_code "正确登录 → 200" 200
  check_json_eq "['username']" "$ADMIN_USER" "登录返回用户名"
  req GET /api/v1/auth/me --cookie "$CJ"
  check_code "me 已登录 → 200" 200
  check_json_eq "['username']" "$ADMIN_USER" "me 返回用户名"
  check_json_eq "['auth_mode']" "simple" "me 返回 auth_mode"
else
  req GET /api/v1/collectors
  check_code "免登模式可直接访问 → 200" 200
  req GET /api/v1/auth/me
  check_code "免登模式 me → 200" 200
  check_json_eq "['username']" "user" "免登模式 me 返回 user"
fi

section "2. 登录健壮性（边界）"
if [ "$E2E_AUTH" = "simple" ]; then
  req POST /api/v1/auth/login --data '{}'
  check_code "空凭据 → 400" 400
  req POST /api/v1/auth/login --data 'not-json'
  check_code "非法 JSON → 400" 400
  req POST /api/v1/auth/login --data '{"username":"admin","password":"Admin@12345"}' --cookie-jar "$CJ"
  check_code "重登（登录后可访问）" 200
fi

section "3. 演示数据完整性"
req GET "/api/v1/collectors?page=1&page_size=10" --cookie "$CJ"
check_code "collectors 分页 → 200" 200
check_json_eq "['total']" "5" "演示库含 5 个 Collector"
req GET /api/v1/stats --cookie "$CJ"
check_code "stats → 200" 200
check_json_eq "['collectors']['healthy']" "2" "stats healthy=2"
check_json_eq "['collectors']['offline']" "1" "stats offline=1"
check_json_eq "['tasks']['awaiting_approval']" "1" "stats 待审批=1"
check_json_eq "['sessions_total']" "1" "stats 会话=1"
req GET "/api/v1/tasks?status=awaiting_approval&page=1&page_size=10" --cookie "$CJ"
check_json_eq "['total']" "1" "待审批任务=1"
req GET "/api/v1/audit?page=1&page_size=100" --cookie "$CJ"
check_code "audit → 200" 200
check_json_eq "['total']" "4" "演示审计=4 条"

section "4. Collector 列表/详情"
req GET "/api/v1/collectors/demo-gateway-1" --cookie "$CJ"
check_code "Collector 详情 → 200" 200
check_json_eq "['status']" "healthy" "gateway 状态 healthy"
check_json_eq "['hostname']" "demo-gateway-1" "gateway hostname"
req GET "/api/v1/collectors/demo-gateway-1/versions?page=1&page_size=10" --cookie "$CJ"
check_code "版本历史 → 200" 200
check_json_eq "['total']" "2" "演示版本=2"
req GET "/api/v1/collectors/no-such-uid" --cookie "$CJ"
check_code "不存在的 Collector → 404" 404

section "5. 会话与对话"
req GET "/api/v1/sessions?page=1&page_size=10" --cookie "$CJ"
check_code "sessions 列表 → 200" 200
check_json_eq "['total']" "1" "演示会话=1"
check_contains "会话摘要含首条消息" "增加内存限制"
SID=$(jget "['items'][0]['id']")
req GET "/api/v1/sessions/$SID" --cookie "$CJ"
check_code "sessions 详情 → 200" 200
check_json_eq "['id']" "$SID" "会话详情 id 一致"
req POST /api/v1/sessions --cookie "$CJ" --data '{}'
check_code "创建会话 → 200" 200
NEW_SID=$(jget "['session_id']")
if [ -n "$NEW_SID" ]; then pass; else fail "创建会话应返回 session_id"; fi
req POST /api/v1/chat --cookie "$CJ" --data "{\"session_id\":\"$NEW_SID\",\"message\":\"\"}"
check_code "空消息 → 400" 400
req POST /api/v1/chat --cookie "$CJ" --data '{"session_id":"no-such","message":"hi"}'
check_code "不存在的会话 → 404" 404
req GET "/api/v1/sessions/no-such" --cookie "$CJ"
check_code "会话详情不存在 → 404" 404

section "6. apply 配置（编辑器保存下发）"
req POST /api/v1/tasks/apply --cookie "$CJ" --data '{"collector_instance_uid":"demo-gateway-1"}'
check_code "apply 缺 yaml → 400" 400
req POST /api/v1/tasks/apply --cookie "$CJ" --data '{"collector_instance_uid":"nope","yaml":"receivers: {}"}'
check_code "apply 未知 Collector → 404" 404
req POST /api/v1/tasks/apply --cookie "$CJ" --data '{"collector_instance_uid":"demo-gateway-1","yaml":"foo: bar"}'
check_code "apply 非法 YAML → 400" 400
check_contains "非法 YAML 含校验文案" "校验"
VALID_YAML='receivers:
  otlp:
    protocols:
      grpc:
exporters:
  debug:
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [debug]
'
apply_payload() {
  python3 - "$1" "$2" <<'PY'
import json, sys
body = {"collector_instance_uid": sys.argv[1], "yaml": sys.argv[2], "note": "e2e 提交"}
print(json.dumps(body))
PY
}
req POST /api/v1/tasks/apply --cookie "$CJ" --data "$(apply_payload demo-gateway-1 "$VALID_YAML")"
check_code "apply 合法 YAML → 201" 201
check_json_eq "['type']" "apply" "apply 任务 type=apply"
check_json_eq "['status']" "awaiting_approval" "apply 任务进入待审批"
APPLY_ID=$(jget "['id']")

section "7. 任务分页与筛选（边界）"
req GET "/api/v1/tasks?page=0" --cookie "$CJ"
check_code "page=0 → 400" 400
req GET "/api/v1/tasks?page_size=0" --cookie "$CJ"
check_code "page_size=0 → 400" 400
req GET "/api/v1/tasks?page_size=101" --cookie "$CJ"
check_code "page_size=101 → 400" 400
req GET "/api/v1/tasks?status=done&page=1&page_size=10" --cookie "$CJ"
check_json_eq "['total']" "1" "done 任务=1"
req GET "/api/v1/collectors?page=abc" --cookie "$CJ"
check_code "collectors page=abc → 400" 400

section "8. 审批流（approver 绑定登录用户）"
req POST "/api/v1/tasks/no-such/approve" --cookie "$CJ" --data '{}'
check_code "审批不存在任务 → 404" 404
req POST "/api/v1/tasks/$APPLY_ID/approve" --cookie "$CJ" --data '{}'
check_code "审批 apply 任务 → 200" 200
req GET "/api/v1/tasks/$APPLY_ID" --cookie "$CJ"
check_code "任务详情 → 200" 200
check_json_eq "['status']" "done" "任务审批后 done"
if [ "$E2E_AUTH" = "simple" ]; then
  check_json_eq "['approver']" "$ADMIN_USER" "审批人绑定登录用户（非 user）"
fi

section "9. 回滚闭环"
VER_ID=$(curl -s -b "$CJ" "$BASE/api/v1/collectors/demo-gateway-1/versions?page=1&page_size=1" | python3 -c "import sys,json;print(json.load(sys.stdin)['items'][0]['id'])")
req POST /api/v1/tasks/rollback --cookie "$CJ" --data "{\"collector_instance_uid\":\"demo-gateway-1\",\"version_id\":$VER_ID}"
check_code "创建回滚任务 → 201" 201
RB_ID=$(jget "['id']")
check_json_eq "['type']" "rollback" "回滚任务 type=rollback"
req POST "/api/v1/tasks/$RB_ID/approve" --cookie "$CJ" --data '{}'
check_code "审批回滚任务 → 200" 200
req GET "/api/v1/tasks/$RB_ID" --cookie "$CJ"
check_json_eq "['status']" "done" "回滚任务 done"
V_TOTAL=$(curl -s -b "$CJ" "$BASE/api/v1/collectors/demo-gateway-1/versions?page=1&page_size=100" | python3 -c "import sys,json;print(json.load(sys.stdin)['total'])")
# 期望：2（演示基线）+ apply 审批 +1 + 回滚审批 +1 = 4
if [ "$V_TOTAL" = "4" ]; then pass; else fail "版本数应为 4，实际 $V_TOTAL"; fi
req POST /api/v1/tasks/rollback --cookie "$CJ" --data '{"collector_instance_uid":"demo-gateway-1","version_id":99999}'
check_code "回滚不存在版本 → 400" 400

section "10. 拒绝流"
req POST "/api/v1/tasks/$APPLY_ID/reject" --cookie "$CJ" --data '{"reason":"已拒绝"}'
check_code "对 done 任务拒绝 → 400（状态机保护）" 400
req POST /api/v1/tasks/apply --cookie "$CJ" --data "$(apply_payload demo-gateway-1 "receivers: {}
exporters:
  debug:
service:
  pipelines:
    traces:
      receivers: []
      exporters: []
")"
check_code "再提交 apply → 201" 201
RJT_ID=$(jget "['id']")
req POST "/api/v1/tasks/$RJT_ID/reject" --cookie "$CJ" --data '{"reason":"e2e 拒绝"}'
check_code "拒绝待审批任务 → 200" 200
req GET "/api/v1/tasks/$RJT_ID" --cookie "$CJ"
check_json_eq "['status']" "rejected" "任务状态 rejected"
check_json_eq "['reject_reason']" "e2e 拒绝" "拒绝原因回读"

section "11. 审计留痕"
req GET "/api/v1/audit?page=1&page_size=100" --cookie "$CJ"
check_code "audit 查询 → 200" 200
A_TOTAL=$(jget "['total']")
if [ "$A_TOTAL" -ge 10 ]; then pass; else fail "审批/回滚/拒绝应追加审计（当前 $A_TOTAL）"; fi

section "12. Web 静态资源与 SPA"
req GET /
check_code "根路径 → 200" 200
check_contains "根路径为 HTML" "<!doctype html>"
check_contains "页面含 Cadenza 标识" "Cadenza"
req GET /settings
check_code "SPA 路由 /settings → 200（fallback）" 200
req GET /not-a-real-page
check_code "未知前端路由 → 200（fallback）" 200
req GET /assets/__definitely_missing__.js
check_code "缺失静态资源 → 404（不误回退 index）" 404

section "13. 未知 API 与 CORS"
req GET /api/v1/definitely-not-exists --cookie "$CJ"
check_code "未知 API → 404" 404
check_contains "未知 API 返回 JSON 错误" '"error"'
hdr() { curl -s -o /dev/null -D - "$@" | tr -d '\r' | grep -i '^access-control-allow-origin:' || true; }
ACAO=$(hdr -H "Origin: http://evil.example" "$BASE/api/v1/collectors")
if [ -z "$ACAO" ]; then pass; else fail "未授权 Origin 不应回写 CORS 头（$ACAO）"; fi
req OPTIONS /api/v1/collectors --origin "$CORS_ORIGIN"
check_code "授权 Origin 预检 → 204" 204
ACAO2=$(hdr -H "Origin: $CORS_ORIGIN" -b "$CJ" "$BASE/api/v1/collectors")
if [ -n "$ACAO2" ]; then pass; else fail "授权 Origin 应回写 CORS 头"; fi

section "14. 登出"
if [ "$E2E_AUTH" = "simple" ]; then
  req POST /api/v1/auth/logout --cookie "$CJ" --data '{}'
  check_code "登出 → 200" 200
  req GET /api/v1/collectors --cookie "$CJ"
  check_code "登出后 Cookie 失效 → 401" 401
fi

log ""
log "=============================================="
log "结果: $PASSED 通过 / $FAILED 失败"
if [ "$FAILED" -gt 0 ]; then
  log "失败用例:"
  for f in "${FAILURES[@]}"; do log "  - $f"; done
  exit 1
fi
log "全部通过 ✓"
