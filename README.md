# Telegram Sender

一个最小可用的本地 Telegram sender service。

它提供 `POST /send` HTTP 接口，把消息写入 SQLite 队列，再由后台 worker 调用 Telegram Bot API 发送。调用方只需要投递任务，不直接接触 bot token。

## 快速开始

### 1. 生成 `config.toml`

```bash
./scripts/config_builder.sh
```

默认会读取 `~/.openclaw/openclaw.json`，在项目根目录生成 `./config.toml`。

如需指定输入文件，可以传：

```bash
./scripts/config_builder.sh --input /path/to/openclaw.json
```

说明：

- `config.toml` 是派生文件，不手工维护
- `config.toml` 包含 bot token，已加入 `.gitignore`
- 生成脚本会把 `config.toml` 权限收紧为 `600`

### 2. 启动服务

```bash
./scripts/start.sh
```

`scripts/start.sh` 会先检查 `./config.toml` 是否存在；缺失时直接失败并提示先生成配置。

sender 运行时只读取 `config.toml`，不再以 `BOT_TOKEN_*` 作为主配置入口。

默认监听：

```text
127.0.0.1:8787
```

这个服务默认只允许监听 loopback 地址，适合本机调用。

### 3. 发送一条消息

```bash
curl -X POST http://127.0.0.1:8787/send \
  -H 'Content-Type: application/json' \
  -d '{
    "bot": "guardian",
    "chat_id": <chat-id>,
    "text": "这是一条测试消息"
  }'
```

成功时返回：

```json
{"job_id":1,"status":"pending"}
```

看到 `job_id` 且 `status` 为 `pending`，说明任务已经成功进入本地队列。

## 权限规则

sender 在 HTTP 入队前执行权限校验：

```text
chat_id ∈ global_allow_user_ids OR chat_id ∈ bot.allow_user_ids
```

也就是：

- 命中全局 allowlist，则当前 bot 可发送
- 未命中全局，但命中当前 bot 私有 allowlist，也可发送
- 两边都没命中，则请求会被拒绝，且不会入队

## 相关文件

- `scripts/config_builder.py`：从 `openclaw.json` 生成 `config.toml`
- `scripts/config_builder.sh`：shell 包装脚本，生成并收紧配置文件权限
- `scripts/start.sh`：检查配置存在后启动 sender
- `config.go`：加载 `config.toml`
- `http_handler.go`：执行 bot/allowlist 校验
