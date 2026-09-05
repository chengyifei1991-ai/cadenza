# cadenza 容器镜像
# 多阶段构建：静态 Go 二进制 + 轻量运行时
# 参考：https://opentelemetry.io/docs/collector/install/binary/linux/

# ── 构建阶段 ──
FROM golang:1.26.6 AS builder

WORKDIR /src

# 依赖层缓存：先拷贝 go.mod/go.sum 再 go mod download
COPY go.mod go.sum ./
RUN go mod download

# 源码 + 构建（静态链接，便于在任何基础镜像运行）
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/cadenza ./cmd/server

# ── 运行时阶段 ──
FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S opamp && adduser -S -G opamp opamp

WORKDIR /app
COPY --from=builder /out/cadenza /app/cadenza
# 随镜像分发许可与第三方声明（Apache-2.0 §4 再分发要求）
COPY LICENSE THIRD_PARTY_NOTICES.md /app/
COPY licenses/ /app/licenses/

# 数据目录（SQLite 卷挂载点）
RUN mkdir -p /data && chown opamp:opamp /data /app

USER opamp

# 默认端口：HTTP（OpAMP /v1/opamp + MCP /mcp + REST /api/v1 一体）
EXPOSE 8080

# 数据卷
VOLUME ["/data"]

ENV HTTP_ADDR=:8080 \
    DB_DRIVER=sqlite \
    DB_SQLITE_PATH=/data/opamp.db \
    LLM_BASE_URL=https://api.deepseek.com \
    LLM_MODEL=deepseek-chat \
    OTELCOL_BIN=/usr/local/bin/otelcol-contrib

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/app/cadenza"]
