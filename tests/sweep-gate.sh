#!/usr/bin/env bash
# Cadenza 中间态超时巡检门禁（1.2.0 C-c / F-18）：进程级验证"卡住的任务会被收口"。
#
# 背景：任务停留在中间态（validating/applying）且写入失败兜底也失败时，会永久卡住
# （1.0.1 的 F-15/F-18 形状）。巡检按周期扫描并收口为 failed，写明来源并留审计。
#
# 覆盖：
#   1. 关闭语义：TASK_SWEEP_INTERVAL=0 → 卡住任务保持原状（默认不误伤）
#   2. 超时收口：停留超过阈值的 applying 任务 → failed，error 写明"巡检收口"
#   3. 事实留痕：task_events 含 → failed；审计含 actor=system / action=sweep
#   4. 指标可见：/api/v1/stats/ops 的 failed 计数包含被收口任务
#   5. 不误伤：未超时的 applying 与已终态 done 任务保持原状
#
# 依赖：bash、curl、python3。故障注入：向服务端 SQLite 直接落一条"卡住的中间态任务"
# （真实并发/崩溃难以在门禁里复现，注入的是状态，验证的是巡检行为）。
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

PORT="${PORT:-18790}"
BASE="http://127.0.0.1:${PORT}"
WORKDIR="${WORKDIR:-$REPO_ROOT/.build/sweep-run}"
DB="$WORKDIR/cadenza.db"

PASS=0
FAIL=0
ok()  { PASS=$((PASS+1)); echo "  ✓ $1"; }
bad() { FAIL=$((FAIL+1)); echo "  ✗ $1"; }

need() { command -v "$1" >/dev/null 2>&1 || { echo "SKIP: 缺少 $1"; exit 0; }; }
need curl
need python3

cleanup() {
  [ -n "${SRV_PID:-}" ] && kill "$SRV_PID" 2>/dev/null
  wait "${SRV_PID:-}" 2>/dev/null
}
trap cleanup EXIT

echo "== 构建 server =="
export GOMODCACHE="${GOMODCACHE:-$REPO_ROOT/.gopath/pkg/mod}"
export GOCACHE="${GOCACHE:-$REPO_ROOT/.gocache}"
export GOFLAGS="${GOFLAGS:--mod=mod}"
export GOSUMDB=off
rm -rf "$WORKDIR"
mkdir -p "$WORKDIR"
BIN="$WORKDIR/cadenza"
go build -o "$BIN" ./cmd/server || { echo "构建 server 失败"; exit 1; }

start_server() { # $1=sweep_interval $2=stuck_timeout $3=logfile
  HTTP_ADDR="127.0.0.1:${PORT}" DB_DRIVER=sqlite DB_SQLITE_PATH="$DB" \
    DEMO_MODE=false WEB_AUTH_MODE=off DISABLE_WEB=true REQUIRE_APPROVAL=true \
    TASK_SWEEP_INTERVAL="$1" TASK_STUCK_TIMEOUT="$2" \
    LLM_API_KEY=sk-placeholder "$BIN" > "$3" 2>&1 &
  SRV_PID=$!
  for _ in $(seq 1 40); do curl -s -m 2 "$BASE/healthz" >/dev/null 2>&1 && break; sleep 0.5; done
  curl -s -m 2 "$BASE/healthz" | grep -q '"status":"ok"'
}

stop_server() {
  [ -n "${SRV_PID:-}" ] && kill "$SRV_PID" 2>/dev/null
  wait "${SRV_PID:-}" 2>/dev/null
  SRV_PID=""
}

inject() { # $1=id $2=status $3=updated_ago_seconds
  python3 - "$DB" "$1" "$2" "$3" <<'PY'
import datetime, sqlite3, sys
db, tid, status, ago = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4])
ts = (datetime.datetime.now(datetime.timezone.utc) - datetime.timedelta(seconds=ago))
# 与 internal/store 的 timeFmt（RFC3339Nano）一致，便于秒级比较与解析
stamp = ts.strftime('%Y-%m-%dT%H:%M:%S.') + f"{ts.microsecond // 1000:03d}Z"
con = sqlite3.connect(db, timeout=10)
con.execute("PRAGMA busy_timeout=10000")
con.execute(
    """INSERT OR REPLACE INTO tasks
       (id,type,status,require_approval,input,generated_yaml,target_group_id,approvers,
        approver,reject_reason,model_used,error,created_at,updated_at)
       VALUES (?,?,?,1,'巡检门禁注入','','demo','[]','','','','',?,?)""",
    (tid, "apply", status, stamp, stamp),
)
con.commit()
con.close()
PY
}

status_of() { curl -s -m 5 "$BASE/api/v1/tasks/$1" | python3 -c "import json,sys;print(json.load(sys.stdin).get('status',''))"; }

echo "== 用例 1：巡检关闭（TASK_SWEEP_INTERVAL=0）不误伤 =="
if start_server 0 2s "$WORKDIR/off.log"; then
  ok "巡检关闭实例就绪（$BASE）"
else
  bad "实例未就绪（见 $WORKDIR/off.log）"; exit 1
fi
inject stuck-off applying 3600
sleep 4
if [ "$(status_of stuck-off)" = "applying" ]; then
  ok "巡检关闭时卡住任务保持原状（未误伤）"
else
  bad "巡检关闭仍改动了任务：status=$(status_of stuck-off)"
fi
stop_server

echo "== 用例 2：超时收口（interval=1s / timeout=2s） =="
if start_server 1s 2s "$WORKDIR/on.log"; then
  ok "巡检开启实例就绪（$BASE）"
else
  bad "实例未就绪（见 $WORKDIR/on.log）"; exit 1
fi
inject stuck-on applying 3600     # 卡住 1 小时 → 应收口
inject fresh-on applying 0        # 刚 updated → 不该动
inject done-on done 3600          # 终态 → 不该动

for _ in $(seq 1 30); do
  [ "$(status_of stuck-on)" = "failed" ] && break
  sleep 0.5
done

if [ "$(status_of stuck-on)" = "failed" ]; then
  ok "卡住的中间态任务被收口为 failed"
else
  bad "未被收口：status=$(status_of stuck-on)（见 $WORKDIR/on.log）"
fi

ERR=$(curl -s -m 5 "$BASE/api/v1/tasks/stuck-on" | python3 -c "import json,sys;print(json.load(sys.stdin).get('error',''))")
if printf '%s' "$ERR" | grep -q "巡检收口"; then
  ok "失败原因写明巡检来源（可区分系统收口与人为失败）"
else
  bad "error 未写明巡检来源：$ERR"
fi

if [ "$(status_of fresh-on)" = "applying" ] && [ "$(status_of done-on)" = "done" ]; then
  ok "未超时的中间态与终态任务均未被误伤"
else
  bad "误伤：fresh=$(status_of fresh-on) done=$(status_of done-on)"
fi

EVENTS=$(curl -s -m 5 "$BASE/api/v1/tasks/stuck-on/events")
if printf '%s' "$EVENTS" | grep -q '"to_status":"failed"'; then
  ok "状态迁移事件已补（时间线可见收口）"
else
  bad "缺少 → failed 事件：$(printf '%s' "$EVENTS" | head -c 200)"
fi

AUDIT=$(curl -s -m 5 "$BASE/api/v1/audit?action=sweep")
if printf '%s' "$AUDIT" | grep -q '"actor":"system"' && printf '%s' "$AUDIT" | grep -q 'stuck-on'; then
  ok "审计留痕（actor=system / action=sweep / 指向该任务）"
else
  bad "缺少巡检审计：$(printf '%s' "$AUDIT" | head -c 200)"
fi

OPS=$(curl -s -m 5 "$BASE/api/v1/stats/ops?window_days=1")
if printf '%s' "$OPS" | python3 -c "
import json,sys
d=json.load(sys.stdin)
sys.exit(0 if d['tasks']['failed'] >= 1 else 1)"; then
  ok "运维效率指标计入收口任务（failed ≥ 1）"
else
  bad "效率指标未计入：$(printf '%s' "$OPS" | head -c 200)"
fi

echo
echo "=============================================="
echo "巡检门禁结果: $PASS 通过 / $FAIL 失败"
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
echo "全部通过 ✓（中间态超时收口：关闭不误伤 / 超时收口 / 留痕 / 指标可见）"
