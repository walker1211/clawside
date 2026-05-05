# clawside

[Landing Page](./README.md) | [中文](./README.zh-CN.md)

`clawside` is a local MCP sidecar / truth layer for OpenClaw.

It is not the OpenClaw runtime itself, and it is not only a Telegram sender. The repository currently hosts three sidecar capabilities: a local sender, handoff/workflow orchestration foundations, and adapter / bridge infrastructure for OpenClaw.

## Relationship with OpenClaw

- **OpenClaw** owns sessions, messages, and the agent runtime.
- **clawside** owns deterministic data planes outside the runtime, such as handoff/workflow truth, watches, repairs, and the sender bridge.
- The repository keeps stable compatibility points for OpenClaw, including `~/.openclaw/openclaw.json`, the stdio MCP server, and the A2A delivery bridge.

## Current capabilities

- **Telegram sender**: local HTTP sender service for enqueueing, authentication, idempotency, and worker delivery.
- **orchestrator CLI / store / state machine / watch / repair foundations**: foundations for handoffs, events, workflows, watches, and repairs.
- **OpenClaw adapter foundations**: integration points for dispatching and bridge actions through OpenClaw-compatible entrypoints.
- **A2A delivery bridge skill**: explicit outward delivery when the official announce / nested callback path is unreliable.
- **MCP + skill v1 surface**: tools OpenClaw can install, register, and consume for handoffs, workflows, watches, repairs, and A2A delivery.

This version now productizes the minimal v1 into an installable, registerable, and verifiable MCP server + skill suite.

## Components

- `cmd/config-builder/`: Go CLI that generates the derived sender config.
- `cmd/orchestrator/`: low-level orchestrator debug / operation entrypoint.
- `cmd/clawside-mcp/`: stdio MCP server entrypoint.
- `cmd/a2a-delivery/`: A2A delivery bridge CLI.
- `internal/configbuilder/`: extracts the minimal sender config from OpenClaw source config.
- `internal/orchestrator/`: handoff, workflow, event, watch, repair, and adapter foundations.
- `internal/toolserver/`: MCP tool handlers.
- `internal/a2adelivery/`: A2A delivery bridge, polling, and orchestration logic.
- `main.go` + `http_handler.go` + `worker.go`: sender service entrypoint, HTTP API, and delivery worker.
- `.claude/skills/openclaw-a2a-delivery/`: OpenClaw A2A delivery skill definition.

## Installation and configuration

### 1. Generate the derived sender config

Recommended setup from OpenClaw config:

```bash
export SENDER_AUTH_KEY='your-local-sender-key'
./scripts/config_builder.sh
```

By default, this reads `~/.openclaw/openclaw.json` and writes:

```text
configs/config.toml
```

To use another input file:

```bash
./scripts/config_builder.sh --input /path/to/openclaw.json
```

Notes:

- `scripts/config_builder.sh` is the stable entrypoint and calls the Go builder internally.
- `configs/config.toml` is derived and should not be maintained by hand when using the builder.
- `configs/config.toml` contains bot tokens and the local sender auth key, and is ignored by git.
- The builder writes the output with mode `600`.
- `sender_auth_key` is separate from Telegram bot tokens and is only used for local `/send` authentication.

### 2. Inspect the manual config template

The repository includes a non-secret template:

```text
configs/config.example.toml
```

If you do not want to generate config from OpenClaw, you can copy it manually:

```bash
cp configs/config.example.toml configs/config.toml
```

Then fill in:

- `sender_auth_key`: local auth key for sender `/send`; it must be distinct from Telegram bot tokens.
- `telegram.global_allow_user_ids`: globally allowed Telegram chat/user ids.
- `telegram.bots.<name>.token`: Telegram bot token for the named logical bot.
- `telegram.bots.<name>.allow_user_ids`: per-bot allowlist.

## Common scripts

```bash
./scripts/config_builder.sh
./build.sh
./start.sh
./stop.sh
./restart.sh
./scripts/start_mcp.sh --db ./sender.db
```

Notes:

- `./build.sh`: builds the root sender binary at `./clawside`.
- `./start.sh`: starts the sender service in the background and writes `logs/sender.pid` plus `logs/sender.log`.
- `./stop.sh`: stops the sender process recorded by `./start.sh`.
- `./restart.sh`: runs `./stop.sh` and then `./start.sh`.
- `./scripts/start.sh`: starts the sender in the foreground through `go run .`, useful for debugging or avoiding a binary build.
- `./scripts/start_mcp.sh`: starts the stdio MCP server for OpenClaw registration.

`./start.sh` / `./stop.sh` / `./restart.sh` only manage the pidfile they write. If you started `./scripts/start.sh` manually in the foreground, stop that process manually.

## Start the sender service

```bash
./build.sh
./start.sh
```

The sender listens on:

```text
127.0.0.1:8787
```

The service only allows loopback listen addresses by default, which is suitable for local use.

## Send a message

```bash
curl -X POST http://127.0.0.1:8787/send \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer your-local-sender-key' \
  -d '{
    "bot": "guardian",
    "chat_id": 123456789,
    "text": "test message",
    "idempotency_key": "idem-001"
  }'
```

Successful response:

```json
{
  "job_id": 1,
  "status": "pending",
  "idempotency_key": "idem-001"
}
```

## Query sender status

```bash
curl http://127.0.0.1:8787/healthz
curl http://127.0.0.1:8787/readyz
curl http://127.0.0.1:8787/stats
curl "http://127.0.0.1:8787/jobs?status=pending&limit=20"
curl http://127.0.0.1:8787/jobs/<job_id>
```

## OpenClaw MCP / tool server

This section summarizes the v1 tool contract, startup path, and invocation boundaries.

Start the stdio MCP server:

```bash
go run ./cmd/clawside-mcp \
  --db ./sender.db \
  --sender-base-url http://127.0.0.1:8787 \
  --sender-auth-key "$SENDER_AUTH_KEY" \
  --target-agent-map "qa=guardian"
```

The more stable local entrypoint is:

```bash
./scripts/start_mcp.sh --db ./sender.db
```

`--target-agent-map` and `CLAWSIDE_TARGET_AGENT_BOT_MAP` accept comma-separated `target_agent=bot` pairs. `a2a_deliver` callers still provide `target_agent`; they do not pass arbitrary sender bot overrides. Semantic A2A retry idempotency requires an explicit stable `idempotency_key`; omitted keys are generated as unique nonce-based delivery attempts.

Built-in target agent mappings include `main -> main`, `planner -> planner`, `engineer -> engineer`, `researcher -> researcher`, `archivist -> archivist`, `guardian -> guardian`, and `closer -> closer`. `main` is intended for the OpenClaw router / main agent path. If `~/.openclaw/openclaw.json` defines a `main -> default` Telegram route, the config builder generates `[telegram.bots.main]`, and `a2a_deliver` can use `target_agent=main` directly.

Current v1 tools:

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

Recommended reading:

- `handoff_create`: creates a new handoff / workflow starting point.
- `handoff_get`: returns current handoff truth and timeline.
- `handoff_dispatch`: records a dispatch attempt and transport request so pure MCP flows can dispatch before receiving.
- `handoff_progress`: applies protocol actions such as `receive`, `claim`, `start`, `submit`, `approve`, and `complete`.
- `workflow_status`: returns one workflow aggregate view.
- `workflow_list`: lists all workflows with projected handoffs.
- `watch_list`: lists watches attached to a handoff.
- `watch_run`: runs due watch checks at a provided RFC3339 timestamp.
- `watch_update`: updates a watch deadline, status, or escalation policy.
- `ownership_get`: returns the ownership binding for a handoff.
- `ownership_update`: updates handoff ownership fields and keeps the ownership binding synchronized.
- `repair_list`: lists repair records, optionally filtered by handoff.
- `repair_invalidate_event`: invalidates an accepted event and replays handoff truth.
- `repair_reopen_handoff`: reopens a terminal handoff and replays truth.
- `repair_candidate_list`: lists repair candidates for a handoff.
- `divergence_list`: lists observer divergence hints for a handoff.
- `sender_health`: checks sender process health.
- `sender_ready`: checks whether the sender is ready to process delivery work.
- `sender_stats`: returns sender queue counts and worker timing fields.
- `sender_job_list`: lists sender jobs by status with a bounded limit.
- `sender_job_get`: returns a single sender job status.
- `a2a_deliver`: performs real outward delivery through the existing sender bridge after resolving `target_agent` through built-in or configured mapping.

Boundaries:

- This is a minimal v1 tool surface, not the full truth-plane MCP product surface.
- Lower-level repair backfill and deeper truth-plane operations still live outside the v1 MCP surface.
- `a2a_deliver` and `sender_*` observability tools depend on the local sender sidecar.
- `sender_*` observability tools are read-only and do not expose raw message text, raw idempotency keys, or Telegram bot tokens.
- `handoff_*` / `workflow_*` / `watch_*` / `ownership_get` / `repair_*` / `divergence_list` depend on `--db` pointing to the same sqlite truth store.

To register with OpenClaw, configure it as a stdio MCP server and point `command` to this repository's `scripts/start_mcp.sh`.

## A2A delivery bridge CLI

After the sender sidecar is running, you can trigger a direct targeted delivery:

```bash
go run ./cmd/a2a-delivery \
  --target-agent planner \
  --text "Please send the result directly to me" \
  --chat-id 123456789 \
  --sender-auth-key "$SENDER_AUTH_KEY"
```

If you do not pass `--chat-id`, you can pass session context fields and let the bridge resolve the target user:

```bash
go run ./cmd/a2a-delivery \
  --target-agent engineer \
  --text "Please sync the current status to the current session user" \
  --delivery-context-to 123456789 \
  --sender-auth-key "$SENDER_AUTH_KEY"
```

Boundaries:

- Only explicit calls from the main agent are supported.
- Delivery goes through the existing sender backend.
- The bridge does not call the Telegram API directly.
- The bridge does not attempt to repair the official announce path.

## Low-level CLI / debug entrypoints

```bash
go run ./cmd/config-builder --input ~/.openclaw/openclaw.json --output ./configs/config.toml
go run .
go run ./cmd/orchestrator handoff create --db ./sender.db --workflow-kind generic --sender agent:planner --receiver agent:writer --task-kind generic_task --intent "write summary"
go run ./cmd/orchestrator handoff list --db ./sender.db
go run ./cmd/orchestrator workflow list --db ./sender.db
go run ./cmd/orchestrator watch run --db ./sender.db
```

## Sender permissions and input boundaries

The sender checks permission before enqueueing HTTP requests:

```text
chat_id ∈ global_allow_user_ids OR chat_id ∈ bot.allow_user_ids
```

Meaning:

- If the global allowlist matches, the current bot can send.
- If the global allowlist does not match but the bot-specific allowlist matches, the current bot can send.
- If neither matches, the request is rejected and not enqueued.

The current implementation also enforces:

- `bot` must be a configured logical bot name.
- `text` must be plain text and cannot be empty.
- `text` longer than Telegram's single-message text limit, 4096 characters, is rejected before enqueue.
- `max_attempts` must be in the `1..5` range.
