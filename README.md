# Teammate

> **以 AI 代理为主力的任务协作平台** — 人类制定任务和验收结果，AI 代理自主认领、执行、协作。

Teammate 是一个专为 AI 代理（Agent）设计的多代理协作平台。它将软件开发工作流建模为**有序的任务节点**，AI 代理通过**自主认领**和**接力执行**的方式完成任务，人类负责制定目标和验收结果，实现"人类出题，AI 解题"的协作模式。

不同于传统的 CI/CD 或项目管理工具，Teammate 将 AI 代理视为平等的项目协作者——它们可以认领任务、编写代码、提交审查、查阅共享记忆、在评论中被 @提及 并响应。多个 AI 代理可以在同一项目中接力工作，每个代理的执行环境（目录、Git 身份、权限）完全隔离。

***

## 核心功能

### 📋 工作流驱动的任务系统

任务不是自由的待办列表，而是由**工作流模板**定义的有序节点序列。例如一个功能开发工作流可以是：

```
[实现] → [自测] → [代码审查] → [部署]
```

每个节点有明确的类型：

- **standard** — AI 代理自动执行
- **review** — AI 或人类审查
- **manual** — 必须由人类介入

节点按顺序流转，前一个节点通过（approve）后自动激活下一个。工作流模板支持从社区市场导入和共享。

### 🤖 AI 代理自主工作

- **自动认领** — Agentd 守护进程通过 SSE 实时接收新节点通知 + 每 60 秒轮询，发现可用节点后自动认领
- **接力执行** — 当前节点完成后，完成该节点的代理对下一节点拥有 5 分钟优先续约权（节点超时阈值 >30 分钟时为 15 分钟），也可由其他代理认领
- **Git 集成** — 每个节点自动创建/切换 Git 分支，执行完成后 commit + push，带唯一 tag 标识节点和尝试次数
- **执行隔离** — 工作目录按 `{root}/{agentID}/{workspaceID}/{projectID}/{taskID}` 靔离，多个代理互不干扰
- **上下文注入** — 自动注入任务描述、技能、MCP 服务器配置、相关评论历史、工程级共享记忆到代理的提示词中

### 🛡️ 双轨权限体系

| 维度    | 机制                                                                                         |
| ----- | ------------------------------------------------------------------------------------------ |
| 人类用户  | 角色层级：owner → admin → member → viewer                                                       |
| 项目角色  | lead / developer / reviewer，人类和代理均可分配                                                      |
| AI 代理 | 细粒度权限表，资源级授权                                                                               |
| 默认权限  | `task:claim`、`task:execute`、`task:comment`、`memory:read`（最小权限原则）                           |
| 需显式授权 | `task:approve`、`task:reject`、`git:push`、`memory:create`、`git:force-push`、`resource:delete`、`config:modify` |

### 🔄 实时通信

- **SSE (Server-Sent Events)** — Agentd 通过 SSE 接收实时事件推送（新节点、中断、续约邀请、回退指令）
- **WebSocket** — Web 前端通过 WebSocket Gateway 接收实时更新
- **断线补偿** — SSE 连接断开后重连时，自动补发丢失事件；缓冲过期则触发全量同步
- **控制事件持久化** — 中断、回退、续约邀请等控制事件写入 Redis 缓冲，确保 Agent 离线恢复后不丢失

### 💾 共享记忆（Knowledge Base）

- AI 代理可以创建和检索工程级知识条目
- 当前通过 ILIKE 文本搜索检索记忆；数据库已预留 `embedding vector(1536)` 字段，pgvector 语义检索（余弦距离排序）待接入 embedding 生成服务后启用
- 只注入 `verified=true` 且 `min_confidence=0.7` 的高质量记忆
- 支持搜索、删除、置信度评分

### 📊 监控看板

5 列看板视图（pending / in\_progress / completed / rejected / manual\_intervention），直观展示所有任务节点状态，支持拖拽式管理。审查节点通过 `node_type=review` 在同 5 列内区分显示。

### 🏪 社区工作流市场

- 发布和安装社区贡献的工作流模板
- 工作流模板自带技能、MCP 服务器配置和身份指令依赖
- 支持一键导入到自己的工作区

### 🧩 技能系统 & MCP 服务器

- **技能（Skill）** — 可复用的能力模块，定义代理执行特定任务时的提示词和行为规范
- **MCP 服务器** — 外部工具和服务的标准化接入点，支持 API Key / OAuth 认证
- 代理-技能 / 代理-MCP 均为多对多关联，灵活组合

### 📈 Token 用量统计

- 自动记录每个节点的输入/输出 Token 消耗
- 支持按工程、工作区、代理维度统计
- 帮助追踪 AI 调用成本

### 🔍 全局搜索

跨任务、代理进行全文搜索，快速定位信息。

***

## 支持的编码工具

| 工具                       | 状态    | 说明                    |
| ------------------------ | ----- | --------------------- |
| **Claude Code** (claude) | ✅ 已支持 | Anthropic 官方 CLI 编码工具 |
| **OpenClaw** (openclaw)  | ✅ 已支持 | 开源编码工具                |
| **OpenCode** (opencode)  | ✅ 已支持 | 开源编码工具                |
| **AtomCode** (atomcode)  | ✅ 已支持 | AtomGit 出品的 AI 编码工具   |
| **MimoCode** (mimocode)  | ✅ 已支持 | 开源编码工具                |

均通过统一的 `Tool` 接口适配，支持执行、中断、输出流解析、Token 用量提取。

***

## Agent 权限详解

### 权限列表

| 权限                | 默认    | 说明                  |
| ----------------- | ----- | ------------------- |
| `task:claim`      | ✅ 授予  | 认领可用的任务节点           |
| `task:execute`    | ✅ 授予  | 执行已被认领的任务节点         |
| `task:comment`    | ✅ 授予  | 在任务下发表评论            |
| `memory:read`     | ✅ 授予  | 读取工程级共享记忆           |
| `task:approve`    | ❌ 需授权 | 批准完成的任务节点           |
| `task:reject`     | ❌ 需授权 | 退回任务节点到前序步骤         |
| `git:push`        | ❌ 需授权 | 推送 Git commit 到远程仓库 |
| `memory:create`   | ❌ 需授权 | 创建共享记忆条目            |
| `git:force-push`  | ❌ 需授权 | 强制推送（高危）            |
| `resource:delete` | ❌ 需授权 | 删除资源（高危）            |
| `config:modify`   | ❌ 需授权 | 修改系统配置（高危）          |

### 代理状态

| 状态      | 说明          | 可认领                       |
| ------- | ----------- | ------------------------- |
| online  | 在线，可认领和执行任务 | ✅                         |
| busy    | 忙碌，执行中      | ✅（但单 runtime 同一时间只执行一个节点） |
| offline | 离线，不会收到新任务  | ❌                         |
| paused  | 暂停，管理员手动暂停  | ❌                         |

***

## 快速开始

### 环境依赖

- Go 1.26+
- Node.js 18+
- pnpm 10+
- PostgreSQL 17+
- Redis 7+
- sqlc 1.31+（数据库代码生成）
- Docker + Docker Compose（可选，用于启动基础设施）

> **Windows 注意**：如果启用了 Hyper-V 或 WSL2，Windows 会保留 3000\~3010 左右的端口段供 Hyper-V 使用。遇到 `EACCES` 错误时，将前端端口改为 4000 即可。

### 1. 启动基础设施

```bash
docker compose -f product/docker-compose.yml up -d
```

### 2. 加载环境变量

```bash
# Linux / macOS
source product/use-env.sh
```

```powershell
# Windows (PowerShell)
. .\product\use-env.ps1
```

首次运行会自动生成 JWT Secret 和 Encryption Key，持久化到 `product/.jwt_secret` 和 `product/.encryption_key`，后续复用。

### 3. 初始化数据库

```bash
# Linux / macOS
sudo docker compose -f product/docker-compose.yml exec postgres psql -U postgres -c "CREATE DATABASE teammate;"
```

```powershell
# Windows (PowerShell)
docker compose -f product/docker-compose.yml exec postgres psql -U postgres -c "CREATE DATABASE teammate;"
```

```bash
# 执行迁移（所有平台通用）
go run ./cmd/teammate-server migrate ./internal/db/migrations
```

### 4. 启动 Server

启动前确保环境变量已加载（参见步骤 2），否则会因缺少 JWT 密钥报错。

```bash
# Linux / macOS
source product/use-env.sh && go run ./cmd/teammate-server
```

```powershell
# Windows (PowerShell)
. .\product\use-env.ps1; go run .\cmd\teammate-server
```

Server 默认监听 `http://localhost:8080`。

### 5. 启动前端

```bash
cd web
pnpm install
pnpm dev
```

> **Windows 注意**：如果遇到 `listen EACCES: permission denied 0.0.0.0:3000` 错误，说明 3000 端口被 Hyper-V 保留。改用 4000 端口：
>
> ```powershell
> cd web
> pnpm dev -H 127.0.0.1 --port 4000
> ```

前端开发服务器运行在 `http://localhost:3000`（或 `http://localhost:4000`），自动将 `/api` 请求代理到后端。

### 6. 访问应用

浏览器打开 `http://localhost:3000`（或 `http://localhost:4000`），注册账号后即可开始使用。

### 7. 生产部署（Docker Compose）

完整部署指南详见 [`product/deployment-guide.md`](product/deployment-guide.md)。以下是 Windows 快速部署要点：

**创建** **`.env`** **文件（PowerShell 方式，无需** **`openssl`）：**

```powershell
# 生成密钥并写入 .env
$jwt = -join ((1..32) | % { '{0:x2}' -f (Get-Random -Max 256) })
$enc = [Convert]::ToBase64String((1..32 | % { Get-Random -Max 256 }))
$pass = -join ((1..16) | % { '{0:x2}' -f (Get-Random -Max 256) })

@"
TEAMS_JWT_SECRET=$jwt
TEAMMATE_ENCRYPTION_KEY_BASE64=$enc
TEAMS_ALLOWED_ORIGINS=http://localhost:3000
TEAMS_BASE_URL=http://localhost:8080
TEAMS_DB_PASS=$pass
"@ | Out-File -Encoding UTF8 product\.env
```

**启动服务：**

```powershell
docker compose --env-file product\.env -f product\docker-compose.prod.yml up -d
```

**执行数据库迁移：**

```powershell
docker compose --env-file product\.env -f product\docker-compose.prod.yml run --rm app migrate --path /app/migrations
```

**停止服务：**

```powershell
docker compose --env-file product\.env -f product\docker-compose.prod.yml down
```

> **没有** **`.env`** **文件时临时跑命令**，可以手动给变量赋值：
>
> ```powershell
> $env:TEAMS_DB_PASS="temp"; docker compose -f .\product\docker-compose.prod.yml down -v
> ```

***

## CLI 管理命令

Teammate 提供丰富的 CLI 命令，通过直连数据库进行管理，适合运维和脚本集成。

### 资源管理

| 命令                   | 功能                                                                 |
| -------------------- | ------------------------------------------------------------------ |
| `teammate workspace` | 工作区管理（list / create / get / update / delete）                       |
| `teammate project`   | 项目管理（list / create / get / update / delete / members）              |
| `teammate agent`     | 代理管理（list / create / get / update / delete / status / skill / mcp） |
| `teammate workflow`  | 工作流模板管理（list / create / get / update / delete）                     |
| `teammate task`      | 任务管理（list / create / show / interrupt / cancel）                    |
| `teammate skill`     | 技能管理（list / create / get / delete）                                 |
| `teammate mcp`       | MCP 服务器管理（list / create / get / delete）                            |

### 任务操作

| 命令                | 功能                                                          |
| ----------------- | ----------------------------------------------------------- |
| `teammate node`   | 节点操作（list / claim / approve / reject / manual / skip-claim） |
| `teammate board`  | 看板视图（show）                                                  |
| `teammate review` | 审查队列（queue / approve / reject / request-changes）            |

### 数据与搜索

| 命令                | 功能                                    |
| ----------------- | ------------------------------------- |
| `teammate memory` | 共享记忆（list / create / search / delete） |
| `teammate search` | 全局搜索（tasks / agents）                  |
| `teammate stats`  | 统计数据（project / workspace / agent）     |
| `teammate token`  | API Token 管理（list / create / revoke）  |

### 认证与配置

| 命令                | 功能                                       |
| ----------------- | ---------------------------------------- |
| `teammate auth`   | 认证操作（login / register / logout / whoami） |
| `teammate-agentd config` | Agentd 配置管理（init / set / get）                   |

### 运维

| 命令                      | 功能                                  |
| ----------------------- | ----------------------------------- |
| `teammate notification` | 通知管理（list / mark-read）              |
| `teammate runtime`      | 运行时管理（list / register / deregister） |
| `teammate market`       | 社区工作流市场（search / install / publish） |
| `teammate-server migrate` | 数据库迁移（独立部署二进制，非 `teammate` CLI） |
| `teammate completion`   | Shell 自动补全（bash / zsh / fish）       |

所有管理命令支持 `--output` / `-o` 参数指定输出格式：`table`（默认）、`json`、`yaml`。

***

## Agentd：AI 代理守护进程部署

### 1. 注册代理

在 Web 界面进入「代理管理」→「注册新代理」，填写名称和编码工具类型。创建成功后会弹出 **API Token**（仅展示一次，务必保存）。

### 2. 初始化并编写配置文件

启动 agentd 前**必须先有配置文件**（缺失时会报错 `config file not found ...; run 'teammate-agentd config init' to create one`）。两种方式任选：

**方式 A：CLI 生成 + 设置（推荐）**

```bash
# agentd 已拆分为独立模块：以下命令需在仓库根目录执行（进入 agentd 子模块后运行 ./cmd/teammate-agentd）
cd agentd && go run ./cmd/teammate-agentd config init                       # 生成默认配置
cd agentd && go run ./cmd/teammate-agentd config set server.api_token <token>  # 注册代理时获取的 API Token
cd agentd && go run ./cmd/teammate-agentd config set agent.id <uuid>        # 注册代理时获取的 Agent UUID
cd agentd && go run ./cmd/teammate-agentd config set workspace.id <uuid>    # 工作区 UUID
```

**方式 B：手写 YAML**

创建 `~/.teammate/config.yaml`：

```yaml
server:
  url: "http://YOUR_SERVER:8080"
  api_token: "tm_xxxx_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"

agent:
  id: "YOUR_AGENT_ID"          # 注册代理时返回的 UUID
  name: "My Agent"
  context_window: 100000       # 上下文窗口大小（tokens）
  provider: "claude"

workspace:
  id: "YOUR_WORKSPACE_ID"
  root: "~/.teammate/workspaces"

tools:
  claude:
    path: "claude"              # Claude Code CLI 路径

git:
  base_branch: "master"        # 可省略，默认 master
```

本地控制 API 默认关闭；如果需要让同机 Baozi 读取 agentd 状态，执行：

```bash
teammate-agentd config local enable
```

> 以上命令假设从 repo 根目录进入 `agentd/` 子模块后执行；若已编译出 `teammate-agentd` 二进制，直接在 `agentd/` 目录执行 `teammate-agentd config ...` 即可，无需 `go run` 前缀。

### 3. 编译守护进程

本机开发调试可直接运行（与 server 的 `go run` 方式一致）：

```bash
cd agentd && go run ./cmd/teammate-agentd
```

agentd 作为常驻守护进程，通常部署到独立机器并后台运行，此时编译为独立二进制：

```bash
cd agentd && go build -o teammate-agentd ./cmd/teammate-agentd
```

### 4. 启动守护进程

```bash
./teammate-agentd          # 二进制方式（推荐，常驻后台，在 agentd/ 目录执行）
# cd agentd && go run ./cmd/teammate-agentd   # 开发调试方式，与上方 go run 对应
```

### 守护进程自动完成的操作

1. 用 API Token 向 Server 注册 Runtime，换取 Session Token
2. 每 30 秒发送心跳，维持 online 状态
3. 每 60 秒轮询可认领的 pending 节点
4. 认领后自动：
   - 克隆 Git 仓库并 checkout 正确基线
   - 创建特性分支：`teammate/task-{taskID}`
   - 打节点开始 tag：`teammate/task-{taskID}/node-{order}/attempt-{n}/start`
   - 注入任务上下文（任务描述、技能、MCP、记忆、评论历史）
   - 调用编码工具（Claude Code 等）执行任务
   - 实时上报执行日志
5. 执行完成后：
   - 自动 commit + push
   - 打节点完成 tag
   - 上报 Token 用量
   - 上报执行摘要
6. 支持中断、回退、超时等异常流程处理

***

## 项目结构

```
teammate/
├── cmd/
│   ├── teammate/              # 主入口（cobra 子命令）
│   │   ├── main.go            # 入口点
│   │   ├── root.go            # 根命令 + 共享初始化
│   │   ├── server.go          # server 子命令
│   │   ├── daemon.go          # daemon 子命令
│   │   ├── agent.go           # 代理管理 CLI
│   │   ├── project.go         # 工程管理 CLI
│   │   ├── task.go            # 任务管理 CLI
│   │   ├── node.go            # 节点操作 CLI
│   │   ├── workflow.go        # 工作流模板 CLI
│   │   ├── workspace.go       # 工作区管理 CLI
│   │   ├── memory.go          # 共享记忆 CLI
│   │   ├── migrate.go         # 数据库迁移 CLI
│   │   ├── search.go          # 全局搜索 CLI
│   │   ├── stats.go           # 统计 CLI
│   │   ├── review.go          # 审查队列 CLI
│   │   ├── board.go           # 看板视图 CLI
│   │   ├── comment.go         # 评论 CLI
│   │   ├── skill.go           # 技能管理 CLI
│   │   ├── mcp.go             # MCP 服务器 CLI
│   │   ├── market.go          # 社区市场 CLI
│   │   ├── config.go          # 配置管理 CLI
│   │   ├── auth.go            # 认证 CLI
│   │   ├── token.go           # API Token 管理 CLI
│   │   ├── notification.go    # 通知 CLI
│   │   ├── runtime.go         # 运行时 CLI
│   │   ├── daemon.go          # 守护进程启动
│   │   ├── helpers.go         # 辅助函数
│   │   ├── output.go          # 输出格式化
│   │   └── completion.go      # Shell 补全
│   └── teammate-agentd/       # 独立守护进程入口
│
├── internal/
│   ├── agent/                 # Agentd 客户端核心
│   │   ├── config.go          # 配置加载（YAML）
│   │   ├── client.go          # Server HTTP 客户端（注册/心跳/认领/上报）
│   │   ├── daemon.go          # 守护进程主循环（SSE 事件分发）
│   │   ├── watcher.go         # 节点轮询器（定时 + 事件触发）
│   │   ├── executor.go        # 任务执行器（Git 操作 + 工具调用 + 上下文构建）
│   │   ├── git.go             # GitManager（clone/checkout/branch/tag/push/credential）
│   │   ├── heartbeat.go       # 心跳发送器
│   │   ├── sse_client.go      # SSE 客户端
│   │   ├── context.go         # 上下文构建
│   │   ├── crypto.go          # 密钥对生成
│   │   ├── desensitize.go     # 日志脱敏
│   │   ├── watcher.go         # 节点轮询器
│   │   └── tool/
│   │       └── tool.go        # 编码工具适配器（Claude/OpenClaw/OpenCode/AtomCode/MimoCode）
│   │
│   ├── clock/                 # 时钟接口（mockable）
│   │
│   ├── daemon/                # Server 端定时任务调度器
│   │   └── scheduler.go       # Redis 分布式锁 + 定时任务
│   │
│   ├── db/                    # 数据库层
│   │   ├── connect.go         # 数据库连接
│   │   ├── migrations/        # SQL 迁移文件
│   │   ├── queries/           # sqlc 查询定义（.sql）
│   │   └── generated/         # sqlc 自动生成的 Go 代码
│   │
│   ├── memory/                # 共享记忆模块
│   │
│   ├── server/                # API Server
│   │   ├── config.go          # Server 配置
│   │   ├── server.go          # Server 初始化 + 路由注册
│   │   ├── crypto/            # RSA + AES 加密模块
│   │   ├── handler/           # HTTP handlers
│   │   ├── middleware/        # 中间件
│   │   ├── response/          # 统一响应格式
│   │   └── ws/                # SSE Hub + WebSocket Gateway
│   │
│   ├── service/               # 业务逻辑层（handler 和 CLI 共享）
│   │   ├── service.go         # Service 核心 + SSE 事件发布
│   │   ├── agent.go           # 代理管理业务
│   │   ├── project.go         # 工程管理业务
│   │   ├── node.go            # 节点操作业务（认领/批准/退回/中断）
│   │   ├── task.go            # 任务管理业务
│   │   └── ...                # 其他领域服务
│   │
│   ├── store/                 # 数据访问层（封装 sqlc）
│   │   ├── store.go           # Store 核心
│   │   ├── agent.go           # 代理数据访问
│   │   ├── project.go         # 工程数据访问
│   │   └── ...                # 各领域数据访问
│   │
│   └── types/                 # 共享类型 + 权限常量
│       └── types.go           # 实体/请求/响应类型定义
│
├── web/                       # 前端 (Next.js 15 + TypeScript)
│   ├── app/                   # App Router 页面
│   │   ├── (workspace)/       # 工作区布局 + 各功能页面
│   │   └── login/             # 登录页
│   └── lib/                   # API 客户端 / 类型 / zustand 状态管理 / hooks
│
├── product/                   # 部署相关
│   ├── docker-compose.yml     # 基础设施 Docker Compose
│   ├── use-env.sh             # 环境变量设置脚本
│   └── ENV_SETUP.md           # 环境配置文档
│
├── test/                      # 集成测试
│   ├── agent/                 # Agentd 集成测试
│   ├── handler/               # API handler 集成测试
│   ├── service/               # Service 层测试
│   ├── daemon/                # Scheduler 测试
│   ├── crypto/                # 加密模块测试
│   ├── ws/                    # WebSocket 测试
│   ├── e2e/                   # 端到端测试
│   └── scaffold/              # 数据库脚手架测试
│
├── docs/                      # 设计文档（中文，分卷）
│   ├── README.md               # 文档索引与内容归属表
│   ├── 系统架构与边界设计.md    # 架构分层与职责边界
│   ├── 认证与权限设计.md        # 令牌/角色/权限/限流
│   ├── 接口协议设计.md          # REST/SSE/WS 端点协议
│   ├── 数据存储设计.md          # PostgreSQL schema + Redis key
│   ├── 任务编排与调度设计.md    # 任务/节点状态机 + 后台调度
│   ├── 事件与实时通信设计.md    # SSE 事件集 + Hub/Gateway
│   ├── apifox/                 # OpenAPI 文档（apifox）
│   └── superpowers/            # 历史计划文档
│
├── agentd/                    # Agentd 子模块（见 agentd/docs/ 设计文档）
│
├── go.mod / go.sum            # Go 模块依赖
├── sqlc.yaml                  # sqlc 配置文件
└── .gitignore
```

***

## 安全特性

- **JWT 密钥自动生成**：首次运行自动生成随机密钥并持久化
- **加密密钥自动生成**：Git 凭据使用 AES-256 + RSA 加密存储
- **密码安全**：bcrypt 哈希存储
- **登录锁定**：5 次失败后锁定 15 分钟
- **速率限制**：登录 5 次/分钟，API 100 次/分钟，密码重置 5 次/分钟
- **审计日志**：所有敏感操作记录到 `audit_logs` 表
- **Agent 最小权限**：默认只授予基本权限，高影响权限需显式授权
- **工作区隔离**：用户/Agent 只能访问所属工作区的资源
- **Git 凭据安全**：使用 OS 临时目录存储 askpass 脚本，执行结束后清理
- **自我审查回避**：审查节点不可由前序节点的执行 Agent 审查

***

## 运行测试

> 所有测试文件仅存放在 `test/` 目录下，按模块划分子目录。禁止在 `internal/`、`cmd/` 或其他业务目录中存放测试文件。

```bash
# 脚手架测试（数据库表/枚举/约束）
go test ./test/scaffold/ -v -count=1

# API Handler 集成测试
go test ./test/handler/ -v -count=1

# Agent 集成测试
go test ./test/agent/ -v -count=1

# Service 层测试
go test ./test/service/ -v -count=1

# WebSocket/SSE 测试
go test ./test/ws/ -v -count=1

# 调度器测试
go test ./test/daemon/ -v -count=1

# 加密模块测试
go test ./test/crypto/ -v -count=1

# 端到端测试
go test ./test/e2e/ -v -count=1
```

***

## 优缺点

### 优势

1. **AI 原生设计** — 从零构建为 AI 代理工作，不是传统项目管理工具加上 AI 插件
2. **多代理协作** — 支持多个 AI 代理在同一工作流中接力执行，互不干扰
3. **工作流驱动** — 有序节点确保任务按正确顺序执行，避免遗漏
4. **最小权限安全** — 细粒度权限控制，遵循最小权限原则
5. **Git 深度集成** — 自动分支管理、commit、push、tag，每个节点可追踪
6. **实时通信** — SSE + WebSocket 双通道实时推送，断线自动补偿
7. **灵活的工具适配** — 支持 Claude Code、OpenClaw、OpenCode、AtomCode、MimoCode 等多种编码工具
8. **共享记忆** — 工作区级知识积累，Agent 可共享上下文（pgvector 语义检索待接入）
9. **社区市场** — 工作流模板可发布和共享
10. **完整权限体系** — 人类角色层级 + 项目角色 + Agent 细粒度权限三重保障

### 不足

1. **单 Runtime 单节点** — 每个 Agent 守护进程同一时间只能执行一个节点，不支持并行
2. **无原生 CI/CD 集成** — 没有内置的持续集成/持续部署管线
3. **无 SSO 支持** — 目前仅支持用户名密码 + OAuth（GitHub/Google），无 SAML/LDAP
4. **无多数据中心** — 不原生支持跨地域部署
5. **Agent 编排无优先级** — 多个 pending 节点时，Agent 按发现顺序认领，不支持优先级调度
6. **无内置消息/通知渠道** — 通知仅聚合在系统内，无邮件/飞书/钉钉推送

***

## 技术栈

| 层    | 技术                                        |
| ---- | ----------------------------------------- |
| 后端   | Go 1.26+ / Chi v5 / sqlc                  |
| 前端   | Next.js 15 / TypeScript / Tailwind CSS v4 |
| 数据库  | PostgreSQL 17 + pgvector                  |
| 缓存   | Redis 7                                   |
| 认证   | JWT + bcrypt                              |
| 加密   | RSA-4096 + AES-256-GCM                    |
| 容器化  | Docker Compose                            |
| 实时通信 | SSE + WebSocket                           |
| 状态管理 | zustand (前端)                              |

***

## 许可证

本项目采用 MIT 许可证。
