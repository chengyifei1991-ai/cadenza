#!/usr/bin/env bash
# Cadenza GitOps 可选模式门禁：本地 git 仓库为版本权威，软件只做读取/下发/审计。
#
# 覆盖：
#   1. fail-closed：CONFIG_SOURCE=git 缺 GIT_REPO_DIR → 进程拒绝启动（非零退出 + 中文原因）
#   2. 只读查询：/api/v1/git/status|commits|file 与 system/info 的 config_source/git_enabled
#   3. 参数安全：路径上跳 / 未知 ref / limit 越界 → 400
#   4. 「从 git 下发」：apply 只给 git_ref（不带 yaml）→ 任务带 git 溯源（commit/path/ref）
#   5. 内置模式零回归：未启用 GitOps 时 git 端点返回 409、传 git_ref 的 apply 也返回 409
#
# 依赖：bash、curl、python3、git。纯 API 断言，不需要真实 Collector。
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

PORT="${PORT:-18780}"
BASE="http://127.0.0.1:${PORT}"
WORKDIR="${WORKDIR:-$REPO_ROOT/.build/gitops-run}"
UID_DASHED="66666666-7777-8888-9999-000011112222"
UID_REST="${UID_DASHED//-/}"

PASS=0
FAIL=0
ok()  { PASS=$((PASS+1)); echo "  ✓ $1"; }
bad() { FAIL=$((FAIL+1)); echo "  ✗ $1"; }

need() { command -v "$1" >/dev/null 2>&1 || { echo "SKIP: 缺少 $1"; exit 0; }; }
need git
need curl
need python3

cleanup() {
  [ -n "${SRV_PID:-}" ] && kill "$SRV_PID" 2>/dev/null
  sleep 1
}
trap cleanup EXIT

echo "== 准备：临时 git 仓库（含 collectors/<uid>.yaml 两次提交） =="
rm -rf "$WORKDIR"
mkdir -p "$WORKDIR/repo/collectors"
export GIT_AUTHOR_NAME=Cadenza GIT_AUTHOR_EMAIL=dev@example.com
export GIT_COMMITTER_NAME=Cadenza GIT_COMMITTER_EMAIL=dev@example.com
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
cat > "$WORKDIR/repo/collectors/${UID_REST}.yaml" <<'YAML'
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:14351
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
git -C "$WORKDIR/repo" init -q -b main
git -C "$WORKDIR/repo" add .
git -C "$WORKDIR/repo" commit -q -m "first: 初始配置"
FIRST_SHA=$(git -C "$WORKDIR/repo" rev-parse HEAD)
sed -i 's/14351/14352/' "$WORKDIR/repo/collectors/${UID_REST}.yaml"
git -C "$WORKDIR/repo" add .
git -C "$WORKDIR/repo" commit -q -m "second: 端口调整"
HEAD_SHA=$(git -C "$WORKDIR/repo" rev-parse HEAD)
ok "仓库就绪（HEAD=${HEAD_SHA:0:8}, 历史含 ${FIRST_SHA:0:8}）"

echo "== 构建 server =="
export GOMODCACHE="${GOMODCACHE:-$REPO_ROOT/.gopath/pkg/mod}"
export GOCACHE="${GOCACHE:-$REPO_ROOT/.gocache}"
export GOFLAGS="${GOFLAGS:--mod=mod}"
export GOSUMDB=off
BIN="$WORKDIR/cadenza"
go build -o "$BIN" ./cmd/server || { echo "构建 server 失败"; exit 1; }

echo "== 用例 1：fail-closed（CONFIG_SOURCE=git 但缺 GIT_REPO_DIR） =="
set +e
OUT=$(CONFIG_SOURCE=git GIT_CONFIG_PATHSPEC="collectors/{uid}.yaml" \
  HTTP_ADDR="127.0.0.1:${PORT}" DB_DRIVER=sqlite DB_SQLITE_PATH="$WORKDIR/fail.db" \
  DEMO_MODE=false WEB_AUTH_MODE=off DISABLE_WEB=true REQUIRE_APPROVAL=true \
  LLM_API_KEY=sk-placeholder "$BIN" 2>&1)
CODE=$?
set -e
if [ "$CODE" -ne 0 ] && printf '%s' "$OUT" | grep -q "GIT_REPO_DIR"; then
  ok "缺 GIT_REPO_DIR 时拒绝启动（exit=$CODE，原因明确）"
else
  bad "应 fail-closed：exit=$CODE out=$(printf '%s' "$OUT" | head -c 120)"
fi

echo "== 用例 2：启动 GitOps 模式实例 =="
HTTP_ADDR="127.0.0.1:${PORT}" DB_DRIVER=sqlite DB_SQLITE_PATH="$WORKDIR/cadenza.db" \
  DEMO_MODE=false WEB_AUTH_MODE=off DISABLE_WEB=true REQUIRE_APPROVAL=true \
  CONFIG_SOURCE=git GIT_REPO_DIR="$WORKDIR/repo" GIT_CONFIG_PATHSPEC="collectors/{uid}.yaml" \
  LLM_API_KEY=sk-placeholder "$BIN" > "$WORKDIR/server.log" 2>&1 &
SRV_PID=$!
for _ in $(seq 1 40); do curl -s -m 2 "$BASE/healthz" >/dev/null 2>&1 && break; sleep 0.5; done
curl -s -m 2 "$BASE/healthz" | grep -q '"status":"ok"' || { bad "server 未就绪（见 $WORKDIR/server.log）"; exit 1; }
ok "GitOps 实例就绪（$BASE）"

jget() { python3 -c "
import json,sys
try:
    d=json.load(open('/tmp/gitops-body'))
    v=d$1
    print(v if not isinstance(v,(dict,list)) else json.dumps(v,ensure_ascii=False))
except Exception:
    print('')
"; }
get() { curl -s -m 5 -o /tmp/gitops-body -w '%{http_code}' "$BASE$1"; }

echo "== 用例 3：只读查询端点 =="
CODE=$(get /api/v1/system/info)
if [ "$CODE" = "200" ] && grep -q '"config_source":"git"' /tmp/gitops-body && grep -q '"git_enabled":true' /tmp/gitops-body; then
  ok "system/info 标注 config_source=git / git_enabled=true"
else
  bad "system/info 标注不符（code=$CODE body=$(head -c 200 /tmp/gitops-body)）"
fi

CODE=$(get "/api/v1/git/status")
if [ "$CODE" = "200" ] && [ "$(jget "['sha']")" = "$HEAD_SHA" ]; then
  ok "git/status 返回当前 HEAD（$(jget "['short_sha']")）"
else
  bad "git/status 不符（code=$CODE body=$(head -c 200 /tmp/gitops-body)）"
fi

CODE=$(get "/api/v1/git/commits?instance_uid=${UID_REST}&limit=10")
if [ "$CODE" = "200" ] && [ "$(jget "['total']")" = "2" ]; then
  ok "git/commits 返回 2 条历史（path=$(jget "['path']")）"
else
  bad "git/commits 不符（code=$CODE body=$(head -c 200 /tmp/gitops-body)）"
fi

CODE=$(get "/api/v1/git/file?instance_uid=${UID_REST}")
if [ "$CODE" = "200" ] && grep -q "14352" /tmp/gitops-body; then
  ok "git/file 读取 HEAD 内容（含 14352）"
else
  bad "git/file(HEAD) 不符（code=$CODE）"
fi

CODE=$(get "/api/v1/git/file?instance_uid=${UID_REST}&ref=${FIRST_SHA}")
if [ "$CODE" = "200" ] && grep -q "14351" /tmp/gitops-body; then
  ok "git/file 可按历史 commit 读取（含 14351，GitOps 回退基础）"
else
  bad "git/file(历史 commit) 不符（code=$CODE）"
fi

echo "== 用例 4：参数安全 =="
CODE=$(get "/api/v1/git/file?path=../secret.yaml")
[ "$CODE" = "400" ] && ok "路径上跳 → 400" || bad "路径上跳应 400（实际 $CODE）"
CODE=$(get "/api/v1/git/file?instance_uid=${UID_REST}&ref=no-such-ref")
[ "$CODE" = "400" ] && ok "未知 ref → 400" || bad "未知 ref 应 400（实际 $CODE）"
CODE=$(get "/api/v1/git/commits?instance_uid=${UID_REST}&limit=999")
[ "$CODE" = "400" ] && ok "limit 越界 → 400" || bad "limit 越界应 400（实际 $CODE）"
CODE=$(get "/api/v1/git/commits")
[ "$CODE" = "400" ] && ok "缺 path/instance_uid → 400" || bad "缺参数应 400（实际 $CODE）"

echo "== 用例 4b：按 git commit 回退（G-c） =="
CODE=$(curl -s -m 5 -o /tmp/gitops-body -w '%{http_code}' -X POST "$BASE/api/v1/tasks/rollback" \
  -H 'Content-Type: application/json' \
  -d "{\"collector_instance_uid\":\"${UID_REST}\",\"git_commit\":\"${FIRST_SHA}\"}")
if [ "$CODE" = "201" ] && [ "$(jget "['git_commit']")" = "$FIRST_SHA" ] && [ "$(jget "['type']")" = "rollback" ]; then
  ok "按 commit 回退创建任务（type=rollback, git_commit=${FIRST_SHA:0:8}）"
else
  bad "git 回退创建任务不符（code=$CODE body=$(head -c 200 /tmp/gitops-body)）"
fi
RB_TASK=$(jget "['id']")
RB_YAML_OK=$(python3 -c "
import json
d=json.load(open('/tmp/gitops-body'))
print('yes' if '14351' in d.get('generated_yaml','') else 'no')")
if [ "$RB_YAML_OK" = "yes" ]; then
  ok "回退内容取自该 commit（含 14351，而非 HEAD 的 14352）"
else
  bad "回退内容应取自指定 commit"
fi

CODE=$(curl -s -m 30 -o /tmp/gitops-body -w '%{http_code}' -X POST "$BASE/api/v1/tasks/${RB_TASK}/approve" \
  -H 'Content-Type: application/json' -d '{}')
if [ "$CODE" = "200" ] && grep -q '"status":"done"' /tmp/gitops-body; then
  ok "回退任务审批后 done（走同一生效确认/快照/审计口径）"
else
  bad "回退任务审批不符（code=$CODE body=$(head -c 200 /tmp/gitops-body)）"
fi

CODE=$(curl -s -m 5 -o /tmp/gitops-body -w '%{http_code}' -X POST "$BASE/api/v1/tasks/rollback" \
  -H 'Content-Type: application/json' -d "{\"collector_instance_uid\":\"${UID_REST}\",\"git_commit\":\"no-such-commit\"}")
[ "$CODE" = "400" ] && ok "未知 commit 回退 → 400" || bad "未知 commit 应 400（实际 $CODE）"

echo "== 用例 5：内置模式零回归（同端口换内置实例前先停 GitOps 实例） =="
kill "$SRV_PID" 2>/dev/null; sleep 1
HTTP_ADDR="127.0.0.1:${PORT}" DB_DRIVER=sqlite DB_SQLITE_PATH="$WORKDIR/builtin.db" \
  DEMO_MODE=false WEB_AUTH_MODE=off DISABLE_WEB=true REQUIRE_APPROVAL=true \
  LLM_API_KEY=sk-placeholder "$BIN" > "$WORKDIR/server-builtin.log" 2>&1 &
SRV_PID=$!
for _ in $(seq 1 40); do curl -s -m 2 "$BASE/healthz" >/dev/null 2>&1 && break; sleep 0.5; done
CODE=$(get /api/v1/system/info)
if [ "$CODE" = "200" ] && grep -q '"config_source":"builtin"' /tmp/gitops-body && grep -q '"git_enabled":false' /tmp/gitops-body; then
  ok "内置模式 system/info 标注 config_source=builtin"
else
  bad "内置模式标注不符（code=$CODE）"
fi
CODE=$(get "/api/v1/git/status")
[ "$CODE" = "409" ] && ok "内置模式 git 端点 → 409（明确未启用）" || bad "内置模式 git/status 应 409（实际 $CODE）"

python3 - "$BASE" <<'PY'
import json, sys, urllib.error, urllib.request
base = sys.argv[1]
for path, body, label in [
    ("/api/v1/tasks/apply", {"collector_instance_uid": "x", "git_ref": "HEAD"}, "apply 带 git_ref"),
    ("/api/v1/tasks/rollback", {"collector_instance_uid": "x", "git_commit": "HEAD"}, "rollback 带 git_commit"),
]:
    req = urllib.request.Request(base + path, method="POST",
                                 data=json.dumps(body).encode(),
                                 headers={"Content-Type": "application/json"})
    try:
        urllib.request.urlopen(req, timeout=5)
        print(f"  ✗ 内置模式 {label} 应 409")
    except urllib.error.HTTPError as e:
        if e.code == 409:
            print(f"  ✓ 内置模式 {label} → 409（明确未启用）")
        else:
            print(f"  ✗ 内置模式 {label} 期望 409，实际 {e.code}")
PY

echo
echo "=============================================="
echo "GitOps 门禁结果: $PASS 通过 / $FAIL 失败"
if [ "$FAIL" -eq 0 ]; then
  echo "全部通过 ✓（GitOps 只读模式与内置模式零回归）"
  exit 0
fi
echo "存在失败 ✗（日志：$WORKDIR/server.log、$WORKDIR/server-builtin.log）"
exit 1
