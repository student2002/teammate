# ==============================================================
# Teammate Server — multi-stage Docker build (with China mirror support)
#
# Build stage: golang:1.26-alpine compiles the Go binary
# Runtime stage: alpine:3.21 minimal image with ca-certificates + migration files
#
# Build:
#   docker build -f product/Dockerfile -t teammate:latest .
#
# Build with China mirror acceleration:
#   docker build -f product/Dockerfile -t teammate:latest \
#     --build-arg GOPROXY=https://goproxy.cn,direct \
#     --build-arg ALPINE_REPO=mirrors.tuna.tsinghua.edu.cn .
#
# Run:
#   docker run -p 8080:8080 --env-file .env teammate:latest
#   docker run --rm teammate:latest migrate --path /app/migrations
# ==============================================================

# ---- Build args ----
ARG GO_IMAGE=golang:1.26-alpine
ARG RUNTIME_IMAGE=alpine:3.21

# ---- Build stage ----
FROM $GO_IMAGE AS builder

ARG GOPROXY
# China users can pass --build-arg GOPROXY=https://goproxy.cn,direct
RUN if [ -n "$GOPROXY" ]; then go env -w GOPROXY="$GOPROXY"; fi

WORKDIR /build

# Copy dependency files first to maximize build cache
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build (CGO_ENABLED=0, no gcc needed)
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /teammate-server ./cmd/teammate-server

# ---- Runtime stage ----
FROM $RUNTIME_IMAGE

ARG ALPINE_REPO
# China users can pass --build-arg ALPINE_REPO=mirrors.aliyun.com
RUN if [ -n "$ALPINE_REPO" ]; then \
      sed -i "s|dl-cdn.alpinelinux.org|$ALPINE_REPO|g" /etc/apk/repositories; \
    fi

# CA certificates (required by HTTPS/OAuth) + timezone data
RUN apk add --no-cache ca-certificates tzdata && \
    update-ca-certificates

# Deployment binary (separate from the `teammate` CLI): teammate-server starts the service / runs migrations
COPY --from=builder /teammate-server /usr/local/bin/teammate-server

# Migration files — copied to /app/migrations; specify --path /app/migrations at runtime
#   Migrate: docker compose run --rm app migrate --path /app/migrations
COPY --from=builder /build/internal/db/migrations /app/migrations

EXPOSE 8080

# ENTRYPOINT is fixed to the deployment binary; with no CMD arguments it starts the service (root command default):
#   docker run -p 8080:8080 --env-file .env teammate:latest  -> start service
#   docker compose run --rm app migrate --path /app/migrations -> run migrations (appended to ENTRYPOINT)
ENTRYPOINT ["teammate-server"]
CMD []
