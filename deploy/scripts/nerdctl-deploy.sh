#!/usr/bin/env bash
# 用 Rancher Desktop 的 containerd 运行时（nerdctl）构建并运行 cadenza。
#
# 背景：Rancher Desktop 未启用 BuildKit，无法 nerdctl build，
# 采用"临时容器 + nerdctl cp + commit"方式构建镜像（已验证可行）。
#
# 用法：
#   bash deploy/scripts/nerdctl-deploy.sh            # 构建镜像
#   bash deploy/scripts/nerdctl-deploy.sh run        # 构建并运行
set -euo pipefail

NERDCTL="${NERDCTL:-/mnt/wsl/rancher-desktop/bin/nerdctl}"
IMAGE="cadenza:0.1.0"
NAME="cadenza"
PORT="${PORT:-8080}"
BASE_IMAGE="alpine:3.22"

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BIN_SRC="${BIN_SRC:-$REPO_ROOT/.build/cadenza}"

# 1. 构建静态二进制（若缺失）
build_binary() {
  if [ ! -x "$BIN_SRC" ]; then
    echo ">> 构建静态二进制..."
    (cd "$REPO_ROOT" && CGO_ENABLED=0 go build -o .build/cadenza ./cmd/server)
  fi
  echo ">> 二进制就绪: $BIN_SRC"
}

# 2. 用临时容器组装镜像
build_image() {
  echo ">> 拉取基础镜像 $BASE_IMAGE ..."
  "$NERDCTL" pull "$BASE_IMAGE" >/dev/null
  prep="$NAME-prep"
  "$NERDCTL" rm -f "$prep" >/dev/null 2>&1 || true
  "$NERDCTL" run -d --name "$prep" "$BASE_IMAGE" sleep 3600 >/dev/null
  "$NERDCTL" exec "$prep" sh -c 'mkdir -p /app /data /usr/local/bin && apk add --no-cache ca-certificates tzdata >/dev/null'
  echo ">> 写入二进制（stdin 方式，规避 WSL 路径翻译问题）..."
  "$NERDCTL" exec -i "$prep" sh -c 'cat > /app/cadenza && chmod +x /app/cadenza' < "$BIN_SRC"
  echo ">> 提交镜像 $IMAGE ..."
  "$NERDCTL" commit "$prep" "$IMAGE"
  "$NERDCTL" rm -f "$prep" >/dev/null
  echo ">> 镜像构建完成"
}

run_container() {
  echo ">> 运行容器 $NAME (端口 $PORT) ..."
  "$NERDCTL" rm -f "$NAME" >/dev/null 2>&1 || true
  "$NERDCTL" volume create opamp-data >/dev/null 2>&1 || true
  "$NERDCTL" run -d --name "$NAME" -p "$PORT:8080" \
    -e "LLM_API_KEY=${LLM_API_KEY:?需要设置 LLM_API_KEY}" \
    -e LLM_BASE_URL="${LLM_BASE_URL:-https://api.deepseek.com}" \
    -e DB_DRIVER=sqlite -e DB_SQLITE_PATH=/data/opamp.db \
    -e "OPAMP_AUTH_TOKEN=${OPAMP_AUTH_TOKEN:-}" \
    -v opamp-data:/data \
    "$IMAGE" /app/cadenza
  echo ">> 容器已启动，验证:"
  "$NERDCTL" ps | grep "$NAME"
  echo ">> Windows 侧访问: http://localhost:${PORT}"
  echo ">> 容器内自检: nerdctl exec $NAME wget -qO- http://127.0.0.1:8080/api/v1/collectors"
}

case "${1:-}" in
  run) build_binary && build_image && run_container ;;
  *)   build_binary && build_image ;;
esac
