# clawside

`clawside` 是挂在 OpenClaw 外侧的 truth layer / sidecar。

它不是 OpenClaw runtime 本体，也不只等于一个 Telegram sender。这个仓库当前承载三类侧车能力：本地 sender、handoff/workflow orchestrator foundations，以及面向 OpenClaw 的 adapter / bridge 基础设施。

## 与 OpenClaw 的关系

- **OpenClaw** 负责会话、消息和 agent runtime
- **clawside** 负责仓库外侧的确定性数据面，例如 handoff/workflow truth、watch、repair、sender bridge
- 仓库会继续保留对上游 OpenClaw 的稳定兼容面，例如 `~/.openclaw/openclaw.json`、`--adapter openclaw`、`scripts/openclaw-dispatch`

## Today

当前仓库已经提供或部分提供这些能力：

- **Telegram sender**：本地 HTTP sender service，负责入队、鉴权、幂等和 worker 发送
- **orchestrator CLI / store / state machine / watch / repair foundations**：提供 handoff、event、workflow、watch、repair 的基础骨架
- **OpenClaw adapter foundations**：用于把调度和桥接动作接到现有 OpenClaw 兼容入口
- **A2A delivery bridge skill**：在官方 announce / nested 回传链路不稳定时，为主 agent 提供显式消息投递桥

这表示仓库今天已经不只是 sender，但也**还没有**完整交付成一套可直接安装即用的 MCP server + skill 产品面。

## Target

目标形态是把 `clawside` 作为 **MCP sidecar / truth layer** 接到 OpenClaw 外侧：

1. 先把 clawside 作为外部 MCP server 接入 OpenClaw
2. 普通用户优先通过 skill 发起和推进任务
3. skill 再调用底层 truth / orchestration tools

推荐理解为：

- **定位上**：MCP sidecar / truth layer
- **默认使用路径上**：先装 MCP，再用 skill

这里描述的是目标接入形态，不代表当前仓库已经完整提供 MCP server 与配套 skill 套件。

## 组件概览

- `cmd/config-builder/`：生成 sender 派生配置的 Go CLI
- `cmd/orchestrator/`：低层 orchestrator 调试 / 操作入口
- `internal/configbuilder/`：从 OpenClaw 源配置提取 sender 所需最小配置
- `internal/orchestrator/`：handoff、workflow、event、watch、repair、adapter 基础实现
- `internal/a2adelivery/`：A2A delivery bridge、轮询与编排逻辑
- `main.go` + `http_handler.go` + `worker.go`：sender 服务入口、HTTP API 和发送 worker
- `.claude/skills/openclaw-a2a-delivery/`：OpenClaw A2A delivery skill 定义

## 快速开始

### 1. 生成 sender 派生配置

```bash
export SENDER_AUTH_KEY='your-local-sender-key'
./scripts/config_builder.sh
```

默认会读取 `~/.openclaw/openclaw.json`，并在当前工作目录生成：

- `configs/config.toml`

如需指定输入文件，可以传：

```bash
./scripts/config_builder.sh --input /path/to/openclaw.json
```

说明：

- `scripts/config_builder.sh` 是稳定入口，内部调用 Go builder
- `configs/config.toml` 是派生文件，不手工维护
- `configs/config.toml` 包含 bot token 与本地 sender 鉴权 key，已加入 `.gitignore`
- builder 会把输出文件权限收紧为 `600`
- `sender_auth_key` 与 Telegram bot token 分离，仅用于本地 `/send` 鉴权

### 2. 启动 sender 服务

```bash
./scripts/start.sh
```

`scripts/start.sh` 会先检查 `configs/config.toml`；缺失时直接失败并提示先生成配置。

sender 默认监听：

```text
127.0.0.1:8787
```

这个服务默认只允许监听 loopback 地址，适合本机调用。

### 3. 发送一条消息

```bash
curl -X POST http://127.0.0.1:8787/send \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer your-local-sender-key' \
  -d '{
    "bot": "guardian",
    "chat_id": <chat-id>,
    "text": "这是一条测试消息",
    "idempotency_key": "idem-001"
  }'
```

成功时返回：

```json
{
  "job_id": 1,
  "status": "pending",
  "idempotency_key": "idem-001"
}
```

### 4. 查询 sender 状态

```bash
curl http://127.0.0.1:8787/healthz
curl http://127.0.0.1:8787/readyz
curl http://127.0.0.1:8787/stats
curl "http://127.0.0.1:8787/jobs?status=pending&limit=20"
curl http://127.0.0.1:8787/jobs/<job_id>
```

## 低层 CLI / 调试入口

如果你需要直接操作当前已存在的底层能力，可以使用这些入口：

### sender / config builder

```bash
go run ./cmd/config-builder --input ~/.openclaw/openclaw.json --output ./configs/config.toml
go run .
```

### orchestrator CLI

```bash
go run ./cmd/orchestrator handoff create --db ./sender.db --workflow-kind generic --sender agent:planner --receiver agent:writer --task-kind generic_task --intent "write summary"
go run ./cmd/orchestrator handoff list --db ./sender.db
go run ./cmd/orchestrator workflow list --db ./sender.db
go run ./cmd/orchestrator watch run --db ./sender.db
```

### OpenClaw A2A delivery skill

边界：

- 只支持 **主 agent 显式调用**
- 通过现有 sender backend 完成投递
- 不直接调用 Telegram API
- 不尝试修复官方 announce 链路，只做 sender bridge

相关文件：

- `.claude/skills/openclaw-a2a-delivery/SKILL.md`

## sender 权限与输入边界

sender 在 HTTP 入队前执行权限校验：

```text
chat_id ∈ global_allow_user_ids OR chat_id ∈ bot.allow_user_ids
```

也就是：

- 命中全局 allowlist，则当前 bot 可发送
- 未命中全局，但命中当前 bot 私有 allowlist，也可发送
- 两边都没命中，则请求会被拒绝，且不会入队

另外当前实现还做了这些输入收紧：

- `bot` 必须是服务端已配置的逻辑 bot 名
- `text` 必须是纯文本，不能为空
- `text` 超过 Telegram 单条文本限制（4096 字符）会在入队前直接拒绝
- `max_attempts` 必须在 `1..5` 范围内
