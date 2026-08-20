# AGENTS.md

Compact guide for AI coding agents. For full architecture details see `CLAUDE.md`.

## Bootstrap

```bash
docker compose -f product/docker-compose.yml up -d    # PG on :15432, Redis on :16379
source product/use-env.sh                              # MUST source — generates JWT secret + encryption key on first run
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
```

No `pnpm test` — there are no frontend tests.

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
- **Tailwind v4 CSS-first config:** no `tailwind.config.js`; theming via `data-theme` attribute + CSS custom properties
- **Frontend UI language:** all labels and user-facing text are zh-CN
- **Frontend state:** single flat Zustand store in `web/lib/store.ts` — no slices, no React Query/SWR
- **Frontend API:** plain `fetch` wrapper in `web/lib/api.ts`; auth via Bearer token from localStorage
- **Data mapping:** `web/lib/mappers.ts` transforms snake_case API → camelCase frontend types
- **Doc-code consistency:** reviewing docs against code — every inconsistency found must be recorded in `docs/实现与设计偏差记录.md` (format follows its header; fix the doc if it's the wrong side)

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
