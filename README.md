# Telegram Sender

一个最小可用的本地 Telegram sender service。

它提供 `POST /send` HTTP 接口，把消息写入 SQLite 队列，再由后台 worker 调用 Telegram Bot API 发送。调用方只需要投递任务，不直接接触
bot token。

## 快速开始

### 1. 生成派生配置

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

- `scripts/config_builder.sh` 是稳定入口，内部已切换为 Go builder
- `configs/config.toml` 是派生文件，不手工维护
- `configs/config.toml` 包含 bot token 与本地 sender 鉴权 key，已加入 `.gitignore`
- builder 会把输出文件权限收紧为 `600`
- `sender_auth_key` 与 Telegram bot token 分离，仅用于本地 `/send` 鉴权

### 2. 启动服务

```bash
./scripts/start.sh
```

`scripts/start.sh` 会先检查：

- `configs/config.toml`

缺失时直接失败并提示先生成配置。

sender 运行时只读取 `configs/config.toml`，不再以 `BOT_TOKEN_*` 作为主配置入口。

默认监听：

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

说明：

- `Authorization: Bearer <sender_auth_key>` 是必填的本地鉴权头
- `idempotency_key` 用于防止同一业务请求重复入队
- 重复使用同一个 `idempotency_key` 时，会返回已有 job，而不是重复创建新任务

### 4. 查询服务与任务状态

健康检查：

```bash
curl http://127.0.0.1:8787/healthz
```

返回示例：

```json
{
  "status": "ok"
}
```

查询任务状态：

```bash
curl http://127.0.0.1:8787/jobs/<job_id>
```

返回字段：

```json
{
  "job_id": 1,
  "status": "pending",
  "attempt_count": 0,
  "last_error": "",
  "created_at": "2026-03-17T10:00:00Z",
  "updated_at": "2026-03-17T10:00:00Z",
  "sent_at": null
}
```

## 权限与输入边界

sender 在 HTTP 入队前执行权限校验：

```text
chat_id ∈ global_allow_user_ids OR chat_id ∈ bot.allow_user_ids
```

也就是：

- 命中全局 allowlist，则当前 bot 可发送
- 未命中全局，但命中当前 bot 私有 allowlist，也可发送
- 两边都没命中，则请求会被拒绝，且不会入队

另外当前 MVP 还做了这些输入收紧：

- `bot` 必须是服务端已配置的逻辑 bot 名
- `text` 必须是纯文本，不能为空
- `text` 超过 Telegram 单条文本限制（4096 字符）会在入队前直接拒绝
- `max_attempts` 必须在 `1..5` 范围内

## OpenClaw A2A delivery skill

当前仓库还包含一个 **OpenClaw A2A delivery skill**，用于在官方 announce / nested 回传链路不稳定时，给主 agent
提供一条显式、可观测的消息投递桥。

边界：

- 只支持 **主 agent 显式调用**
- 通过现有 sender backend 完成投递
- 不直接调用 Telegram API
- 不尝试修复官方 announce 链路，只做 sender bridge

相关 skill 文件：

- `.claude/skills/openclaw-a2a-delivery/SKILL.md`

## 相关文件

- `cmd/config-builder/main.go`：Go builder CLI 入口
- `internal/configbuilder/`：Go builder 核心逻辑
- `internal/a2adelivery/`：A2A delivery bridge、轮询与编排逻辑
- `scripts/config_builder.sh`：稳定 shell 入口，调用 Go builder
- `scripts/start.sh`：检查 `configs/config.toml` 后启动 sender
- `.claude/skills/openclaw-a2a-delivery/SKILL.md`：A2A delivery skill 定义
- `config.go`：加载派生配置
- `http_handler.go`：执行鉴权、bot/allowlist 校验、幂等复用和状态查询
- `store.go`：持久化 jobs、幂等键与 sending lease 恢复
- `worker.go`：消费队列、发送 Telegram 消息并处理重试
