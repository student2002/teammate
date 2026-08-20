# ==============================================================
# Teammate Server — 多阶段 Docker 构建（支持国内镜像源）
#
# 构建阶段：golang:1.26-alpine 编译 Go 二进制
# 运行阶段：alpine:3.21 最小镜像，含 ca-certificates + 迁移文件
#
# 构建：
#   docker build -f product/Dockerfile -t teammate:latest .
#
# 国内镜像加速构建：
#   docker build -f product/Dockerfile -t teammate:latest \
#     --build-arg GOPROXY=https://goproxy.cn,direct \
#     --build-arg ALPINE_REPO=mirrors.tuna.tsinghua.edu.cn .
#
# 运行：
#   docker run -p 8080:8080 --env-file .env teammate:latest
#   docker run --rm teammate:latest migrate --path /app/migrations
# ==============================================================

# ---- 构建参数 ----
ARG GO_IMAGE=golang:1.26-alpine
ARG RUNTIME_IMAGE=alpine:3.21

# ---- 构建阶段 ----
FROM $GO_IMAGE AS builder

ARG GOPROXY
# 国内用户可传 --build-arg GOPROXY=https://goproxy.cn,direct
RUN if [ -n "$GOPROXY" ]; then go env -w GOPROXY="$GOPROXY"; fi

WORKDIR /build

# 先复制依赖文件，最大化构建缓存
COPY go.mod go.sum ./
RUN go mod download

# 复制源码并编译（CGO_ENABLED=0 不需要 gcc）
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /teammate-server ./cmd/teammate-server

# ---- 运行阶段 ----
FROM $RUNTIME_IMAGE

ARG ALPINE_REPO
# 国内用户可传 --build-arg ALPINE_REPO=mirrors.aliyun.com
RUN if [ -n "$ALPINE_REPO" ]; then \
      sed -i "s|dl-cdn.alpinelinux.org|$ALPINE_REPO|g" /etc/apk/repositories; \
    fi

# CA 证书（HTTPS/OAuth 需要）+ 时区
RUN apk add --no-cache ca-certificates tzdata && \
    update-ca-certificates

# 部署二进制（独立于 `teammate` CLI）：teammate-server 启动服务 / 执行迁移
COPY --from=builder /teammate-server /usr/local/bin/teammate-server

# 迁移文件 — 复制到 /app/migrations，运行时需指定 --path /app/migrations
#   迁移：docker compose run --rm app migrate --path /app/migrations
COPY --from=builder /build/internal/db/migrations /app/migrations

EXPOSE 8080

# ENTRYPOINT 固定为部署二进制；无 CMD 参数时启动服务（根命令默认行为）：
#   docker run -p 8080:8080 --env-file .env teammate:latest  -> 启动服务
#   docker compose run --rm app migrate --path /app/migrations -> 执行迁移（追加到 ENTRYPOINT）
ENTRYPOINT ["teammate-server"]
CMD []
