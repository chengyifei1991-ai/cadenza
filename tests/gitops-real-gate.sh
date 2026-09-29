#!/usr/bin/env bash
# Cadenza GitOps 真机门禁：**真实 otelcol-contrib** 收到来自 git 仓库的配置并真实生效，
# 再按历史 commit 回退并再次真实生效（证明 GitOps 模式下"配置版本权威在 git"端到端可用）。
#
# 依赖：go、git、curl、python3、otelcol-contrib（缺失则 SKIP）
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

COLLECTOR_BIN="${COLLECTOR_BIN:-/usr/bin/otelcol-contrib}"
PORT="${PORT:-18781}"
BASE="http://127.0.0.1:${PORT}"
TOKEN="${GATE_TOKEN:-gitops-real-token}"
WORKDIR="${WORKDIR:-$REPO_ROOT/.build/gitops-real}"
UID_DASHED="77777777-8888-9999-0000-111122223333"
UID_REST="${UID_DASHED//-/}"
PORT_A=14361
PORT_B=14362

PASS=0
FAIL=0
ok()  { PASS=$((PASS+1)); echo "  ✓ $1"; }
bad() { FAIL=$((FAIL+1)); echo "  ✗ $1"; }

if [ ! -x "$COLLECTOR_BIN" ]; then
  echo "SKIP: 未找到 otelcol-contrib（$COLLECTOR_BIN），GitOps 真机门禁跳过"
  exit 0
fi

cleanup() {
  [ -n "${SUP_PID:-}" ] && kill "$SUP_PID" 2>/dev/null
  [ -n "${SRV_PID:-}" ] && kill "$SRV_PID" 2>/dev/null
  pkill -f "$WORKDIR/sup/effective.yaml" 2>/dev/null
  sleep 1
}
trap cleanup EXIT

echo "== 准备：git 仓库（两次提交，端口 ${PORT_A} → ${PORT_B}） =="
rm -rf "$WORKDIR"
mkdir -p "$WORKDIR/repo/collectors" "$WORKDIR/sup"
export GIT_AUTHOR_NAME=Cadenza GIT_AUTHOR_EMAIL=dev@example.com
export GIT_COMMITTER_NAME=Cadenza GIT_COMMITTER_EMAIL=dev@example.com
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
cfg() {
  cat <<YAML
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:$1
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
}
cfg "$PORT_A" > "$WORKDIR/repo/collectors/${UID_REST}.yaml"
git -C "$WORKDIR/repo" init -q -b main
git -C "$WORKDIR/repo" add .
git -C "$WORKDIR/repo" commit -q -m "commit A: 端口 ${PORT_A}"
COMMIT_A=$(git -C "$WORKDIR/repo" rev-parse HEAD)
cfg "$PORT_B" > "$WORKDIR/repo/collectors/${UID_REST}.yaml"
git -C "$WORKDIR/repo" add .
git -C "$WORKDIR/repo" commit -q -m "commit B: 端口 ${PORT_B}"
COMMIT_B=$(git -C "$WORKDIR/repo" rev-parse HEAD)
ok "仓库就绪（A=${COMMIT_A:0:8} → B=${COMMIT_B:0:8}）"

echo "== 构建 server 与 supervisor fixture =="
export GOMODCACHE="${GOMODCACHE:-$REPO_ROOT/.gopath/pkg/mod}"
export GOCACHE="${GOCACHE:-$REPO_ROOT/.gocache}"
export GOFLAGS="${GOFLAGS:--mod=mod}"
export GOSUMDB=off
go build -o "$WORKDIR/cadenza" ./cmd/server || { echo "构建 server 失败"; exit 1; }
(cd tests/supervisor-fixture && go build -o "$WORKDIR/supervisor-fixture" .) || { echo "构建 fixture 失败"; exit 1; }

echo "== 启动 GitOps 模式 server :$PORT =="
HTTP_ADDR="127.0.0.1:${PORT}" DB_DRIVER=sqlite DB_SQLITE_PATH="$WORKDIR/cadenza.db" \
  DEMO_MODE=false WEB_AUTH_MODE=off DISABLE_WEB=true OPAMP_AUTH_TOKEN="$TOKEN" \
  REQUIRE_APPROVAL=true CONFIG_SOURCE=git GIT_REPO_DIR="$WORKDIR/repo" \
  GIT_CONFIG_PATHSPEC="collectors/{uid}.yaml" \
  OTELCOL_BIN="$COLLECTOR_BIN" STRICT_VALIDATE=true \
  LLM_API_KEY=sk-placeholder "$WORKDIR/cadenza" > "$WORKDIR/server.log" 2>&1 &
SRV_PID=$!
for _ in $(seq 1 40); do curl -s -m 2 "$BASE/healthz" >/dev/null 2>&1 && break; sleep 0.5; done
curl -s -m 2 "$BASE/healthz" | grep -q '"status":"ok"' || { echo "server 未就绪"; exit 1; }
ok "GitOps server 就绪"

echo "== 启动真实 collector（supervisor 托管） =="
"$WORKDIR/supervisor-fixture" -server "ws://127.0.0.1:${PORT}/v1/opamp" -token "$TOKEN" \
  -instance-uid "$UID_DASHED" -collector "$COLLECTOR_BIN" \
  -config "$WORKDIR/repo/collectors/${UID_REST}.yaml" -workdir "$WORKDIR/sup" > "$WORKDIR/supervisor.log" 2>&1 &
SUP_PID=$!
port_open() { (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }

for _ in $(seq 1 40); do
  st=$(curl -s -m 3 "$BASE/api/v1/collectors" | python3 -c "
import json,sys
try: rows=json.load(sys.stdin)
except Exception: rows=[]
print(next((c['status'] for c in rows if c['instance_uid']=='$UID_REST'), ''))" 2>/dev/null)
  [ "$st" = "healthy" ] && break
  sleep 0.5
done
[ "${st:-}" = "healthy" ] && ok "真实 collector 注册并 healthy" || bad "collector 未 healthy（见 $WORKDIR/supervisor.log）"

approve_and_wait() {
  local code
  code=$(curl -s -m 30 -o "$WORKDIR/approve.json" -w '%{http_code}' \
    -X POST "$BASE/api/v1/tasks/$1/approve" -H 'Content-Type: application/json' -d '{}')
  [ "$code" = "200" ] || { echo "    [approve http=$code] $(head -c 160 "$WORKDIR/approve.json")" >&2; bad "approve 期望 200，实际 $code"; return; }
  local body
  body=$(curl -s -m 5 "$BASE/api/v1/tasks/$1")
  for _ in $(seq 1 20); do
    case "$(printf '%s' "$body" | python3 -c "import json,sys
try: print(json.load(sys.stdin).get('status',''))
except Exception: print('')")" in
      done|failed|rejected) break ;;
    esac
    sleep 0.5
    body=$(curl -s -m 5 "$BASE/api/v1/tasks/$1")
  done
  printf '%s' "$body"
}

echo "== 用例 1：按 git commit A 下发（真实生效到 $PORT_A） =="
T1=$(curl -s -m 5 -X POST "$BASE/api/v1/tasks/apply" -H 'Content-Type: application/json' \
  -d "{\"collector_instance_uid\":\"$UID_REST\",\"git_ref\":\"$COMMIT_A\",\"note\":\"gitops 真机 A\"}" \
  | python3 -c "import json,sys;print(json.load(sys.stdin).get('id',''))")
if [ -z "$T1" ]; then
  bad "创建 git 下发任务失败"
else
  D1=$(approve_and_wait "$T1")
  echo "$D1" | python3 -c "
import json,sys
t=json.load(sys.stdin)
assert t['status']=='done', 'status='+str(t.get('status'))
assert not t.get('error'), 'error='+str(t.get('error'))
assert t.get('git_commit')=='$COMMIT_A', 'git_commit='+str(t.get('git_commit'))
" && ok "任务 done、无告警、git 溯源为 commit A" || bad "任务状态/溯源不符: $(printf '%s' "$D1" | head -c 200)"
fi
for _ in $(seq 1 30); do port_open "$PORT_A" && break; sleep 0.5; done
port_open "$PORT_A" && ok "collector 真实监听 $PORT_A（内容来自 git commit A）" || bad "$PORT_A 未监听"

echo "== 用例 2：按 git commit B 下发（真实切到 $PORT_B） =="
T2=$(curl -s -m 5 -X POST "$BASE/api/v1/tasks/apply" -H 'Content-Type: application/json' \
  -d "{\"collector_instance_uid\":\"$UID_REST\",\"git_ref\":\"$COMMIT_B\",\"note\":\"gitops 真机 B\"}" \
  | python3 -c "import json,sys;print(json.load(sys.stdin).get('id',''))")
D2=$(approve_and_wait "$T2")
echo "$D2" | python3 -c "
import json,sys
t=json.load(sys.stdin)
assert t['status']=='done' and not t.get('error'), str(t)
" && ok "commit B 下发任务 done" || bad "commit B 任务不符: $(printf '%s' "$D2" | head -c 200)"
for _ in $(seq 1 30); do port_open "$PORT_B" && break; sleep 0.5; done
port_open "$PORT_B" && ok "collector 真实监听 $PORT_B" || bad "$PORT_B 未监听"
if port_open "$PORT_A"; then bad "$PORT_A 应已释放"; else ok "旧端口 $PORT_A 已释放"; fi

echo "== 用例 3：按 commit 回退（回到 commit A 的 $PORT_A） =="
T3=$(curl -s -m 5 -X POST "$BASE/api/v1/tasks/rollback" -H 'Content-Type: application/json' \
  -d "{\"collector_instance_uid\":\"$UID_REST\",\"git_commit\":\"$COMMIT_A\"}" \
  | python3 -c "import json,sys;print(json.load(sys.stdin).get('id',''))")
if [ -z "$T3" ]; then
  bad "创建按 commit 回退任务失败"
else
  D3=$(approve_and_wait "$T3")
  echo "$D3" | python3 -c "
import json,sys
t=json.load(sys.stdin)
assert t['status']=='done', 'status='+str(t.get('status'))
assert not t.get('error'), 'error='+str(t.get('error'))
assert t.get('git_commit')=='$COMMIT_A', 'git_commit='+str(t.get('git_commit'))
" && ok "回退任务 done、git 溯源为 commit A" || bad "回退任务不符: $(printf '%s' "$D3" | head -c 200)"
fi
for _ in $(seq 1 30); do port_open "$PORT_A" && break; sleep 0.5; done
port_open "$PORT_A" && ok "已按 commit 回退：真实监听 $PORT_A" || bad "回退后 $PORT_A 未监听"
if port_open "$PORT_B"; then bad "回退后 $PORT_B 仍在监听"; else ok "回退后 $PORT_B 已释放"; fi

echo "== 用例 4：审计含 git 溯源 =="
curl -s -m 5 "$BASE/api/v1/audit?page=1&page_size=100" | python3 -c "
import json,sys
rows=json.load(sys.stdin)['items']
trace=[r for r in rows if 'git 溯源 commit=' in r['detail']]
acts={r['action'] for r in rows}
assert trace, '缺 git 溯源审计'
assert {'apply','approve','rollback'} <= acts, '缺动作: '+str({'apply','approve','rollback'}-acts)
" && ok "审计含 git 溯源与 apply/approve/rollback" || bad "审计缺 git 溯源或动作"

echo
echo "=============================================="
echo "GitOps 真机门禁结果: $PASS 通过 / $FAIL 失败"
if [ "$FAIL" -eq 0 ]; then
  echo "全部通过 ✓（git 来源配置在真实 collector 上生效与按 commit 回退均已验证）"
  exit 0
fi
echo "存在失败 ✗（日志：$WORKDIR/server.log、$WORKDIR/supervisor.log）"
exit 1
