# AGENTS.md

Compact guide for AI coding agents. For full architecture details see `CLAUDE.md`.

## Bootstrap

```bash
docker compose -f product/docker-compose.yml up -d    # PG on :15432, Redis on :16379
TEAMMATE_DEV=true go run ./cmd/teammate-server         # dev mode: auto dev JWT secret & encryption key
PGPASSWORD=teammate psql -h localhost -p 15432 -U postgres -d postgres -c "CREATE DATABASE teammate;"
go run ./cmd/teammate-server migrate ./internal/db/migrations
```

## Build & Run

```bash
go build ./cmd/teammate                        # 管理 CLI（纯 HTTP）
go build -o teammate-server ./cmd/teammate-server  # server 部署二进制（启动服务/迁移）
cd agentd && go build -o teammate-agentd ./cmd/teammate-agentd  # agent daemon（独立模块 agentd/）
go run ./cmd/teammate-server                   # API on :8080
```

## Regenerate DB code

```bash
sqlc generate   # after editing internal/db/queries/*.sql or internal/db/migrations/*.sql
```

**sqlc 安装：** 确保 PATH 中包含 `sqlc`（推荐 v1.31+）：
- macOS: `brew install sqlc`
- Linux: `go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1`
- 安装后确认 `sqlc` 在 PATH 中（`~/go/bin` 可能需要加入 PATH）
- 或参考 [sqlc 官方文档](https://docs.sqlc.dev/en/latest/overview/install.html)

CI 会执行 `sqlc generate` 后检查 `git diff --exit-code internal/db/generated/`，确保生成代码与查询文件一致。

> **注意：** 修改 `internal/db/queries/*.sql` 或 `internal/db/migrations/*.sql` 后务必运行 `sqlc generate` 并提交生成结果，保持 `internal/db/generated/` 与查询文件一致。

## Frontend (run from `web/`)

```bash
pnpm install && pnpm dev    # dev server on :3000, proxies /api/* → localhost:8080
pnpm lint                   # ESLint (next lint)
pnpm build                  # production build
pnpm test                   # vitest (lib/**/*.test.ts)
```

## Tests

Tests are **not** co-located with source. They live exclusively in `test/`. Run from project root:

```bash
go test ./test/handler/ -v -count=1 -run TestName   # single test
go test ./test/architecture/ -v -count=1             # architecture boundary tests
go test ./test/handler/ -v -count=1                  # API handler tests
go test ./test/service/ -v -count=1                  # service layer tests
go test ./test/store/ -v -count=1                    # store layer tests
go test ./test/scheduler/ -v -count=1                # scheduler (daemon) tests
go test ./test/crypto/ -v -count=1                   # encryption tests
go test ./test/middleware/ -v -count=1               # middleware tests
go test ./test/ws/ -v -count=1                       # WebSocket/SSE tests
go test ./test/e2e/ -v -count=1                      # end-to-end
```

All tests require Docker containers (Postgres + Redis) to be running.

## Architecture quick-ref

Four-layer: `Handler → Service → Store → sqlc-generated DB code`

- **Handlers** (`internal/server/handler/`) — each has `Routes() chi.Router`, extracts auth from context, returns JSON via `response.JSON()`
- **Service** (`internal/service/`) — `Service` struct holds `*store.Store`, `*ws.Hub`, `*redis.Client`, `*sql.DB`. Sub-services are per-request wrappers
- **Store** (`internal/store/`) — wraps sqlc. Transactions: `s.DB.BeginTx()` + `s.Q.WithTx(tx)` + `defer tx.Rollback()`
- **Generated** (`internal/db/generated/`) — never edit; regenerate with `sqlc generate`
- **Types** (`internal/types/`) — all shared domain models, DTOs, enums, error codes, permissions

DI wiring: `db.Connect()` → `store.New(db)` → `service.New(db, hub, redis)` → handlers receive `*service.Service`

## Key conventions agents miss

- **Error wrapping:** always `fmt.Errorf("context: %w", err)` — no bare returns
- **Workspace isolation:** every resource access must verify caller's `WorkspaceID` from auth claims
- **Dual access control:** humans use role levels (owner>admin>member>viewer), agents use permission strings from `agent_permissions` table. Unified via `RequireAccessWithChecker`
- **Env var prefixes:** `TEAMS_` for server config, `TEAMMATE_` for agent config
- **Frontend path alias:** `@/*` maps to `web/*` root (not `web/src/*`)
- **Tailwind v4 CSS-first config:** no `tailwind.config.js`; theming via `data-theme` attribute + CSS custom properties. Themes are pluggable modules under `web/lib/themes/<id>/` (metadata in `index.ts`, vars in `theme.css`); register a new theme by adding one line to `web/lib/themes/registry.ts`. Components use semantic utilities (`bg-surface`, `text-fg-muted`, `border-line`, `bg-primary/15`) — never raw palette classes
- **Frontend UI language:** all labels and user-facing text are zh-CN
- **Frontend state:** single flat Zustand store in `web/lib/store.ts` — no slices, no React Query/SWR
- **Frontend API:** plain `fetch` wrapper in `web/lib/api.ts`; auth via Bearer token from localStorage
- **Data mapping:** `web/lib/mappers.ts` transforms snake_case API → camelCase frontend types
- **注释只描述当前行为，不记录修改历史**——注释里禁止出现"修改记录 / change log / Added in vX / renamed from Y / 旧逻辑是 Z"之类的历史叙述；代码演进历史由 git 提交记录承载，注释只写代码现在是什么、为什么这样设计
- **不做向后兼容**——接口/函数/配置破坏性变更时直接替换并同步更新所有调用方，禁止保留旧接口别名、deprecated 包装层、兼容分支或双路径共存；不写 `deprecated` 标记后长期保留旧实现
- **Doc-code consistency:** reviewing docs against code — every inconsistency found must be recorded in `docs/实现与设计偏差记录.md` (format follows its header; fix the doc if it's the wrong side)

## 双平台支持规则（Windows / Linux）

本项目须在 Windows 与 Linux 上均可开发、构建、运行。以下规则来自实测踩坑，新增代码/脚本/文档必须遵守：

**路径与脚本**
- Go 中路径拼接一律用 `filepath` 系列（`filepath.Join` / `filepath.Dir` 等），禁止手拼 `"/"` 或 `"\\"`；作为 CLI 参数传递的路径（如 migrate 的 `./internal/db/migrations`）保持相对路径形式，两平台通用
- 文档与命令示例按 bash 语法书写，路径用正斜杠；Git Bash 下 `"D:\dir\"` 的尾部反斜杠会转义引号导致命令断裂

**端口与连接**
- 前端 dev 脚本固定 :3000（`web/package.json` 的 `dev`）；Windows 上 3000 可能被 Hyper-V 保留（报 `EACCES`）或与 Gitea 冲突，此时改用 `pnpm dev --port 4000`
- 数据库/Redis 连接串主机名写 `127.0.0.1` 而非 `localhost`——Windows 上 `localhost` 可能优先解析到 IPv6 `::1`，若该端口被 WSL 端口转发（`wslrelay.exe`）占用，会连到错误实例（15432/16379 均已踩坑）

**构建**
- `pnpm build`（web/）因 `output: "standalone"` 在 Windows 需要符号链接特权，trace 复制阶段报 `EPERM: symlink`；**生产产物构建统一在 Linux/CI 执行**（或开启 Windows 开发者模式）。Windows 本地构建以 `Compiled successfully` + 17/17 页面生成为通过标准，末尾的 trace EPERM 警告可忽略
- Go 二进制与 `sqlc`、`go test` 在两平台均可本地执行，无平台限制

**行尾**
- 仓库文件按 LF 提交；Windows 开发机 `core.autocrlf=true` 出现 "LF will be replaced by CRLF" 提示属正常现象，勿通过改文件行尾来消除提示，避免整文件无意义 diff

## sqlc type overrides

Configured in `sqlc.yaml` — these affect generated code:
- `uuid` → `github.com/google/uuid.UUID`
- `timestamptz` → `time.Time`
- `jsonb` → `encoding/json.RawMessage`
- `text[]` → `[]string`
- `uuid[]` → `[]uuid.UUID`

## Database

- Single consolidated migration: `internal/db/migrations/001_init.up.sql` (32 tables)
- 17 query files in `internal/db/queries/`
- pgvector `vector(1536)` for semantic search on memories
- Optimistic concurrency via `version` column on task_nodes
- **FK 策略：能不用外键就不用**——新表/新列默认不建 `REFERENCES` 约束。外键会在后续表结构更新（改列类型、删列、大表 DDL）时带来额外迁移成本（需先删约束再重建、可能锁表）。完整性由应用层保证（Service 校验 + 显式删除顺序）；仅对必须由数据库强制的级联清理（如工作区/工程删除清理子资源）才保留外键

## Agent daemon (separate binary)

Built from `agentd/cmd/teammate-agentd/`（独立 Go 模块 `agentd/`）. Config at `~/.teammate/config.yaml`. Key behaviors:
- RSA keypair registration → session token exchange → SSE + 30s heartbeat
- Per-task: branch `teammate/task-{taskID}`, tags `teammate/task-{taskID}/node-{order}/attempt-{attempt}/start`
- Credentials via temporary `GIT_ASKPASS` scripts (auto-cleaned)
