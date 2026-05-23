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
- **MCP + skill v1 surface**：让 OpenClaw 可以安装、注册并消费 handoff、workflow、watch、repair、agent coordination 和 A2A delivery 工具
- **Agent coordination policy**：基于 registry 的 work projection，支持 heartbeat 默认值、stale/offline owner 信号和 expired lease 建议
- **A2A compatibility endpoint**：实验性 Agent Card + JSON-RPC 入口，用于 coordination 查询、标准 task status 查询、受控幂等 inbound task creation 和只读 task event streaming

当前版本已把最小可用 v1 收口为可安装、可注册、可验证的 MCP server + skill 产品套件。

## 最小使用路径

1. 生成本地 sender 配置：

```bash
cp .example.env .env
# 编辑 .env，设置 SENDER_AUTH_KEY 为本地随机 key
./scripts/config_builder.sh
```

2. 构建并启动 sender：

```bash
./build.sh
./start.sh
```

3. 在 OpenClaw 中注册 clawside MCP server：

```text
command: <repo-root>/scripts/start_mcp.sh
args: --db <repo-root>/sender.db
env: SENDER_AUTH_KEY=<local-sender-key>
```

4. 做本地只读验收：

```bash
SENDER_AUTH_KEY=<local-sender-key> ./scripts/verify_openclaw_mcp.sh
```

5. 先生成 coordination evidence summary，再从 OpenClaw trajectory 生成并复验 release evidence bundle：

```bash
go run ./cmd/orchestrator workflow evidence \
  --db <sqlite-db> \
  --workflow-id <workflow-id> \
  --include-agents \
  > <coordination-evidence-summary.json>

go run ./cmd/openclaw-release-evidence-bundle \
  --output-dir <bundle-dir> \
  --events .openclaw/trajectory-exports/<export-dir>/events.jsonl \
  --coordination-evidence-summary <coordination-evidence-summary.json> \
  --verify
```

6. tag 前复验；准备发布时去掉 `--verify-only`，脚本会创建并推送 tag：

```bash
./scripts/tag-release.sh --verify-only --evidence-bundle <bundle-dir> vX.Y.Z
```

## 组件概览

- `cmd/config-builder/`：生成 sender 派生配置的 Go CLI
- `cmd/orchestrator/`：低层 orchestrator 调试 / 操作入口
- `cmd/clawside-mcp/`：stdio MCP server 入口
- `cmd/clawside-a2a/`：实验性的 A2A compatibility HTTP endpoint，支持受控查询、inbound task creation 和只读 task events
- `cmd/openclaw-mcp-smoke/`：OpenClaw 消费 clawside MCP v1 surface 的本地 smoke verifier
- `cmd/openclaw-dispatch/`：把 `handoff_dispatch adapter=openclaw` 请求适配到 OpenClaw-compatible CLI command 的本地 helper
- `cmd/openclaw-tool-results-extract/`：从 OpenClaw trajectory 提取 clawside tool structured result 的本地只读 CLI
- `cmd/openclaw-truth-plane-extract/`：从 OpenClaw trajectory 提取最小 truth-plane handoff/workflow/watch/ownership 验收结果的本地只读 CLI
- `cmd/openclaw-truth-plane-progression-extract/`：从 OpenClaw trajectory 提取完整 handoff progression 验收结果的本地只读 CLI
- `cmd/openclaw-truth-plane-mutation-extract/`：从 OpenClaw trajectory 提取 watch / ownership mutation 验收结果的本地只读 CLI
- `cmd/openclaw-truth-plane-repair-extract/`：从 OpenClaw trajectory 提取 repair invalidate/backfill replay 验收结果的本地只读 CLI
- `cmd/openclaw-truth-plane-reopen-extract/`：从 OpenClaw trajectory 提取 divergence/candidate/reopen handoff 验收结果的本地只读 CLI
- `cmd/openclaw-truth-plane-divergence-extract/`：从 OpenClaw trajectory 提取 divergence/candidate/E2E completed truth 验收结果的本地只读 CLI
- `cmd/openclaw-truth-plane-continuity-extract/`：从 OpenClaw trajectory 提取 truth-plane continuity reopen 后继续推进验收结果的本地只读 CLI
- `cmd/openclaw-truth-plane-delivery-extract/`：从 OpenClaw trajectory 提取 handoff + A2A delivery sender job 验收结果的本地只读 CLI
- `cmd/a2a-delivery/`：A2A delivery bridge CLI
- `internal/configbuilder/`：从 OpenClaw 源配置提取 sender 所需最小配置
- `internal/orchestrator/`：handoff、workflow、event、watch、repair、adapter 基础实现
- `internal/toolserver/`：MCP tool handler
- `internal/a2aserver/`：Agent Card 与受控 JSON-RPC compatibility server
- `internal/a2adelivery/`：A2A delivery bridge、轮询与编排逻辑
- `main.go` + `http_handler.go` + `worker.go`：sender 服务入口、HTTP API 和发送 worker
- `.claude/skills/openclaw-a2a-delivery/`：OpenClaw A2A delivery skill 定义

## 安装与配置

### 1. 生成 sender 派生配置

推荐从 OpenClaw 配置生成本地 sender 配置：

```bash
cp .example.env .env
# 编辑 .env，将 SENDER_AUTH_KEY 改成本地随机 key
./scripts/config_builder.sh
```

本地脚本会自动读取 `.env`，但不会覆盖显式传入的环境变量或命令行参数。默认会读取 `~/.openclaw/openclaw.json`，并生成：

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
./scripts/secret-scan.sh
./scripts/ci-local.sh clean
./scripts/install-hooks.sh
./scripts/tag-release.sh --help
./build.sh
./start.sh
./stop.sh
./restart.sh
./scripts/start_mcp.sh --db ./sender.db
./scripts/extract_openclaw_tool_results.sh --events .openclaw/trajectory-exports/<export-dir>/events.jsonl
./scripts/extract_openclaw_truth_plane_results.sh --events .openclaw/trajectory-exports/<export-dir>/events.jsonl
./scripts/extract_openclaw_truth_plane_progression_results.sh --events .openclaw/trajectory-exports/<export-dir>/events.jsonl
./scripts/extract_openclaw_truth_plane_mutation_results.sh --events .openclaw/trajectory-exports/<export-dir>/events.jsonl
./scripts/extract_openclaw_truth_plane_repair_results.sh --events .openclaw/trajectory-exports/<export-dir>/events.jsonl
./scripts/extract_openclaw_truth_plane_reopen_results.sh --events .openclaw/trajectory-exports/<export-dir>/events.jsonl
./scripts/extract_openclaw_truth_plane_divergence_results.sh --events .openclaw/trajectory-exports/<export-dir>/events.jsonl
./scripts/extract_openclaw_truth_plane_continuity_results.sh --events .openclaw/trajectory-exports/<export-dir>/events.jsonl
./scripts/extract_openclaw_truth_plane_delivery_results.sh --events .openclaw/trajectory-exports/<export-dir>/events.jsonl
```

说明：

- `./build.sh`：构建根 sender 二进制 `./clawside`
- `./scripts/secret-scan.sh`：扫描 tracked 文件和可选 git history 中的高风险 secret，输出会 redaction，不打印完整 secret
- `./scripts/ci-local.sh clean`：从 tracked 文件构造临时干净目录，运行 secret scan、gofmt、vet、test 和 build
- `./scripts/install-hooks.sh`：安装本仓库 `.git/hooks/pre-push`，push 前默认执行 clean 本地 CI
- `./scripts/tag-release.sh --help`：查看本地 release tag 保护脚本用法；实际创建 tag 前请先阅读 Stage 8 本地发布保护说明
- `./start.sh`：后台启动 sender 服务，写入 `logs/sender.pid` 和 `logs/sender.log`
- `./stop.sh`：停止由 `./start.sh` 记录的 sender 进程
- `./restart.sh`：依次执行 `./stop.sh` 和 `./start.sh`
- `./scripts/start.sh`：前台 `go run .` 启动，适合调试或不想构建二进制时使用
- `./scripts/start_mcp.sh`：启动 stdio MCP server，供 OpenClaw 注册使用
- `./scripts/extract_openclaw_tool_results.sh`：从 OpenClaw trajectory `events.jsonl` 提取 clawside sender tool structured result
- `./scripts/extract_openclaw_truth_plane_results.sh`：从 OpenClaw trajectory `events.jsonl` 提取最小 truth-plane 验收结果
- `./scripts/extract_openclaw_truth_plane_progression_results.sh`：从 OpenClaw trajectory `events.jsonl` 提取完整 progression 验收结果
- `./scripts/extract_openclaw_truth_plane_mutation_results.sh`：从 OpenClaw trajectory `events.jsonl` 提取 watch / ownership mutation 验收结果
- `./scripts/extract_openclaw_truth_plane_repair_results.sh`：从 OpenClaw trajectory `events.jsonl` 提取 repair invalidate/backfill replay 验收结果
- `./scripts/extract_openclaw_truth_plane_reopen_results.sh`：从 OpenClaw trajectory `events.jsonl` 提取 divergence/candidate/reopen handoff 验收结果
- `./scripts/extract_openclaw_truth_plane_divergence_results.sh`：从 OpenClaw trajectory `events.jsonl` 提取 divergence/candidate/E2E completed truth 验收结果
- `./scripts/extract_openclaw_truth_plane_continuity_results.sh`：从 OpenClaw trajectory `events.jsonl` 提取 truth-plane continuity reopen 后继续推进验收结果
- `./scripts/extract_openclaw_truth_plane_delivery_results.sh`：从 OpenClaw trajectory `events.jsonl` 提取 handoff + A2A delivery sender job 验收结果

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

`/jobs` 和 MCP `sender_job_list` 使用 sender 内部状态名：`pending`、`sending`、`retry`、`failed`、`sent`。A2A bridge 输出中的 `retrying` 是投递结果语义，会对应 sender 的 `pending` / `sending` / `retry` 队列状态。

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
- `agent_register`
- `agent_list`
- `next_work`
- `blocked_work`
- `watch_list`
- `watch_run`
- `watch_update`
- `ownership_get`
- `ownership_update`
- `repair_list`
- `repair_invalidate_event`
- `repair_backfill_event`
- `repair_reopen_handoff`
- `repair_candidate_list`
- `divergence_record`
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
- `agent_register`：注册或更新 agent 的能力和 heartbeat；省略 heartbeat 时默认使用服务端时间
- `agent_list`：按 capability、project ref、task kind 或 status 列出已注册 agents
- `next_work`：列出可执行 handoffs，并包含非阻断的 liveness / lease warnings 和 suggestions
- `blocked_work`：列出带有 dependency、watch、reviewer、liveness、lease 原因与建议的 handoffs
- `watch_list`：列出单个 handoff 当前挂载的 watches
- `watch_run`：按给定 RFC3339 时间运行 due watch 检查
- `watch_update`：更新单个 watch 的 deadline、status 或 escalation policy
- `ownership_get`：查询单个 handoff 当前 ownership binding
- `ownership_update`：更新 handoff ownership 字段，并同步 ownership binding
- `repair_list`：列出 repair 记录，可按 handoff 过滤
- `repair_invalidate_event`：使已接受事件失效并重放 handoff truth
- `repair_backfill_event`：补录 accepted event 并重放 handoff truth
- `repair_reopen_handoff`：重新打开 terminal handoff 并重放 truth
- `repair_candidate_list`：列出单个 handoff 的 repair candidates
- `divergence_record`：记录单个 handoff 的 observer divergence signal
- `divergence_list`：列出单个 handoff 的 observer divergence hints
- `sender_health`：检查 sender 进程健康状态
- `sender_ready`：检查 sender 是否已准备好处理投递工作
- `sender_stats`：返回 sender 队列计数和 worker 时间字段
- `sender_job_list`：按状态和有界 limit 列出 sender jobs
- `sender_job_get`：返回单个 sender job 状态
- `a2a_deliver`：通过内置或配置的 mapping 把 `target_agent` 解析成 bot 后，经现有 sender bridge 做真实 outward delivery

边界：

- 这是一个最小可用 v1 tool surface，不是完整 truth-plane MCP 产品面
- invalidate/backfill/reopen repair 之外的更深层 truth-plane 操作仍不属于 v1 MCP surface
- `a2a_deliver` 和 `sender_*` observability tools 依赖本地 sender sidecar 正常运行
- `sender_*` observability tools 只读，不暴露原始消息文本、raw idempotency key 或 Telegram bot token
- `handoff_*` / `workflow_*` / `agent_*` / `*_work` / `watch_*` / `ownership_get` / `repair_*` / `divergence_list` 依赖 `--db` 指向同一个 sqlite truth store
- Agent liveness 和 lease policy 只作为 projection signal：不会启动 worker、执行命令、清空 owner、抢占 lease，也不会自动改写 handoff lifecycle state

如果要在 OpenClaw 中注册，核心是把它作为一个 stdio MCP server 注册，并让 OpenClaw 通过该 server 调用上述 tools。`command` 建议指向当前仓库的 `scripts/start_mcp.sh`。

## A2A compatibility endpoint

`clawside-a2a` 是实验性的兼容入口，用于 A2A-style discovery、coordination 查询、标准 task status 查询、受控 inbound task creation 和只读 task event streaming。它暴露 Agent Card、小型 JSON-RPC allowlist，以及由同一份 sqlite truth store 支撑的 SSE task stream。

用独立的 A2A auth key 启动：

```bash
CLAWSIDE_A2A_AUTH_KEY=<local-a2a-key> \
  go run ./cmd/clawside-a2a --db ./sender.db --addr 127.0.0.1:8789
```

查看命令帮助：

```bash
go run ./cmd/clawside-a2a --help
```

外部 client 建议按这个顺序接入：

1. 读取公开 Agent Card。它的 `metadata` 会声明 endpoint hints、稳定 method matrix、SSE 指引和安全边界。

   ```bash
   curl -sS http://127.0.0.1:8789/.well-known/agent-card.json
   ```

2. 通过 `/a2a/rpc` 携带 bearer auth 调用 JSON-RPC method：

   ```bash
   curl -sS http://127.0.0.1:8789/a2a/rpc \
     -H "Authorization: Bearer $CLAWSIDE_A2A_AUTH_KEY" \
     -H "Content-Type: application/json" \
     -d '{"jsonrpc":"2.0","id":"1","method":"clawside.workflow.list","params":{}}'
   ```

3. 用 `tasks/get` 读取某个 Clawside handoff 的标准风格 task status：

   ```bash
   curl -sS http://127.0.0.1:8789/a2a/rpc \
     -H "Authorization: Bearer $CLAWSIDE_A2A_AUTH_KEY" \
     -H "Content-Type: application/json" \
     -d '{"jsonrpc":"2.0","id":"1","method":"tasks/get","params":{"id":"hf_example","historyLength":0}}'
   ```

4. 订阅某个 Clawside handoff 的只读 task projection events：

   ```bash
   curl -N http://127.0.0.1:8789/a2a/tasks/hf_example/events?historyLength=1 \
     -H "Authorization: Bearer $CLAWSIDE_A2A_AUTH_KEY" \
     -H "Accept: text/event-stream"
   ```

5. 断线重连时，先调用 `tasks/get` 刷新当前 task snapshot，再重新订阅 SSE stream。stream 会发送 `retry: 3000` 和当前 truth-plane projection 的 `id:` cursor，但不保证基于 `Last-Event-ID` 回放历史事件。

创建一条受控 inbound task。该调用会创建一个 root workflow / handoff，并返回之后可由 `tasks/get` 读取的同一 task view：

```bash
curl -sS http://127.0.0.1:8789/a2a/rpc \
  -H "Authorization: Bearer $CLAWSIDE_A2A_AUTH_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": "1",
    "method": "clawside.task.create",
    "params": {
      "idempotency_key": "external-system-123",
      "intent": "Review downstream API compatibility",
      "receiver": {"id": "writer"},
      "project_ref": "project://downstream-api",
      "artifact_refs": [
        {"uri": "https://example.invalid/specs/api.md", "type": "spec", "checksum": "sha256:abc123"}
      ]
    }
  }'
```

取消一条受控 inbound task。该调用只会把对应 Clawside handoff 在 truth-plane 中标记为 failed；不会停止进程、session、worker 或 sender delivery：

```bash
curl -sS http://127.0.0.1:8789/a2a/rpc \
  -H "Authorization: Bearer $CLAWSIDE_A2A_AUTH_KEY" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":"1","method":"tasks/cancel","params":{"id":"hf_example"}}'
```

Method matrix：

| Method | Transport | Mode | 说明 |
| --- | --- | --- | --- |
| `clawside.workflow.list` | JSON-RPC `/a2a/rpc` | read | 列出 workflow 和 projected handoffs。 |
| `clawside.workflow.status` | JSON-RPC `/a2a/rpc` | read | 读取单个 workflow 和 projected handoffs。 |
| `clawside.handoff.get` | JSON-RPC `/a2a/rpc` | read | 读取 handoff 当前 truth 和 timeline。 |
| `clawside.agent.list` | JSON-RPC `/a2a/rpc` | read | 用安全过滤条件列出已注册 agents。 |
| `clawside.work.next` | JSON-RPC `/a2a/rpc` | read | 查询某个 agent/filter 当前可执行的 handoffs。 |
| `clawside.work.blocked` | JSON-RPC `/a2a/rpc` | read | 查询 blocked handoffs、原因和建议。 |
| `clawside.task.create` | JSON-RPC `/a2a/rpc` | controlled write | 幂等创建一条 inbound workflow/handoff，不执行 runtime 或 delivery。 |
| `tasks/cancel` | JSON-RPC `/a2a/rpc` | controlled write | 只把对应 Clawside handoff 在 truth-plane 中标记为 failed；不停止进程、session、worker 或 sender delivery。 |
| `tasks/get` | JSON-RPC `/a2a/rpc` | read | 用 Clawside handoff id 返回 A2A-style task projection。 |
| `tasks/events` | SSE `/a2a/tasks/{handoffID}/events` | stream | 只通过 path-based stream 暴露，不是 JSON-RPC method。 |

JSON-RPC 契约：unsupported method 返回 `-32601`；invalid 或 unsafe params 返回 `-32602`；parse/invalid request 使用 JSON-RPC 标准错误码；缺失或错误 bearer auth 在 dispatch 前返回 HTTP `401/403`。错误 payload 保持稳定，不回显 command、args、本地路径、private prompt、token、stdout/stderr、sender job 或 delivery job。

边界：这个 endpoint 只允许受控 truth-plane mutation（`clawside.task.create` 和 `tasks/cancel`），以及只读 task event streaming。`tasks/cancel` 只会把对应 handoff 标记为 failed；不会停止 runtime 进程、managed session、worker、sender delivery 或 Telegram delivery。不实现完整 Google A2A message runtime、raw `handoff_create`、`message/send`、push notification、sandbox、OpenClaw / Claude managed session、command / args / 本地路径 / runtime session / private prompt / worker launch 参数、sender delivery 或动态 mutating JSON-RPC mapping。

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

如需通过 `handoff_dispatch adapter=openclaw` 跑一次真实 OpenClaw dispatch smoke，请把 MCP server 指向本地 `openclaw-dispatch` helper，并选择一个你的 OpenClaw 配置中真实存在的 agent。该命令会执行一次真实的 `openclaw agent` run，可能消耗模型额度并写入本机 OpenClaw session 状态：

```bash
SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --openclaw-dispatch-smoke \
  --openclaw-target agent:main \
  --openclaw-command go \
  --openclaw-arg run \
  --openclaw-arg ./cmd/openclaw-dispatch \
  --openclaw-arg --mode \
  --openclaw-arg agent \
  --openclaw-arg --timeout \
  --openclaw-arg 300s \
  --openclaw-arg --openclaw-command \
  --openclaw-arg openclaw
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

### Stage 3 / 阶段 3 truth-plane repair invalidate/backfill 验收

如需验收 OpenClaw 真实 repair replay 能力，可让 main agent 创建 handoff、dispatch、receive，然后 invalidate 这次 receive 对应的 accepted event，再 backfill 一条等价的 accepted receive event，最后从 trajectory 提取并验收两条 repair 记录与最终 replayed truth：

```text
请通过已注册的 clawside MCP tools 创建一条测试 handoff，dispatch 后执行 receive，invalidate 这次 receive 对应的 event，再 backfill 一条 replacement received event，并查询 repair 与最终 handoff truth。

请按顺序调用：
1. handoff_create
2. handoff_dispatch
3. handoff_progress action=receive
4. repair_invalidate_event
5. repair_backfill_event
6. repair_list
7. handoff_get

创建参数请使用：
workflow_kind=manual_openclaw_truth_plane_repair_smoke
sender=agent:main
receiver=agent:planner
task_kind=truth_plane_repair_smoke
intent=verify OpenClaw can invalidate and backfill a clawside handoff event and observe replayed truth

dispatch 参数请使用：
handoff_id=<created handoff_id>
adapter=manual
target=agent:planner

handoff_progress 参数请使用：
action=receive
handoff_id=<created handoff_id>
actor=agent:planner

repair_invalidate_event 请使用 handoff_progress(receive) 返回的 event id，并设置：
event_id=<receive event id>
reason=manual repair smoke invalidate receive event
actor=agent:main

repair_backfill_event 请使用同一个 handoff 和 workflow，并设置：
workflow_id=<created workflow_id>
handoff_id=<created handoff_id>
type=received
subject_actor=agent:planner
producer_actor=agent:planner
requested_by=agent:main
reason=manual repair smoke backfill receive event

repair_list 请使用同一个 handoff_id。
handoff_get 请使用同一个 handoff_id。

调用完成后，请输出 handoff_id、workflow_id、invalidated event_id、invalidate repair_id、backfill repair_id、两条 repair action、最终 handoff state。最终 handoff state 应为 received。
```

```bash
openclaw sessions export-trajectory --agent main --session-key '<session-key>' --json

./scripts/extract_openclaw_truth_plane_repair_results.sh \
  --events .openclaw/trajectory-exports/<export-dir>/events.jsonl \
  --output /tmp/openclaw-truth-plane-repair-results.json

SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --openclaw-truth-plane-repair-results /tmp/openclaw-truth-plane-repair-results.json
```

### Stage 4 / 阶段 4 truth-plane divergence/candidate/reopen smoke 验收

如需验收 OpenClaw 真实观察 divergence、列出 repair candidate，并 reopen completed handoff，可让 main agent 完整推进一条 handoff，再查询 divergence 与 candidate 后执行 reopen：

```text
请通过已注册的 clawside MCP tools 创建一条测试 handoff，dispatch 后按协议推进到 completed，然后查询 divergence、repair candidate，reopen 这条 completed handoff，并查询 repair、最终 handoff truth 与 workflow status。

请按顺序调用：
1. handoff_create
2. handoff_dispatch
3. handoff_progress action=receive
4. handoff_progress action=claim
5. handoff_progress action=start
6. handoff_progress action=checkpoint
7. handoff_progress action=complete
8. divergence_list
9. repair_candidate_list
10. repair_reopen_handoff
11. repair_list
12. handoff_get
13. workflow_status

创建参数请使用：
workflow_kind=manual_openclaw_truth_plane_reopen_smoke
sender=agent:main
receiver=agent:planner
task_kind=truth_plane_reopen_smoke
intent=verify OpenClaw can list divergence and repair candidates, then reopen a clawside handoff

dispatch 参数请使用：
将 handoff_id 设为 handoff_create 返回的 handoff_id
adapter=manual
target=agent:planner

所有 handoff_progress 调用请将 handoff_id 设为 handoff_create 返回的 handoff_id，actor=agent:planner，并按上面的 action 顺序执行。
divergence_list、repair_candidate_list、repair_list、handoff_get 请使用 handoff_create 返回的 handoff_id。
repair_reopen_handoff 请使用 handoff_id、reason 和 actor：handoff_id 设为 handoff_create 返回的 handoff_id，reason 设为 `manual repair smoke reopen completed handoff`，actor=agent:main。
workflow_status 请将 workflow_id 设为 handoff_create 返回的 workflow_id。

调用完成后，请输出 handoff_id、workflow_id、divergence_id、candidate_id、reopened handoff_id、repair_id、最终 handoff state、workflow status。
```

```bash
openclaw sessions export-trajectory --agent main --session-key '<session-key>' --json

./scripts/extract_openclaw_truth_plane_reopen_results.sh \
  --events .openclaw/trajectory-exports/<export-dir>/events.jsonl \
  --output /tmp/openclaw-truth-plane-reopen-results.json

SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --openclaw-truth-plane-reopen-results /tmp/openclaw-truth-plane-reopen-results.json
```

预期结果摘要包含：

```text
openclaw_truth_plane_reopen_results: ok
```

本地提取命令示例：

```bash
./scripts/extract_openclaw_truth_plane_reopen_results.sh --events PATH --output /tmp/openclaw-truth-plane-reopen-results.json
```

本地 verifier 命令示例：

```bash
./scripts/verify_openclaw_mcp.sh --openclaw-truth-plane-reopen-results /tmp/openclaw-truth-plane-reopen-results.json
```

### Stage 5 / 阶段 5 truth-plane continuity smoke 验收

如需验收 OpenClaw 在 reopen 同一条 handoff 后，仍可沿同一 handoff 继续推进并再次落到 completed truth，可让 main agent 先完整推进一条 handoff，观察 divergence 与 repair candidate，reopen 后再对同一个 handoff 重新 dispatch 与 progress：

```text
请通过已注册的 clawside MCP tools 创建一条测试 handoff，dispatch 后按协议推进到 completed，然后查询 divergence、repair candidate，reopen 这条 completed handoff，并对同一个 handoff 再次 dispatch、receive、claim、start、checkpoint、complete，最后查询 handoff truth 与 workflow status。

请按顺序调用：
1. handoff_create
2. handoff_dispatch
3. handoff_progress action=receive
4. handoff_progress action=claim
5. handoff_progress action=start
6. handoff_progress action=checkpoint
7. handoff_progress action=complete
8. divergence_list
9. repair_candidate_list
10. repair_reopen_handoff
11. handoff_dispatch
12. handoff_progress action=receive
13. handoff_progress action=claim
14. handoff_progress action=start
15. handoff_progress action=checkpoint
16. handoff_progress action=complete
17. handoff_get
18. workflow_status

创建参数请使用：
workflow_kind=manual_openclaw_truth_plane_continuity_smoke
sender=agent:main
receiver=agent:planner
task_kind=truth_plane_continuity_smoke
required_for_workflow_completion=true
intent=verify OpenClaw can reopen a completed clawside handoff and continue it to completed truth again

第一次 handoff_dispatch 请将 handoff_id 设为 handoff_create 返回的 handoff_id，adapter=manual，target=agent:planner。
第一次 handoff_progress 序列请使用 handoff_create 返回的 handoff_id，actor=agent:planner，并依次执行 receive、claim、start、checkpoint、complete。
divergence_list 和 repair_candidate_list 请使用 handoff_create 返回的 handoff_id。
repair_reopen_handoff 请使用同一个 handoff_id，reason 设为 `manual continuity smoke reopen completed handoff`，actor=agent:main。
第二次 handoff_dispatch 请继续使用同一个 handoff_id，adapter=manual，target=agent:planner。
第二次 handoff_progress 序列请继续使用同一个 handoff_id，actor=agent:planner，并依次执行 receive、claim、start、checkpoint、complete。
handoff_get 请使用同一个 handoff_id。workflow_status 请使用 handoff_create 返回的 workflow_id。

调用完成后，请输出 handoff_id、workflow_id、repair_id、repair action、reopened_state、post-reopen final handoff state、post-reopen final workflow status，以及 divergence_list / repair_candidate_list 是否返回 structuredContent。
```

导出与提取示例中，请将 `完整 session key` 替换为实际完整 session key，将 `export-directory` 替换为 `openclaw sessions export-trajectory` 打印的实际导出目录名；`export-directory` 不是字面路径片段。

```bash
openclaw sessions export-trajectory --agent main --session-key '完整 session key' --json

./scripts/extract_openclaw_truth_plane_continuity_results.sh \
  --events .openclaw/trajectory-exports/export-directory/events.jsonl \
  --output /tmp/openclaw-truth-plane-continuity-results.json

SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --openclaw-truth-plane-continuity-results /tmp/openclaw-truth-plane-continuity-results.json
```

预期结果摘要包含：

```text
openclaw_truth_plane_continuity_results: ok
```

本地提取命令示例：

```bash
./scripts/extract_openclaw_truth_plane_continuity_results.sh --events PATH --output /tmp/openclaw-truth-plane-continuity-results.json
```

本地 verifier 命令示例：

```bash
./scripts/verify_openclaw_mcp.sh --openclaw-truth-plane-continuity-results /tmp/openclaw-truth-plane-continuity-results.json
```

### Stage 6 / 阶段 6 smoke profile 验收

Stage 6 将前面 Stage 0-5 的验收入口收敛成明确的 profile，避免每次手工记住一长串参数。

快速本地健康检查：

```bash
SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh --profile quick
```

完整 truth-plane evidence gate：

```bash
SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --profile truth-plane-full \
  --openclaw-tool-results /tmp/openclaw-tool-results.json \
  --openclaw-truth-plane-results /tmp/openclaw-truth-plane-results.json \
  --openclaw-truth-plane-progression-results /tmp/openclaw-truth-plane-progression-results.json \
  --openclaw-truth-plane-mutation-results /tmp/openclaw-truth-plane-mutation-results.json \
  --openclaw-truth-plane-repair-results /tmp/openclaw-truth-plane-repair-results.json \
  --openclaw-truth-plane-reopen-results /tmp/openclaw-truth-plane-reopen-results.json \
  --openclaw-truth-plane-divergence-results /tmp/openclaw-truth-plane-divergence-results.json \
  --openclaw-truth-plane-continuity-results /tmp/openclaw-truth-plane-continuity-results.json \
  --openclaw-truth-plane-delivery-results /tmp/openclaw-truth-plane-delivery-results.json \
  --coordination-evidence-summary <coordination-evidence-summary.json>
```

`truth-plane-full` 要求 Stage 0-5、Stage 12 divergence 和 Stage 13 delivery evidence 的 OpenClaw trajectory extractor JSON，加上 `coordination-evidence-summary.json`，都显式传入；缺少任一项都会失败，不再把缺失 evidence 当成 skipped。

发布级只读 evidence gate：优先使用 Stage 11 的 bundle-first 流程生成 9 个 trajectory result JSON、复制 `coordination-evidence-summary.json` 并写出 `verify-release-evidence.sh`；下面的长命令保留为高级手工 fallback。

```bash
SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --profile release-evidence \
  --coordination-evidence-summary <coordination-evidence-summary.json> \
  --openclaw-tool-results /tmp/openclaw-tool-results.json \
  --openclaw-truth-plane-results /tmp/openclaw-truth-plane-results.json \
  --openclaw-truth-plane-progression-results /tmp/openclaw-truth-plane-progression-results.json \
  --openclaw-truth-plane-mutation-results /tmp/openclaw-truth-plane-mutation-results.json \
  --openclaw-truth-plane-repair-results /tmp/openclaw-truth-plane-repair-results.json \
  --openclaw-truth-plane-reopen-results /tmp/openclaw-truth-plane-reopen-results.json \
  --openclaw-truth-plane-divergence-results /tmp/openclaw-truth-plane-divergence-results.json \
  --openclaw-truth-plane-continuity-results /tmp/openclaw-truth-plane-continuity-results.json \
  --openclaw-truth-plane-delivery-results /tmp/openclaw-truth-plane-delivery-results.json
```

带真实投递的发布前本地 gate：

```bash
SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --profile release \
  --openclaw-tool-results /tmp/openclaw-tool-results.json \
  --openclaw-truth-plane-results /tmp/openclaw-truth-plane-results.json \
  --openclaw-truth-plane-progression-results /tmp/openclaw-truth-plane-progression-results.json \
  --openclaw-truth-plane-mutation-results /tmp/openclaw-truth-plane-mutation-results.json \
  --openclaw-truth-plane-repair-results /tmp/openclaw-truth-plane-repair-results.json \
  --openclaw-truth-plane-reopen-results /tmp/openclaw-truth-plane-reopen-results.json \
  --openclaw-truth-plane-divergence-results /tmp/openclaw-truth-plane-divergence-results.json \
  --openclaw-truth-plane-continuity-results /tmp/openclaw-truth-plane-continuity-results.json \
  --openclaw-truth-plane-delivery-results /tmp/openclaw-truth-plane-delivery-results.json \
  --deliver-main \
  --chat-id <telegram_chat_id>
```

`release-evidence` 会先执行本地 Go readiness 检查，再执行完整 OpenClaw MCP smoke，并保持只读。`release` 在此基础上增加显式真实投递 gate。真实投递仍然只通过 sender 后端完成，不直接调用 Telegram API。

### Stage 7 / 阶段 7 fixtures profile 回归验收

Stage 7 增加仓库内置的 golden evidence fixtures，让本地和 CI 可以不用依赖 `.openclaw/trajectory-exports` 私有路径，也能稳定回归 Stage 0-5 的 OpenClaw MCP verifier 行为。

最短命令：

```bash
SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh --profile fixtures
```

`fixtures` 会读取仓库内置的 extracted JSON 样本：

```text
testdata/openclaw-smoke/stage0-5/
```

这些 fixtures 用于本地 / CI regression，证明 verifier 对固定 golden evidence 的判断没有回退；它们不是发布验收 evidence，也不证明新的真实 OpenClaw trajectory 仍然能产出同样结果。

发布或完整验收仍然使用：

- `truth-plane-full`：显式传入真实 OpenClaw trajectory extractor 输出的 9 个 trajectory result JSON，加上 `coordination-evidence-summary.json`；
- `release-evidence`：通过真实 trajectory evidence bundle，或显式 trajectory JSON + coordination evidence summary，执行只读发布级验收；
- `release`：在真实 evidence（包含 coordination evidence summary）基础上再显式开启 `--deliver-main` 和 `--chat-id`。

真实投递仍然只通过 sender 后端完成，不直接调用 Telegram API。

### Stage 8 / 阶段 8 本地发布保护

Stage 8 增加本地 release guard，用于在打 tag 和 push 前重复执行同一组安全检查。它不是 GitHub Actions，也不会直接创建 GitHub Release。

本地 clean CI：

```bash
./scripts/ci-local.sh clean
```

`clean` 模式只从 `git ls-files` 的 tracked 文件构造临时目录，然后依次运行：

1. `scripts/secret-scan.sh`
2. `scripts/secret-scan.sh --history`
3. `gofmt` 检查
4. `go vet ./...`
5. `go test -count=1 ./...`
6. `./build.sh`

安装 pre-push hook：

```bash
./scripts/install-hooks.sh
```

创建并推送 release tag：

```bash
./scripts/tag-release.sh --evidence-bundle ./release-evidence/openclaw-v0.1.0 v0.1.0
```

`tag-release.sh` 要求工作树干净、tag 名以 `v` 开头、tag 不存在，并且必须通过 `--evidence-bundle DIR` 或 `CLAWSIDE_RELEASE_EVIDENCE_BUNDLE=DIR` 显式提供 release evidence bundle。脚本会先只读复验 bundle manifest 和 `verify-release-evidence.sh`，再运行 `scripts/ci-local.sh clean`，最后创建 tag 并会自动 push tag；push tag 本身不会直接创建 GitHub Release，Stage 8 不新增 GitHub Actions release workflow。

### Stage 9 / 阶段 9 远端 CI 与 Release workflow

Stage 9 在 Stage 8 本地发布保护之上增加 GitHub Actions 远端 CI 和 `v*` tag 触发的 release workflow。Stage 8 负责本地 pre-tag guard，Stage 9 负责 tag 到达 GitHub 后的远端 preflight、构建、checksum 和 GitHub Release。

远端 CI 在 `push` 和 `pull_request` 上运行：

1. `scripts/secret-scan.sh`
2. `scripts/secret-scan.sh --history`
3. `gofmt` 检查
4. `go vet ./...`
5. `go test -count=1 ./...`

Release workflow 只在 `v*` tag 上运行，构建 linux amd64/arm64、darwin amd64/arm64、windows amd64 五个平台产物，生成 checksums，并创建或更新 GitHub Release。每个 release 归档包含二进制、`LICENSE`、根 README、多语言 README、`.example.env` 和 `configs/config.example.toml`，不包含 `.env`、`configs/config.toml`、数据库、日志或 `.openclaw/trajectory-exports`。

推荐发布路径是在明确发布授权后执行：

```bash
scripts/ci-local.sh clean
scripts/tag-release.sh --evidence-bundle ./release-evidence/openclaw-vX.Y.Z vX.Y.Z
```

`tag-release.sh` 会先只读复验传入的 release evidence bundle，再运行本地 clean CI，之后创建并 push `v*` tag 到 GitHub，然后由 GitHub Actions release workflow 远端构建并创建或更新 GitHub Release。Stage 9 的实现和本地验收不执行 push、tag 或 release；这些共享状态操作仍需要显式授权。

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

### Stage 10 / 阶段 10 repair backfill MCP 验收

Stage 10 复用既有 Stage 3 repair evidence 路径，不新增独立的 `truth_plane_backfill` 通道。同一个 `--openclaw-truth-plane-repair-results` verifier 现在会要求 `repair_invalidate_event` 之后出现 `repair_backfill_event` evidence，校验 `manual repair smoke backfill receive event`，并要求最终 handoff truth 回到 `received`。

仓库内置 `fixtures` profile 已在 `testdata/openclaw-smoke/stage0-5/repair-results.json` 中包含这条 backfill replay evidence。

### Stage 11 / 阶段 11 release evidence gate

Stage 11 将 release acceptance 拆成只读的发布级 evidence gate，以及需要显式授权的真实投递 gate。`fixtures` 只用于回归，`truth-plane-full` 用于只验证真实 trajectory evidence，`release-evidence` 用于打 tag 前把真实 OpenClaw trajectory extracts 当成发布级 evidence 验收。

生成 release evidence bundle 前，先从本地 orchestrator truth store 生成 coordination evidence summary。这个文件是预先生成的 sanitized artifact，不是 trajectory extractor 输出：

```bash
go run ./cmd/orchestrator workflow evidence \
  --db <sqlite-db> \
  --workflow-id <workflow-id> \
  --include-agents \
  > <coordination-evidence-summary.json>
```

推荐的 bundle-first 路径先从真实 trajectory exports 和预生成的 coordination summary 生成本地 evidence bundle，并用 `--verify` 立即运行只读发布级 verifier：

```bash
./scripts/build_openclaw_release_evidence_bundle.sh \
  --output-dir ./release-evidence/openclaw-vX.Y.Z \
  --coordination-evidence-summary <coordination-evidence-summary.json> \
  --tool-events <stage0-export>/events.jsonl \
  --truth-plane-events <stage1-export>/events.jsonl \
  --progression-events <stage2-export>/events.jsonl \
  --mutation-events <stage3-export>/events.jsonl \
  --repair-events <stage10-export>/events.jsonl \
  --reopen-events <stage4-export>/events.jsonl \
  --continuity-events <stage5-export>/events.jsonl \
  --divergence-events <stage12-export>/events.jsonl \
  --delivery-events <stage13-export>/events.jsonl \
  --verify
```

bundle 命令会为 9 个 trajectory-derived result 文件调用已有 extractor，并复制预生成的 `coordination-evidence-summary.json`；bundle 阶段不会打开 orchestrator DB。它会写出 `manifest.json`、10 个 evidence artifacts 和 `verify-release-evidence.sh`。`--verify` 运行只读发布级复验，保持只读，不包含 `--deliver-main`、`--chat-id`，也不直接调用 Telegram API。`verify-release-evidence.sh` 会用脚本自身目录定位 evidence artifacts，先通过 `openclaw-release-evidence-bundle verify-manifest` 校验 manifest 中各 evidence 文件的存在性、元数据和 SHA256，再用当前仓库的 `scripts/verify_openclaw_mcp.sh --profile release-evidence` 携带全部 evidence path 复验，其中包括 `--coordination-evidence-summary`；因此 bundle 可以在同一仓库内移动后继续复验。`release-evidence/openclaw-vX.Y.Z` 是本地生成目录，默认被 git 忽略。

如需拆成两步，也可以手工运行生成的验证脚本：

```bash
./release-evidence/openclaw-vX.Y.Z/verify-release-evidence.sh
```

发布前可以先跑不会创建 tag、不会 push 的安全验收：

```bash
scripts/tag-release.sh --verify-only --evidence-bundle ./release-evidence/openclaw-vX.Y.Z vX.Y.Z
```

也可以用环境变量传入同一个目录：

```bash
CLAWSIDE_RELEASE_EVIDENCE_BUNDLE=./release-evidence/openclaw-vX.Y.Z scripts/tag-release.sh --verify-only vX.Y.Z
```

高级手工 fallback 仍可显式传入 9 个 trajectory result JSON 和 coordination summary：

```bash
scripts/ci-local.sh clean
SENDER_AUTH_KEY=... scripts/verify_openclaw_mcp.sh \
  --profile release-evidence \
  --coordination-evidence-summary <coordination-evidence-summary.json> \
  --openclaw-tool-results /tmp/openclaw-tool-results.json \
  --openclaw-truth-plane-results /tmp/openclaw-truth-plane-results.json \
  --openclaw-truth-plane-progression-results /tmp/openclaw-truth-plane-progression-results.json \
  --openclaw-truth-plane-mutation-results /tmp/openclaw-truth-plane-mutation-results.json \
  --openclaw-truth-plane-repair-results /tmp/openclaw-truth-plane-repair-results.json \
  --openclaw-truth-plane-reopen-results /tmp/openclaw-truth-plane-reopen-results.json \
  --openclaw-truth-plane-divergence-results /tmp/openclaw-truth-plane-divergence-results.json \
  --openclaw-truth-plane-continuity-results /tmp/openclaw-truth-plane-continuity-results.json \
  --openclaw-truth-plane-delivery-results /tmp/openclaw-truth-plane-delivery-results.json
```

只有在显式授权后，才运行带 `--profile release --deliver-main --chat-id <telegram_chat_id>` 的真实投递 gate。该路径仍然使用 `scripts/verify_openclaw_mcp.sh`，经 sender 后端完成，不直接调用 Telegram。

### Diagnostic support bundle / 诊断支持包

需要给 reviewer 或 operator 收集本地 readiness 线索时，使用只读 diagnostic bundle：

```bash
./scripts/build_openclaw_diagnostic_bundle.sh \
  --output-dir ./diagnostic-bundles/local

./diagnostic-bundles/local/verify-diagnostic-bundle.sh
```

该命令写出 `manifest.json`、`smoke-report.json`、`sender-health.json`、`sender-ready.json`、`sender-stats.json`、`sender-jobs.json`、`registration-guidance.json`、`environment-summary.json` 和 `verify-diagnostic-bundle.sh`。它只读取 smoke、registration guidance 和 sender observability；不执行真实投递，不写 OpenClaw 或 Claude 配置。`SENDER_AUTH_KEY` 只从本地环境继承，secrets 会被 redacted，`diagnostic-bundles/` 是本地生成目录并默认被 git 忽略。

### Stage 12 / 阶段 12 divergence / E2E 闭环验收

Stage 12 将 divergence 观察从 reopen/continuity 验收里拆成独立 evidence：同一条 handoff dispatch 后，先用 `divergence_record` 记录 `transport_accepted` observer signal，再完整走到 `completed`。随后用 `divergence_list` 观察 `transport_accepted` divergence，用 `repair_candidate_list` 验证 `missing_authoritative_progress` candidate，最后用 `handoff_get` 和 `workflow_status` 证明 E2E truth 仍然闭环在 `completed`。导出、extractor 与 verifier 仍是只读 evidence 路径。

```text
请通过已注册的 clawside MCP tools 创建一条测试 handoff，dispatch 后记录一条 transport_accepted observer divergence signal，再按协议推进到 completed，然后查询 divergence、repair candidate、handoff truth 与 workflow status。

请按顺序调用：
1. handoff_create
2. handoff_dispatch
3. divergence_record type=transport_accepted
4. handoff_progress action=receive
5. handoff_progress action=claim
6. handoff_progress action=start
7. handoff_progress action=checkpoint
8. handoff_progress action=complete
9. divergence_list
10. repair_candidate_list
11. handoff_get
12. workflow_status

divergence_record 请使用 handoff_create 返回的 workflow_id / handoff_id，type=transport_accepted，producer_actor=system:adapter。

调用完成后，请输出 handoff_id、workflow_id、divergence_id、candidate_id、signal_type、candidate reason、final handoff state 和 final workflow status。
```

```bash
openclaw sessions export-trajectory --agent main --session-key '<session-key>' --json

./scripts/extract_openclaw_truth_plane_divergence_results.sh \
  --events .openclaw/trajectory-exports/<export-dir>/events.jsonl \
  --output /tmp/openclaw-truth-plane-divergence-results.json

SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --openclaw-truth-plane-divergence-results /tmp/openclaw-truth-plane-divergence-results.json
```

预期结果摘要包含：

```text
openclaw_truth_plane_divergence_results: ok
```

### Stage 13 / 阶段 13 handoff + A2A delivery evidence 验收

Stage 13 验证 OpenClaw 可以通过 MCP 可见的 sender job evidence，把一条 clawside handoff 和一条 A2A delivery sender job 关联起来。它不改变 handoff state machine，不把 delivery 记录成权威 progress，也不直接调用 Telegram API；投递 evidence 只来自 MCP 工具可见的 sender job 查询结果。

```text
请通过已注册的 clawside MCP tools 创建一条测试 handoff，dispatch 后发起一次 A2A delivery，再用 sender job 查询工具读取投递 evidence，最后查询 handoff truth 与 workflow status。

请按顺序调用：
1. handoff_create
2. handoff_dispatch
3. a2a_deliver
4. sender_job_get
5. sender_job_list
6. handoff_get
7. workflow_status

参数：
workflow_kind=manual_openclaw_truth_plane_delivery_smoke
sender=agent:main
receiver=agent:planner
task_kind=truth_plane_delivery_smoke
required_for_workflow_completion=false
intent=verify OpenClaw can tie clawside handoff truth to A2A delivery evidence
dispatch adapter=manual target=agent:planner
a2a_deliver target_agent=planner text=manual Stage 13 delivery smoke for <created handoff_id> chat_id=<telegram_chat_id>
sender_job_get 使用 a2a_deliver 返回的 job_id；sender_job_list 使用 status=sent limit=10；handoff_get / workflow_status 使用同一组 handoff_id / workflow_id。

调用完成后，请输出 handoff_id、workflow_id、delivery job_id、sender job status、handoff state 和 workflow status。
```

```bash
openclaw sessions export-trajectory --agent main --session-key '<session-key>' --json

./scripts/extract_openclaw_truth_plane_delivery_results.sh \
  --events .openclaw/trajectory-exports/<export-dir>/events.jsonl \
  --output /tmp/openclaw-truth-plane-delivery-results.json

SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --openclaw-truth-plane-delivery-results /tmp/openclaw-truth-plane-delivery-results.json
```

预期结果摘要包含：

```text
openclaw_truth_plane_delivery_results: ok
```

## A2A delivery bridge CLI

sender sidecar 启动后，可以直接用本地 bridge 入口发起一次定向投递：

```bash
go run ./cmd/a2a-delivery \
  --target-agent planner \
  --text "请直接把结果发给我" \
  --chat-id <telegram_chat_id> \
  --sender-auth-key "$SENDER_AUTH_KEY"
```

如果不显式传 `--chat-id`，也可以传会话上下文字段让 bridge 解析目标用户：

```bash
go run ./cmd/a2a-delivery \
  --target-agent engineer \
  --text "请把当前状态同步给当前会话用户" \
  --delivery-context-to <telegram_chat_id> \
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
