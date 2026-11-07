# Teammate 部署指南

## 目录

1. [快速启动（Docker Compose）](#1-快速启动docker-compose)
2. [环境变量清单](#2-环境变量清单)
3. [数据库迁移](#3-数据库迁移)
4. [健康检查与就绪探针](#4-健康检查与就绪探针)
5. [生产配置清单](#5-生产配置清单)
6. [备份与恢复](#6-备份与恢复)
7. [监控与日志](#7-监控与日志)
8. [故障排除](#8-故障排除)

---

## 1. 快速启动（Docker Compose）

### 前置条件

- Docker Engine 24+ 和 Docker Compose v2
- `openssl`（用于生成密钥）

### 国内镜像加速

默认镜像源（Docker Hub、Go Proxy、Alpine apk）在国内访问较慢。Dockerfile 和 docker-compose 已内置镜像源参数，无需修改文件即可使用国内源：

```bash
# 在 `product/.env` 中加入以下变量即可自动使用国内镜像
cat >> product/.env << 'EOF'
DOCKER_REGISTRY=registry.cn-hangzhou.aliyuncs.com/
GOPROXY=https://goproxy.cn,direct
ALPINE_REPO=mirrors.aliyun.com
EOF
```

各参数说明：

| 环境变量 | 默认值 | 说明 |
|----------|--------|------|
| `DOCKER_REGISTRY` | `（空）` | Docker 镜像仓库前缀，如 `registry.cn-hangzhou.aliyuncs.com/` |
| `GOPROXY` | `（空，使用 Go 默认）` | Go 模块代理，如 `https://goproxy.cn,direct` |
| `ALPINE_REPO` | `（空，使用 Alpine 默认）` | Alpine apk 镜像，如 `mirrors.aliyun.com` |

常用镜像源：

- **Docker Hub 镜像**：`dockerproxy.cn` / `docker.m.daocloud.io` / `registry.cn-hangzhou.aliyuncs.com`
- **Go 代理**：`https://goproxy.cn`（七牛云） / `https://goproxy.io`
- **Alpine 镜像**：`mirrors.aliyun.com` / `mirrors.tuna.tsinghua.edu.cn` / `mirrors.ustc.edu.cn`

> 注意：若 Docker daemon 已配置 `registry-mirrors`，则无需设置 `DOCKER_REGISTRY`。<br>
> `DOCKER_REGISTRY` 值末尾必须带 `/`，如 `dockerproxy.cn/`。

### 启动路径

两条路径共用同一份 compose 文件与同一个 `product/.env`。区别只在镜像来源：

| 路径 | 适用 | 镜像来源 | 命令 |
|------|------|----------|------|
| A 零构建 | 只想部署使用 | 仓库拉取预构建镜像 | `pull` + `up -d` |
| B 本地构建 | 改过源码 | 本机 Docker 构建 | `up -d --build` |

#### 路径 A：零构建启动（只需要 Docker）

```bash
# 1. 克隆项目
git clone <repo-url> teammate
cd teammate

# 2. 创建 product/.env 文件（生成生产密钥）
cat > product/.env << EOF
TEAMS_JWT_SECRET=$(openssl rand -hex 32)
TEAMMATE_ENCRYPTION_KEY_BASE64=$(openssl rand 32 | base64 -w0)
TEAMS_ALLOWED_ORIGINS=https://your-frontend-domain.com
TEAMS_BASE_URL=https://api.your-domain.com
TEAMS_DB_PASS=$(openssl rand -hex 16)
EOF

# 3. 登录镜像仓库（Gitea 容器仓库为明文 HTTP，需先在 daemon.json 配 insecure-registries）
docker login host.docker.internal:3000 -u <你的用户名>

# 4. 拉取预构建镜像并启动（app 启动时自动执行数据库迁移，成功后才启动服务）
docker compose --env-file product/.env -f product/docker-compose.prod.yml pull
docker compose --env-file product/.env -f product/docker-compose.prod.yml up -d
```

#### 路径 B：本地构建启动（改过源码后）

```bash
docker compose --env-file product/.env -f product/docker-compose.prod.yml up -d --build
```

#### 验证服务状态

```bash
curl http://localhost:28080/health     # → {"status":"ok"}
curl http://localhost:28080/ready      # → {"status":"ready"}
```

### 镜像来源参数

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `TEAMS_REGISTRY` | `host.docker.internal:3000/zhenfeng` | 镜像仓库前缀（含 host:port 与命名空间）。换仓库只改这里 |
| `TEAMS_IMAGE_TAG` | `latest` | 镜像 tag。生产部署建议固定为发布版本号（如 `0.1.0`），便于回滚 |

仅当本地不存在对应镜像时 `up` 才会拉取；需要强制更新到远端某 tag 时，先 `docker compose ... pull`。

### 端口与 dev 环境隔离

生产栈使用项目名 `teammate`（数据卷 `teammate_pgdata`），宿主机端口与开发栈（`docker-compose.yml`，项目名 `teammate-dev`，端口 15432/16379）完全错开，两套栈可同时运行：

| 服务 | 宿主机端口 | 说明 |
|------|-----------|------|
| postgres | `127.0.0.1:25432` | 数据卷 `teammate_pgdata`，密码为 `TEAMS_DB_PASS`（仅首次初始化生效） |
| redis | `127.0.0.1:26379` | 无持久化 |
| app | `127.0.0.1:28080`（`TEAMS_PORT` 可覆盖） | 容器内仍监听 8080 |
| web | `3333`（`WEB_PORT` 可覆盖） | 容器内仍监听 3000 |

### 停止服务

```bash
docker compose --env-file product/.env -f product/docker-compose.prod.yml down
```

保留数据卷：

```bash
docker compose --env-file product/.env -f product/docker-compose.prod.yml down --volumes   # ⚠️ 删除所有数据
```

### 维护者：发布预构建镜像

发布由 Gitea Actions 完成，workflow 为 `.gitea/workflows/release.yml`，推送 `v*` tag 触发：

```bash
git tag v0.1.0
git push local --tags
```

产物：`<TEAMS_REGISTRY>/teammate-server:{0.1.0,latest}`、`<TEAMS_REGISTRY>/teammate-web:{0.1.0,latest}`，仅 `linux/amd64`。

一次性前置：

1. Gitea 创建访问令牌，作用域含 `write:package`。
2. 仓库 Actions 设置中配置 secrets：`TEAMS_REGISTRY_USER`、`TEAMS_REGISTRY_TOKEN`；vars：`TEAMS_REGISTRY`（如 `host.docker.internal:3000/zhenfeng`）、`NEXT_PUBLIC_API_BASE_URL`（供 Web 容器内服务端 `/api` 反代使用，填容器内可达地址，如 `http://app:8080`；浏览器端不消费该值）。
3. `daemon.json` 增加 `"insecure-registries": ["host.docker.internal:3000"]` 并重启 Docker（Gitea 容器仓库为明文 HTTP）。
4. 拉起 runner：`docker start gitea-runner`。

构建缓存写入独立的 `teammate-server-buildcache` / `teammate-web-buildcache` 镜像，与正式镜像的 tag 列表分开。

---

## 2. 环境变量清单

所有环境变量以 `TEAMS_` 为前缀。生产环境必须显式配置标记为 **必需** 的变量。

### 核心配置

| 变量 | 默认值 | 必需 | 说明 |
|------|--------|------|------|
| `TEAMS_PORT` | `8080` | 否 | HTTP 监听端口 |
| `TEAMS_DATABASE_URL` | 见下 | **是** | PostgreSQL 连接串 |
| `TEAMS_REDIS_URL` | 见下 | **是** | Redis 连接串 |
| `TEAMS_JWT_SECRET` | `dev-secret-change-me` | **是** | JWT 签名密钥（至少 32 位 hex） |
| `TEAMS_ALLOWED_ORIGINS` | `*` | **是** | CORS 允许的源（逗号分隔，生产禁止 `*`） |
| `TEAMS_BASE_URL` | `http://localhost:8080` | 否 | 服务器公网 URL（OAuth 回调用） |

### 加密密钥

| 变量 | 默认值 | 必需 | 说明 |
|------|--------|------|------|
| `TEAMMATE_ENCRYPTION_KEY_BASE64` | — | **是** | AES-256 密钥，32 字节 Base64 编码 |
| `TEAMMATE_DEV` | — | 否 | 设为 `true` 启用开发模式（放松安全检查） |

JWT Secret 和 Encryption Key 生成命令：

```bash
# JWT Secret（64 位 hex = 32 字节）
openssl rand -hex 32

# Encryption Key（32 字节 → Base64）
openssl rand 32 | base64 -w0
```

### OAuth 配置（可选）

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `TEAMS_GITHUB_CLIENT_ID` | — | GitHub OAuth App Client ID |
| `TEAMS_GITHUB_SECRET` | — | GitHub OAuth App Client Secret |
| `TEAMS_GOOGLE_CLIENT_ID` | — | Google OAuth Client ID |
| `TEAMS_GOOGLE_SECRET` | — | Google OAuth Client Secret |

### 超时配置（可选）

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `TEAMS_READ_TIMEOUT` | `15s` | HTTP 读取超时 |
| `TEAMS_WRITE_TIMEOUT` | `60s` | HTTP 写入超时 |

### Docker Compose 默认连接串

在 Docker Compose 环境中，数据库和 Redis 的默认连接串为：

```
TEAMS_DATABASE_URL=postgres://postgres:${TEAMS_DB_PASS}@postgres:5432/teammate?sslmode=disable
TEAMS_REDIS_URL=redis://redis:6379/0
```

---

## 3. 数据库迁移

### 自动迁移（默认）

app 容器每次启动时都会先执行数据库迁移、成功后再启动 HTTP 服务（`docker-compose.prod.yml` 中的 `entrypoint` 已内建），因此正常部署**无需手动迁移**。迁移是幂等的，且 golang-migrate 通过 Postgres advisory lock 保证多实例并发启动时只有一个实例真正执行迁移。

### 手动迁移（排查问题用）

```bash
docker compose --env-file product/.env -f product/docker-compose.prod.yml run --rm --entrypoint teammate-server app migrate --path /app/migrations
```

### 升级（源码更新后）

```bash
# 重建镜像并重启，app 启动时会自动先跑迁移
docker compose --env-file product/.env -f product/docker-compose.prod.yml up -d --build
```

### 验证迁移状态

迁移是幂等的，重复执行不会损坏已有数据。迁移文件位于 `internal/db/migrations/`。

---

## 4. 健康检查与就绪探针

### 端点

| 端点 | 用途 | 预期响应 |
|------|------|----------|
| `GET /health` | 存活检查（Liveness） | `{"status":"ok"}` (200) |
| `GET /ready` | 就绪检查（Readiness，验证 DB 连接） | `{"status":"ready"}` (200) 或 `{"status":"not_ready"}` (503) |

### Docker Compose 健康检查

PostgreSQL 和 Redis 已在 compose 文件中配置了 `healthcheck`。应用容器通过 `depends_on` 的 `condition: service_healthy` 确保依赖就绪后才启动。

### Kubernetes 探针示例

```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 15

readinessProbe:
  httpGet:
    path: /ready
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 10
  failureThreshold: 3
```

---

## 5. 生产配置清单

部署上线前逐项确认：

- [ ] **JWT Secret** 已从默认值改为随机值
- [ ] **Encryption Key** 已配置为 32 字节 Base64
- [ ] **CORS 源** 已限定为前端域名（非 `*`）
- [ ] **数据库密码** 已改为强密码
- [ ] **Redis** 已配置密码或绑定到 `127.0.0.1`
- [ ] **TLS** 已配置（通过反向代理如 Nginx/Caddy 终结 HTTPS）
- [ ] **请求体大小限制** 已按需调整（`TEAMS_MAX_BODY_SIZE`，默认 10MB）
- [ ] **数据库连接池** 已按并发量调整（代码默认：`SetMaxOpenConns(25)`）
- [ ] **日志级别** 已设置为 `warn` 或 `error`（开发默认 `info`）
- [ ] **数据库定期备份** 已配置
- [ ] **健康检查端点** 已纳入容器编排探针
- [ ] **时区** 已配置（镜像默认 UTC）

---

## 6. 备份与恢复

### PostgreSQL 备份

```bash
# 备份
docker exec -t teammate-postgres-1 pg_dump -U postgres teammate > backup_$(date +%Y%m%d_%H%M%S).sql

# 压缩备份
docker exec -t teammate-postgres-1 pg_dump -U postgres teammate | gzip > backup_$(date +%Y%m%d_%H%M%S).sql.gz
```

### PostgreSQL 恢复

```bash
# 从 SQL 文件恢复
cat backup.sql | docker exec -i teammate-postgres-1 psql -U postgres teammate

# 从压缩文件恢复
gunzip -c backup.sql.gz | docker exec -i teammate-postgres-1 psql -U postgres teammate
```

### 完整备份脚本

```bash
#!/bin/bash
# backup.sh — 数据库、密钥、配置文件全量备份

BACKUP_DIR="./backups/$(date +%Y%m%d)"
mkdir -p "$BACKUP_DIR"

# 备份数据库
docker exec teammate-postgres-1 pg_dump -U postgres teammate | gzip > "$BACKUP_DIR/db.sql.gz"

# 备份.env 配置
cp .env "$BACKUP_DIR/env.bak"

# 保留最近 7 天，清理旧备份
find ./backups -maxdepth 1 -type d -mtime +7 -exec rm -rf {} \;

echo "Backup completed: $BACKUP_DIR"
```

---

## 7. 监控与日志

### 查看日志

```bash
# 应用日志
docker compose --env-file product/.env -f product/docker-compose.prod.yml logs -f app

# 所有服务
docker compose --env-file product/.env -f product/docker-compose.prod.yml logs -f
```

### 日志格式

日志输出为结构化格式（通过 Go 的 `log/slog`），可按服务名、级别过滤：

```
2025/06/14 22:30:00 INFO server starting addr=:8080
2025/06/14 22:30:01 INFO auth login user_id=xxx
```

### 指标

当前无内置 Prometheus 指标端点。建议在生产环境中：
- 通过 `GET /ready` 实现 Kubernetes 就绪探针
- 通过容器编排平台（K8s/Nomad）监控资源使用
- 使用 `docker stats` 或 cAdvisor 采集容器指标

---

## 8. 故障排除

### 问题：服务启动失败，提示"encryption key not initialized"

**原因：** 未设置 `TEAMMATE_ENCRYPTION_KEY_BASE64`。
**解决：** 生成 32 字节 Base64 密钥并设置环境变量。

```bash
echo "TEAMMATE_ENCRYPTION_KEY_BASE64=$(openssl rand 32 | base64 -w0)" >> .env
```

### 问题：服务启动失败，提示"JWT secret must be changed"

**原因：** 使用了默认的 JWT Secret。
**解决：** 设置随机 JWT Secret。

```bash
echo "TEAMS_JWT_SECRET=$(openssl rand -hex 32)" >> .env
```

### 问题：CORS 错误（前端无法调用 API）

**原因：** `TEAMS_ALLOWED_ORIGINS` 未正确配置。
**解决：** 设置为前端的完整源（包括端口）。

```bash
TEAMS_ALLOWED_ORIGINS=https://app.your-domain.com
```

多个源用逗号分隔：

```bash
TEAMS_ALLOWED_ORIGINS=https://app.your-domain.com,https://admin.your-domain.com
```

### 问题：数据库连接被拒绝

**原因：** PostgreSQL 未就绪或密码不匹配。
**解决：** 检查 `TEAMS_DATABASE_URL` 中的用户名、密码和主机名。Docker Compose 环境中主机名为服务名 `postgres`。

### 问题：请求体过大（413）

**原因：** 默认限制 10MB。
**解决：** 增大 `TEAMS_MAX_BODY_SIZE`（单位字节）。

```bash
echo "TEAMS_MAX_BODY_SIZE=52428800" >> .env  # 50MB
```
