#!/usr/bin/env bash
# Cadenza 真机门禁：真实 otelcol-contrib + supervisor fixture 的"下发生效"端到端验证。
#
# 验证的不是"服务端把配置发出去了"，而是**Collector 进程真正换上了新配置**：
#   1. 真实 collector 接入并上报 healthy / version / effective config；
#   2. 提交→审批→下发后，任务收到生效确认（error 为空）；
#   3. 子进程被重启、监听端口真实迁移（14321 → 14322）、effective.yaml 被重写；
#   4. 回滚把端口与配置切回历史版本，版本快照与审计留痕。
#
# 依赖：go、curl、python3、bash，以及 otelcol-contrib 二进制（缺失则 SKIP）。
#
# 用法：
#   ./tests/real-collector-gate.sh
#   COLLECTOR_BIN=/usr/bin/otelcol-contrib PORT=18765 ./tests/real-collector-gate.sh
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

COLLECTOR_BIN="${COLLECTOR_BIN:-/usr/bin/otelcol-contrib}"
PORT="${PORT:-18765}"
BASE="http://127.0.0.1:${PORT}"
GATE_TOKEN="${GATE_TOKEN:-gate-token-1}"
WORKDIR="${WORKDIR:-$REPO_ROOT/.build/gate-run}"
UID_DASHED="33333333-4444-5555-6666-777788889999"
UID_REST="${UID_DASHED//-/}"
PORT_A=14321
PORT_B=14322

PASS=0
FAIL=0
ok()   { PASS=$((PASS+1)); echo "  ✓ $1"; }
bad()  { FAIL=$((FAIL+1)); echo "  ✗ $1"; }

if [ ! -x "$COLLECTOR_BIN" ]; then
  echo "SKIP: 未找到 otelcol-contrib（$COLLECTOR_BIN），真机门禁跳过（请安装或设置 COLLECTOR_BIN）"
  exit 0
fi

cleanup() {
  [ -n "${SUP_PID:-}" ] && kill "$SUP_PID" 2>/dev/null
  [ -n "${SRV_PID:-}" ] && kill "$SRV_PID" 2>/dev/null
  # 清理由 fixture 托管的 collector 子进程（按配置路径精确匹配，避免误杀系统实例）
  pkill -f "$WORKDIR/sup/effective.yaml" 2>/dev/null
  sleep 1
}
trap cleanup EXIT

echo "== 准备：构建 server 与 supervisor fixture =="
rm -rf "$WORKDIR"
mkdir -p "$WORKDIR/sup"
export GOMODCACHE="${GOMODCACHE:-$REPO_ROOT/.gopath/pkg/mod}"
export GOCACHE="${GOCACHE:-$REPO_ROOT/.gocache}"
export GOFLAGS="${GOFLAGS:--mod=mod}"
export GOSUMDB=off
go build -o "$WORKDIR/cadenza" ./cmd/server || { echo "构建 server 失败"; exit 1; }
# fixture 是独立子模块（测试专用，避免把 opamp-go client 的依赖带进产品 go.mod）
(cd tests/supervisor-fixture && go build -o "$WORKDIR/supervisor-fixture" .) || { echo "构建 fixture 失败"; exit 1; }

cat > "$WORKDIR/base.yaml" <<YAML
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:${PORT_A}
exporters:
  debug:
    verbosity: basic
processors:
  batch:
    timeout: 3s
service:
  telemetry:
    metrics:
      level: none
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [debug]
YAML

python3 - "$WORKDIR/base.yaml" "$WORKDIR/apply-base.json" "$WORKDIR/apply-b.json" "$UID_REST" "$PORT_A" "$PORT_B" <<'PY'
import json, sys
base_path, out_base, out_b, uid, port_a, port_b = sys.argv[1:7]
base = open(base_path).read()
open(out_base, 'w').write(json.dumps({"collector_instance_uid": uid, "yaml": base, "note": "gate: base"}))
changed = base.replace("0.0.0.0:%s" % port_a, "0.0.0.0:%s" % port_b)
assert changed != base, "端口替换失败"
open(out_b, 'w').write(json.dumps({"collector_instance_uid": uid, "yaml": changed,
                                   "note": "gate: port %s->%s" % (port_a, port_b)}))
PY

echo "== 启动 server :$PORT =="
HTTP_ADDR="127.0.0.1:${PORT}" DB_DRIVER=sqlite DB_SQLITE_PATH="$WORKDIR/cadenza.db" \
  DEMO_MODE=false WEB_AUTH_MODE=off DISABLE_WEB=true OPAMP_AUTH_TOKEN="$GATE_TOKEN" \
  REQUIRE_APPROVAL=true OTELCOL_BIN="$COLLECTOR_BIN" STRICT_VALIDATE=true \
  LLM_API_KEY=sk-placeholder "$WORKDIR/cadenza" > "$WORKDIR/server.log" 2>&1 &
SRV_PID=$!
for _ in $(seq 1 40); do curl -s -m 2 "$BASE/healthz" >/dev/null 2>&1 && break; sleep 0.5; done
curl -s -m 2 "$BASE/healthz" | grep -q '"status":"ok"' || { echo "server 未就绪，见 $WORKDIR/server.log"; exit 1; }
ok "server 就绪（$BASE）"

echo "== 启动 supervisor fixture + 真实 collector =="
"$WORKDIR/supervisor-fixture" -server "ws://127.0.0.1:${PORT}/v1/opamp" -token "$GATE_TOKEN" \
  -instance-uid "$UID_DASHED" -collector "$COLLECTOR_BIN" \
  -config "$WORKDIR/base.yaml" -workdir "$WORKDIR/sup" > "$WORKDIR/supervisor.log" 2>&1 &
SUP_PID=$!

port_open() { (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }

echo "== 用例 1：真实 collector 接入与上报 =="
COLL=""
for _ in $(seq 1 40); do
  COLL=$(curl -s -m 3 "$BASE/api/v1/collectors" | python3 -c "
import json,sys
try:
    rows=json.load(sys.stdin)
except Exception:
    rows=[]
for c in rows:
    if c.get('instance_uid')=='$UID_REST': print(json.dumps(c))
" 2>/dev/null)
  [ -n "$COLL" ] && break
  sleep 0.5
done
if [ -z "$COLL" ]; then
  bad "collector 未注册（见 $WORKDIR/supervisor.log）"
else
  ok "collector 已注册"
  echo "$COLL" | python3 -c "
import json,sys
c=json.load(sys.stdin)
assert c['status']=='healthy', 'status='+str(c['status'])
assert c['version'], 'version 为空'
assert '$PORT_A' in c['effective_config'], 'effective_config 未含 $PORT_A'
" && ok "状态 healthy / version / effective config 上报（含 $PORT_A）" || bad "上报内容不符: $(echo "$COLL" | head -c 200)"
fi
port_open "$PORT_A" && ok "collector 进程真实监听 $PORT_A" || bad "未监听 $PORT_A（collector 未启动？）"
RESTARTS_1=$(grep -c "collector 已启动" "$WORKDIR/supervisor.log" 2>/dev/null || echo 0)

# apply_task 提交配置并返回任务 id（空表示提交失败，同时打印服务端响应便于定位）。
# 首次注册后短窗口内可能读到"目标 Collector 不存在"（上报已入注册表但持久化尚未可见），
# 因此做有限重试，保证门禁可重复。
apply_task() { # $1=请求体文件
  local body id attempt
  for attempt in 1 2 3 4 5; do
    body=$(curl -s -m 10 -X POST "$BASE/api/v1/tasks/apply" -H 'Content-Type: application/json' -d @"$1")
    id=$(printf '%s' "$body" | python3 -c "import json,sys
try: print(json.load(sys.stdin).get('id',''))
except Exception: print('')" 2>/dev/null)
    [ -n "$id" ] && break
    printf '%s' "$body" | grep -q "目标 Collector 不存在" || break
    sleep 1
  done
  if [ -z "$id" ]; then
    echo "    [apply 响应] $(printf '%s' "$body" | head -c 300)" >&2
  fi
  printf '%s' "$id"
}

approve_and_wait() { # $1=task id -> 输出 task json
  curl -s -m 30 -X POST "$BASE/api/v1/tasks/$1/approve" -H 'Content-Type: application/json' -d '{}' >/dev/null
  curl -s -m 5 "$BASE/api/v1/tasks/$1"
}

echo "== 用例 2：下发同配置 → 生效确认（不发重启命令） =="
T1=$(apply_task "$WORKDIR/apply-base.json")
APPROVED_1=$(approve_and_wait "$T1")
echo "$APPROVED_1" | python3 -c "
import json,sys
t=json.load(sys.stdin)
assert t['status']=='done', 'status='+str(t['status'])
assert not t.get('error'), 'error='+str(t.get('error'))
" && ok "任务 done 且收到生效确认（error 为空）" || bad "任务状态异常: $(echo "$APPROVED_1" | head -c 200)"

echo "== 用例 3：下发端口变更 → 进程真正换配置 =="
T2=$(apply_task "$WORKDIR/apply-b.json")
APPROVED_2=$(approve_and_wait "$T2")
echo "$APPROVED_2" | python3 -c "
import json,sys
t=json.load(sys.stdin)
assert t['status']=='done', 'status='+str(t['status'])
assert not t.get('error'), 'error='+str(t.get('error'))
" && ok "任务 done 且收到生效确认（error 为空）" || bad "任务状态异常: $(echo "$APPROVED_2" | head -c 200)"

for _ in $(seq 1 30); do port_open "$PORT_B" && break; sleep 0.5; done
port_open "$PORT_B" && ok "新端口 $PORT_B 已监听（进程真实重启换配置）" || bad "新端口 $PORT_B 未监听"
if port_open "$PORT_A"; then bad "旧端口 $PORT_A 仍在监听（未真正切换）"; else ok "旧端口 $PORT_A 已释放"; fi
grep -q "$PORT_B" "$WORKDIR/sup/effective.yaml" && ok "supervisor 托管的 effective.yaml 已重写为 $PORT_B" || bad "effective.yaml 未更新"
RESTARTS_2=$(grep -c "collector 已启动" "$WORKDIR/supervisor.log" 2>/dev/null || echo 0)
[ "$RESTARTS_2" -gt "$RESTARTS_1" ] && ok "collector 子进程重启次数增加（$RESTARTS_1 → $RESTARTS_2）" || bad "子进程未重启（$RESTARTS_1 → $RESTARTS_2）"
curl -s -m 3 "$BASE/api/v1/collectors/$UID_REST" | python3 -c "
import json,sys
e=json.load(sys.stdin)['effective_config']
assert '$PORT_B' in e, '服务端记录的 effective 未含 $PORT_B'
" && ok "服务端 effective_config 已更新为 $PORT_B" || bad "服务端 effective_config 未更新"

echo "== 用例 4：回滚到历史版本 → 进程再次换回 =="
VER=$(curl -s -m 5 "$BASE/api/v1/collectors/$UID_REST/versions?page=1&page_size=100" | python3 -c "
import json,sys
rows=json.load(sys.stdin)['items']
base=[v for v in rows if '$PORT_A' in v['yaml'] and '$PORT_B' not in v['yaml']]
print(base[0]['id'] if base else '')")
if [ -z "$VER" ]; then
  bad "未找到含 $PORT_A 的历史版本"
else
  ok "定位历史版本 id=$VER"
  T3=$(curl -s -m 5 -X POST "$BASE/api/v1/tasks/rollback" -H 'Content-Type: application/json' \
    -d "{\"collector_instance_uid\":\"$UID_REST\",\"version_id\":$VER}" \
    | python3 -c "import json,sys;print(json.load(sys.stdin)['id'])")
  APPROVED_3=$(approve_and_wait "$T3")
  echo "$APPROVED_3" | python3 -c "
import json,sys
t=json.load(sys.stdin)
assert t['status']=='done', 'status='+str(t['status'])
assert not t.get('error'), 'error='+str(t.get('error'))
" && ok "回滚任务 done 且收到生效确认" || bad "回滚任务异常: $(echo "$APPROVED_3" | head -c 200)"
  for _ in $(seq 1 30); do port_open "$PORT_A" && break; sleep 0.5; done
  port_open "$PORT_A" && ok "已回滚：$PORT_A 重新监听" || bad "回滚后 $PORT_A 未监听"
  if port_open "$PORT_B"; then bad "回滚后 $PORT_B 仍在监听"; else ok "回滚后 $PORT_B 已释放"; fi
  grep -q "$PORT_A" "$WORKDIR/sup/effective.yaml" && ok "effective.yaml 已回滚为 $PORT_A" || bad "effective.yaml 未回滚"
fi

echo "== 用例 5：审计留痕 =="
curl -s -m 5 "$BASE/api/v1/audit?page=1&page_size=100" | python3 -c "
import json,sys
rows=json.load(sys.stdin)['items']
acts={r['action'] for r in rows}
missing=[a for a in ('apply','approve','rollback') if a not in acts]
assert not missing, '缺少审计动作: '+str(missing)
" && ok "审计含 apply/approve/rollback 记录" || bad "审计动作缺失"

echo
echo "=============================================="
echo "真机门禁结果: $PASS 通过 / $FAIL 失败"
if [ "$FAIL" -eq 0 ]; then
  echo "全部通过 ✓（真实 collector 进程级生效已验证）"
  exit 0
fi
echo "存在失败 ✗（日志：$WORKDIR/server.log、$WORKDIR/supervisor.log）"
exit 1
