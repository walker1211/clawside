# clawside

[入口页](./README.md) | [English](./README.en.md)

`clawside` 是挂在 OpenClaw 外侧的本地 MCP sidecar / truth layer。

它不是 OpenClaw runtime 本体，也不只等于一个 Telegram sender。这个仓库当前承载三类侧车能力：本地 sender、handoff/workflow orchestrator foundations，以及面向 OpenClaw 的 adapter / bridge 基础设施。

## 与 OpenClaw 的关系

- **OpenClaw** 负责会话、消息和 agent runtime
- **clawside** 负责仓库外侧的确定性数据面，例如 handoff/workflow truth、watch、repair、sender bridge
- 仓库保留对上游 OpenClaw 的稳定兼容面，例如 `~/.openclaw/openclaw.json`、OpenClaw MCP stdio server、A2A delivery bridge

## 当前能力

- **Telegram sender**：本地 HTTP sender service，负责入队、鉴权、幂等和 worker 发送
- **orchestrator CLI / store / state machine / watch / repair foundations**：提供 handoff、event、workflow、watch、repair 的基础骨架
- **OpenClaw adapter foundations**：用于把调度和桥接动作接到现有 OpenClaw 兼容入口
- **A2A delivery bridge skill**：在官方 announce / nested 回传链路不稳定时，为主 agent 提供显式消息投递桥
- **MCP + skill v1 surface**：让 OpenClaw 可以安装、注册并消费 handoff、workflow、watch、repair 和 A2A delivery 工具

当前版本已把最小可用 v1 收口为可安装、可注册、可验证的 MCP server + skill 产品套件。

## 组件概览

- `cmd/config-builder/`：生成 sender 派生配置的 Go CLI
- `cmd/orchestrator/`：低层 orchestrator 调试 / 操作入口
- `cmd/clawside-mcp/`：stdio MCP server 入口
- `cmd/openclaw-mcp-smoke/`：OpenClaw 消费 clawside MCP v1 surface 的本地 smoke verifier
- `cmd/openclaw-tool-results-extract/`：从 OpenClaw trajectory 提取 clawside tool structured result 的本地只读 CLI
- `cmd/openclaw-truth-plane-extract/`：从 OpenClaw trajectory 提取最小 truth-plane handoff/workflow/watch/ownership 验收结果的本地只读 CLI
- `cmd/openclaw-truth-plane-progression-extract/`：从 OpenClaw trajectory 提取完整 handoff progression 验收结果的本地只读 CLI
- `cmd/openclaw-truth-plane-mutation-extract/`：从 OpenClaw trajectory 提取 watch / ownership mutation 验收结果的本地只读 CLI
- `cmd/a2a-delivery/`：A2A delivery bridge CLI
- `internal/configbuilder/`：从 OpenClaw 源配置提取 sender 所需最小配置
- `internal/orchestrator/`：handoff、workflow、event、watch、repair、adapter 基础实现
- `internal/toolserver/`：MCP tool handler
- `internal/a2adelivery/`：A2A delivery bridge、轮询与编排逻辑
- `main.go` + `http_handler.go` + `worker.go`：sender 服务入口、HTTP API 和发送 worker
- `.claude/skills/openclaw-a2a-delivery/`：OpenClaw A2A delivery skill 定义

## 安装与配置

### 1. 生成 sender 派生配置

推荐从 OpenClaw 配置生成本地 sender 配置：

```bash
export SENDER_AUTH_KEY='your-local-sender-key'
./scripts/config_builder.sh
```

默认会读取 `~/.openclaw/openclaw.json`，并生成：

```text
configs/config.toml
```

如需指定输入文件：

```bash
./scripts/config_builder.sh --input /path/to/openclaw.json
```

说明：

- `scripts/config_builder.sh` 是稳定入口，内部调用 Go builder
- `configs/config.toml` 是派生文件，不手工维护
- `configs/config.toml` 包含 bot token 与本地 sender 鉴权 key，已加入 `.gitignore`
- builder 会把输出文件权限收紧为 `600`
- `sender_auth_key` 与 Telegram bot token 分离，仅用于本地 `/send` 鉴权

### 2. 查看手写配置模板

仓库提供非敏感模板：

```text
configs/config.example.toml
```

如果你不想从 OpenClaw 配置生成，也可以手动复制模板：

```bash
cp configs/config.example.toml configs/config.toml
```

然后填写：

- `sender_auth_key`：本地 sender 的 `/send` 鉴权 key，不能等于 Telegram bot token
- `telegram.global_allow_user_ids`：全局允许发送的 Telegram chat/user id
- `telegram.bots.<name>.token`：对应 bot 的 Telegram bot token
- `telegram.bots.<name>.allow_user_ids`：当前 bot 的私有 allowlist

## 常用脚本

```bash
./scripts/config_builder.sh
./build.sh
./start.sh
./stop.sh
./restart.sh
./scripts/start_mcp.sh --db ./sender.db
./scripts/extract_openclaw_tool_results.sh --events .openclaw/trajectory-exports/<export-dir>/events.jsonl
./scripts/extract_openclaw_truth_plane_results.sh --events .openclaw/trajectory-exports/<export-dir>/events.jsonl
./scripts/extract_openclaw_truth_plane_progression_results.sh --events .openclaw/trajectory-exports/<export-dir>/events.jsonl
./scripts/extract_openclaw_truth_plane_mutation_results.sh --events .openclaw/trajectory-exports/<export-dir>/events.jsonl
```

说明：

- `./build.sh`：构建根 sender 二进制 `./clawside`
- `./start.sh`：后台启动 sender 服务，写入 `logs/sender.pid` 和 `logs/sender.log`
- `./stop.sh`：停止由 `./start.sh` 记录的 sender 进程
- `./restart.sh`：依次执行 `./stop.sh` 和 `./start.sh`
- `./scripts/start.sh`：前台 `go run .` 启动，适合调试或不想构建二进制时使用
- `./scripts/start_mcp.sh`：启动 stdio MCP server，供 OpenClaw 注册使用
- `./scripts/extract_openclaw_tool_results.sh`：从 OpenClaw trajectory `events.jsonl` 提取 clawside sender tool structured result
- `./scripts/extract_openclaw_truth_plane_results.sh`：从 OpenClaw trajectory `events.jsonl` 提取最小 truth-plane 验收结果
- `./scripts/extract_openclaw_truth_plane_progression_results.sh`：从 OpenClaw trajectory `events.jsonl` 提取完整 progression 验收结果
- `./scripts/extract_openclaw_truth_plane_mutation_results.sh`：从 OpenClaw trajectory `events.jsonl` 提取 watch / ownership mutation 验收结果

`./start.sh` / `./stop.sh` / `./restart.sh` 只管理自己写入的 pidfile。如果你是手动前台运行 `./scripts/start.sh`，请手动停止该进程。

## 启动 sender 服务

```bash
./build.sh
./start.sh
```

sender 默认监听：

```text
127.0.0.1:8787
```

这个服务默认只允许监听 loopback 地址，适合本机调用。

## 发送一条消息

```bash
curl -X POST http://127.0.0.1:8787/send \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer your-local-sender-key' \
  -d '{
    "bot": "guardian",
    "chat_id": 123456789,
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

## 查询 sender 状态

```bash
curl http://127.0.0.1:8787/healthz
curl http://127.0.0.1:8787/readyz
curl http://127.0.0.1:8787/stats
curl "http://127.0.0.1:8787/jobs?status=pending&limit=20"
curl http://127.0.0.1:8787/jobs/<job_id>
```

## OpenClaw MCP / tool server

本节汇总 v1 tool contract、启动路径和调用边界。

启动 stdio MCP server：

```bash
go run ./cmd/clawside-mcp \
  --db ./sender.db \
  --sender-base-url http://127.0.0.1:8787 \
  --sender-auth-key "$SENDER_AUTH_KEY" \
  --target-agent-map "qa=guardian"
```

更稳定的本地启动方式是包装脚本：

```bash
./scripts/start_mcp.sh --db ./sender.db
```

`--target-agent-map` 和 `CLAWSIDE_TARGET_AGENT_BOT_MAP` 接受逗号分隔的 `target_agent=bot` 映射。`a2a_deliver` 调用方仍只传 `target_agent`，不能在请求里绕过路由策略直接指定任意 bot。语义化 A2A 重试幂等需要显式传稳定的 `idempotency_key`；省略时 bridge 会为每次投递生成唯一的 nonce-based key。

内置默认 target agent 映射包括 `main -> main`、`planner -> planner`、`engineer -> engineer`、`researcher -> researcher`、`archivist -> archivist`、`guardian -> guardian`、`closer -> closer`。其中 `main` 用于 OpenClaw router / main agent 场景；如果 `~/.openclaw/openclaw.json` 中配置了 `main -> default` Telegram route，config builder 会生成 `[telegram.bots.main]`，之后 `a2a_deliver` 可直接使用 `target_agent=main`。

当前 v1 暴露的 tools：

- `handoff_create`
- `handoff_get`
- `handoff_dispatch`
- `handoff_progress`
- `workflow_status`
- `workflow_list`
- `watch_list`
- `watch_run`
- `watch_update`
- `ownership_get`
- `ownership_update`
- `repair_list`
- `repair_invalidate_event`
- `repair_reopen_handoff`
- `repair_candidate_list`
- `divergence_list`
- `sender_health`
- `sender_ready`
- `sender_stats`
- `sender_job_list`
- `sender_job_get`
- `a2a_deliver`

推荐理解：

- `handoff_create`：创建一个新的 handoff / workflow 起点
- `handoff_get`：查询单个 handoff 的当前 truth 与 timeline
- `handoff_dispatch`：记录 handoff dispatch attempt 与 transport request，让纯 MCP 流程可以先 dispatch 再 receive
- `handoff_progress`：推进 handoff 协议动作，如 `receive` / `claim` / `start` / `submit` / `approve` / `complete`
- `workflow_status`：查询单个 workflow 聚合视图
- `workflow_list`：列出当前所有 workflow 及其 projected handoffs
- `watch_list`：列出单个 handoff 当前挂载的 watches
- `watch_run`：按给定 RFC3339 时间运行 due watch 检查
- `watch_update`：更新单个 watch 的 deadline、status 或 escalation policy
- `ownership_get`：查询单个 handoff 当前 ownership binding
- `ownership_update`：更新 handoff ownership 字段，并同步 ownership binding
- `repair_list`：列出 repair 记录，可按 handoff 过滤
- `repair_invalidate_event`：使已接受事件失效并重放 handoff truth
- `repair_reopen_handoff`：重新打开 terminal handoff 并重放 truth
- `repair_candidate_list`：列出单个 handoff 的 repair candidates
- `divergence_list`：列出单个 handoff 的 observer divergence hints
- `sender_health`：检查 sender 进程健康状态
- `sender_ready`：检查 sender 是否已准备好处理投递工作
- `sender_stats`：返回 sender 队列计数和 worker 时间字段
- `sender_job_list`：按状态和有界 limit 列出 sender jobs
- `sender_job_get`：返回单个 sender job 状态
- `a2a_deliver`：通过内置或配置的 mapping 把 `target_agent` 解析成 bot 后，经现有 sender bridge 做真实 outward delivery

边界：

- 这是一个最小可用 v1 tool surface，不是完整 truth-plane MCP 产品面
- repair backfill 和更深层 truth-plane 操作仍不属于 v1 MCP surface
- `a2a_deliver` 和 `sender_*` observability tools 依赖本地 sender sidecar 正常运行
- `sender_*` observability tools 只读，不暴露原始消息文本、raw idempotency key 或 Telegram bot token
- `handoff_*` / `workflow_*` / `watch_*` / `ownership_get` / `repair_*` / `divergence_list` 依赖 `--db` 指向同一个 sqlite truth store

如果要在 OpenClaw 中注册，核心是把它作为一个 stdio MCP server 注册，并让 OpenClaw 通过该 server 调用上述 tools。`command` 建议指向当前仓库的 `scripts/start_mcp.sh`。

## OpenClaw MCP smoke verifier

`openclaw-mcp-smoke` 是一个本地、可重复执行的 smoke verifier，用来验证 OpenClaw 消费 clawside MCP v1 surface 的基础链路。它会检查本地配置、sender `healthz` / `readyz` / `stats`、MCP tool list，并可选通过 MCP `a2a_deliver` 走一次真实的 `target_agent=main` 投递。

默认情况下它不会修改 OpenClaw / Claude 配置，不会直接调用 Telegram，也不会执行真实投递；它只会打印只读的 MCP 注册指引。敏感信息建议通过 `SENDER_AUTH_KEY` 环境变量传入，不要把 secret 粘贴到共享日志里。

基础检查：

```bash
SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh
```

如需机器可读输出：

```bash
SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh --json
```

如需让输出附带 OpenClaw 侧只读 tool call checklist，可传入 `--openclaw-tool-call-checklist`。它只说明应该在 OpenClaw runtime / session 中手动调用 `sender_health`、`sender_ready`、`sender_stats`，以及如何判断返回结果；不会代替 OpenClaw 执行或伪造这些调用：

```bash
SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh --openclaw-tool-call-checklist
```

在 OpenClaw runtime / session 中按 checklist 手动调用三个 tools 后，推荐从 OpenClaw trajectory 导出的 `events.jsonl` 提取原始 `structuredContent`，再用 `--openclaw-tool-results` 做只读验收。该路径不会执行或伪造 OpenClaw 调用，也不依赖 TG / dashboard 最终回复是否暴露原始 structured result：

```bash
openclaw sessions export-trajectory --agent main --session-key '<session-key>' --json

./scripts/extract_openclaw_tool_results.sh \
  --events .openclaw/trajectory-exports/<export-dir>/events.jsonl \
  --output /tmp/openclaw-tool-results.json

SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --openclaw-tool-results /tmp/openclaw-tool-results.json
```

### 最小 truth-plane 工具链验收

在 OpenClaw web / TG 中发起一次真实工具链调用，验证 OpenClaw 可以消费 handoff truth、workflow status、watch list 和 ownership binding：

```text
请通过已注册的 clawside MCP tools 创建一条测试 handoff，并依次查询它的 handoff truth、workflow status、watch list、ownership binding。

请按顺序调用：
1. handoff_create
2. handoff_get
3. workflow_status
4. watch_list
5. ownership_get

创建参数请使用：
workflow_kind=manual_openclaw_truth_plane_smoke
sender=agent:main
receiver=agent:planner
task_kind=truth_plane_smoke
intent=verify OpenClaw can consume clawside truth-plane tools

调用完成后，请输出 handoff_id 和 workflow_id。
```

调用完成后，导出 trajectory 并提取 truth-plane 验收结果，再交给 smoke verifier 做只读验收：

```bash
openclaw sessions export-trajectory --agent main --session-key '<session-key>' --json

./scripts/extract_openclaw_truth_plane_results.sh \
  --events .openclaw/trajectory-exports/<export-dir>/events.jsonl \
  --output /tmp/openclaw-truth-plane-results.json

SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --openclaw-truth-plane-results /tmp/openclaw-truth-plane-results.json
```

如需验收 OpenClaw 真实推进 handoff 状态，可让 main agent 创建 handoff 并按协议推进到 `completed`，再从 trajectory 提取并验收结果：

```text
请通过已注册的 clawside MCP tools 创建一条测试 handoff，并将它完整推进到 completed，然后查询最终 handoff truth 和 workflow status。

请按顺序调用：
1. handoff_create
2. handoff_dispatch
3. handoff_progress action=receive
4. handoff_progress action=claim
5. handoff_progress action=start
6. handoff_progress action=checkpoint
7. handoff_progress action=complete
8. handoff_get
9. workflow_status

创建参数请使用：
workflow_kind=manual_openclaw_truth_plane_progression_smoke
sender=agent:main
receiver=agent:planner
task_kind=truth_plane_progression_smoke
required_for_workflow_completion=true
intent=verify OpenClaw can progress clawside truth-plane handoff state

dispatch 参数请使用：
handoff_id=<created handoff_id>
adapter=manual
target=agent:planner

progress actor 请使用 receiver：agent:planner。

调用完成后，请输出 handoff_id、workflow_id、最终 handoff state 和最终 workflow status。
```

```bash
openclaw sessions export-trajectory --agent main --session-key '<session-key>' --json

./scripts/extract_openclaw_truth_plane_progression_results.sh \
  --events .openclaw/trajectory-exports/<export-dir>/events.jsonl \
  --output /tmp/openclaw-truth-plane-progression-results.json

SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --openclaw-truth-plane-progression-results /tmp/openclaw-truth-plane-progression-results.json
```

### Stage 2 / 阶段 2 truth-plane mutation 验收

如需验收 OpenClaw 真实 mutation watch / ownership 状态，可让 main agent 创建 handoff 后更新 watch 与 ownership，再从 trajectory 提取并验收结果：

```text
请通过已注册的 clawside MCP tools 创建一条测试 handoff，然后对同一个 handoff 的 watch 和 ownership 做 mutation，并查询最终结果。

请按顺序调用：
1. handoff_create
2. watch_list
3. watch_update
4. ownership_update
5. watch_list
6. ownership_get

创建参数请使用：
workflow_kind=manual_openclaw_truth_plane_mutation_smoke
sender=agent:main
receiver=agent:planner
task_kind=truth_plane_mutation_smoke
intent=verify OpenClaw can mutate clawside truth-plane watch and ownership state

第一次 watch_list 请使用创建得到的 handoff_id。

watch_update 请使用第一次 watch_list 返回的第一个 watch id，并设置：
status=disabled
deadline_at=2026-05-07T12:30:00Z
escalation_policy=manual-smoke-escalation

ownership_update 请使用创建得到的 handoff_id，并设置：
current_owner=agent:operator
lease_holder=agent:operator
reviewer_actor=agent:reviewer
escalation_owner=user:ops
fallback_owner=agent:planner
leased_at=2026-05-07T12:00:00Z
lease_expires_at=2026-05-07T12:30:00Z

第二次 watch_list 请再次使用同一个 handoff_id。
ownership_get 请使用同一个 handoff_id。

调用完成后，请输出 handoff_id、workflow_id、更新后的 watch_id、watch status、watch deadline_at、watch escalation_policy、current_owner、lease_holder、reviewer_actor、escalation_owner、fallback_owner、leased_at、lease_expires_at。
```

```bash
openclaw sessions export-trajectory --agent main --session-key '<session-key>' --json

./scripts/extract_openclaw_truth_plane_mutation_results.sh \
  --events .openclaw/trajectory-exports/<export-dir>/events.jsonl \
  --output /tmp/openclaw-truth-plane-mutation-results.json

SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --openclaw-truth-plane-mutation-results /tmp/openclaw-truth-plane-mutation-results.json
```

如需只读校验本机 MCP 注册配置，可显式传入 JSON 配置路径；该检查只读取文件并对照当前 registration guidance，不会写入或修补配置：

```bash
SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh --registration-config /path/to/mcp.json
```

如需验证真实投递，必须显式传 `--deliver-main` 和 `--chat-id`，投递会经已配置的 sender/main bot 路径完成：

```bash
SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh --deliver-main --chat-id <telegram_chat_id>
```

包装脚本默认使用 `configs/config.toml`、`sender.db`、`http://127.0.0.1:8787` 和 `scripts/start_mcp.sh`。如需自定义 MCP 启动命令，可使用：

```bash
./scripts/verify_openclaw_mcp.sh --mcp-command ./scripts/start_mcp.sh
```

## A2A delivery bridge CLI

sender sidecar 启动后，可以直接用本地 bridge 入口发起一次定向投递：

```bash
go run ./cmd/a2a-delivery \
  --target-agent planner \
  --text "请直接把结果发给我" \
  --chat-id 123456789 \
  --sender-auth-key "$SENDER_AUTH_KEY"
```

如果不显式传 `--chat-id`，也可以传会话上下文字段让 bridge 解析目标用户：

```bash
go run ./cmd/a2a-delivery \
  --target-agent engineer \
  --text "请把当前状态同步给当前会话用户" \
  --delivery-context-to 123456789 \
  --sender-auth-key "$SENDER_AUTH_KEY"
```

边界：

- 只支持主 agent 显式调用
- 通过现有 sender backend 完成投递
- 不直接调用 Telegram API
- 不尝试修复官方 announce 链路，只做 sender bridge

## 低层 CLI / 调试入口

```bash
go run ./cmd/config-builder --input ~/.openclaw/openclaw.json --output ./configs/config.toml
go run .
go run ./cmd/orchestrator handoff create --db ./sender.db --workflow-kind generic --sender agent:planner --receiver agent:writer --task-kind generic_task --intent "write summary"
go run ./cmd/orchestrator handoff list --db ./sender.db
go run ./cmd/orchestrator workflow list --db ./sender.db
go run ./cmd/orchestrator watch run --db ./sender.db
```

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
