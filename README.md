[中文](./README.zh-CN.md)

# clawside

`clawside` is a local MCP sidecar and truth layer for OpenClaw.

It is not the OpenClaw runtime, and it is not only a Telegram sender. OpenClaw owns sessions, messages, and model execution. `clawside` owns deterministic coordination state outside the runtime: workflows, handoffs, watches, repairs, ownership, evidence, and sender-backed delivery bridges.

## What it provides

- **Local Telegram sender** for authenticated, idempotent, loopback-only delivery.
- **Truth-plane orchestration** for handoffs, workflows, events, watches, ownership, repairs, and divergence signals.
- **MCP server** for OpenClaw or another runtime to register agents, query work, progress handoffs, and read evidence.
- **A2A compatibility endpoint** for Agent Card discovery, JSON-RPC coordination queries, controlled inbound tasks, and read-only task events.
- **A2A delivery bridge** for explicit outward delivery when the normal announce / nested callback path is unreliable.
- **Verification scripts** for local CI, MCP smoke, A2A compatibility, private readiness, and public-readiness checks.

## Demo

This private Telegram/OpenClaw dogfood run used a Werewolf-style game to stress A2A routing and handoff ownership. OpenClaw and Telegram carry the chat; `clawside` records durable coordination truth and exposes it through MCP/A2A surfaces.

| Multi-agent chat | Orchestrator constraints |
| --- | --- |
| <img src="./assets/readme/clawside-a2a-game-01.png" alt="Redacted Telegram screenshot showing multiple agents in an A2A coordination game" width="420"> | <img src="./assets/readme/clawside-a2a-game-02.png" alt="Redacted Telegram screenshot showing orchestrator constraints for A2A dogfood" width="420"> |

## Mental model

| Runtime owns | `clawside` owns |
| --- | --- |
| Model workers, sessions, sandboxes, scheduling, and task execution | Durable workflow/handoff truth, ownership, watches, repairs, evidence, and delivery records |
| Telegram conversation flow and agent messages | Sender queue, delivery status, idempotency, and bounded observability |
| Decisions about what work to run | Deterministic projections of executable work, blocked work, reviewer gates, and stale owners |

`clawside` does not launch OpenClaw, Claude, Kimi, model workers, runtime sessions, or sandboxes.

## Quick start

1. Generate local sender config from OpenClaw config:

```bash
cp .example.env .env
# edit .env and set SENDER_AUTH_KEY to a local random key
./scripts/config_builder.sh
```

2. Build and start the loopback sender:

```bash
./build.sh
./start.sh
```

The sender listens on `127.0.0.1:8787` by default. Check it with:

```bash
curl http://127.0.0.1:8787/healthz
curl http://127.0.0.1:8787/readyz
```

3. Register the MCP server in OpenClaw or another runtime:

```text
command: <repo-root>/scripts/start_mcp.sh
args: --db <repo-root>/sender.db
env: SENDER_AUTH_KEY=<local-sender-key>
```

4. Run the default local MCP smoke:

```bash
SENDER_AUTH_KEY=<local-sender-key> ./scripts/verify_openclaw_mcp.sh
```

Useful help commands:

```bash
go run ./cmd/clawside-a2a --help
go run ./cmd/clawside-swarmd help
./scripts/tag-release.sh --help
```

For the full operator runbook, see [Clawside operator guide](./assets/readme/operator-guide.md).

## Which verifier should I run?

| Situation | Command |
| --- | --- |
| Day-to-day local development | `./scripts/ci-local.sh clean` |
| A2A compatibility endpoint | `./scripts/verify_clawside_a2a.sh` |
| MCP tool surface and OpenClaw sidecar smoke | `SENDER_AUTH_KEY=<local-sender-key> ./scripts/verify_openclaw_mcp.sh` |
| Private coordination rehearsal | `./scripts/verify_openclaw_mcp.sh --profile private-coordination --json` |
| Private validation aggregate before release/public work | `./scripts/verify_private_readiness.sh` |
| Real OpenClaw external-runtime evidence closure | `./scripts/close_private_openclaw_external_runtime_evidence.sh --export-dir <export-dir>` |
| Final private/local closure dry run | `./scripts/final_closure_checklist.sh --external-runtime-evidence ./external-runtime-evidence.json --evidence-bundle ./release-evidence/<bundle-dir> --tag v0.0.0-dry-run --repo <owner>/<repo>` |
| GitHub public-readiness review | `./scripts/github-readiness.sh <owner>/<repo>` |

Do not claim public-readiness or release-readiness from a partial run. Use the matching verifier for the claim. Detailed stage-by-stage validation notes live in the [operator guide](./assets/readme/operator-guide.md#openclaw-mcp-smoke-verifier).

## MCP coordination surface

Start the stdio MCP server with the wrapper script:

```bash
./scripts/start_mcp.sh --db ./sender.db
```

The current MCP surface groups tools by role:

| Area | Tools |
| --- | --- |
| Handoff/workflow truth | `handoff_create`, `handoff_get`, `handoff_dispatch`, `handoff_progress`, `workflow_status`, `workflow_list` |
| Agent coordination | `agent_register`, `agent_list`, `next_work`, `blocked_work` |
| Templates | `collaboration_template_list`, `collaboration_template_apply` |
| Watches and ownership | `watch_list`, `watch_run`, `watch_update`, `ownership_get`, `ownership_update` |
| Repair and divergence | `repair_list`, `repair_invalidate_event`, `repair_backfill_event`, `repair_reopen_handoff`, `repair_candidate_list`, `divergence_record`, `divergence_list` |
| Sender observability | `sender_health`, `sender_ready`, `sender_stats`, `sender_job_list`, `sender_job_get` |
| Delivery | `a2a_deliver` |

A minimal external runtime loop looks like this:

```text
agent_register actor=agent:planner project_refs=project://example/upstream capabilities=planning
handoff_create workflow_kind=coordination task_kind=planning intent="Plan the work"
next_work agent_id=agent:planner project_ref=project://example/upstream
handoff_progress action=receive handoff_id=<handoff-id>
handoff_progress action=claim handoff_id=<handoff-id>
handoff_progress action=start handoff_id=<handoff-id>
handoff_progress action=complete handoff_id=<handoff-id>
workflow_status workflow_id=<workflow-id>
coordination_evidence_summary workflow_id=<workflow-id> include_agents=true
```

Use protocol actions such as `receive`, `claim`, `start`, `submit`, `review`, `approve`, and `complete`; do not use projected state names such as `started` or `completed` as actions.

## A2A compatibility endpoint

`cmd/clawside-a2a` exposes an experimental A2A-compatible local endpoint backed by the same SQLite truth store as the MCP server.

Start it with a separate A2A auth key:

```bash
CLAWSIDE_A2A_AUTH_KEY=<local-a2a-key> \
  go run ./cmd/clawside-a2a --db ./sender.db --addr 127.0.0.1:8789
```

Verify it as an external client:

```bash
./scripts/verify_clawside_a2a.sh
```

The endpoint supports Agent Card discovery, a small JSON-RPC allowlist, `tasks/get`, controlled inbound task creation, cancellation, and read-only SSE task events. It does not call `message/send`, `message/stream`, sender delivery, Telegram delivery, runtime launch APIs, worker APIs, or local commands.

## Private Telegram operator

`cmd/clawside-telegram-operator` is a private dogfood operator for fixed truth-plane slash commands:

```text
/health
/status <workflow_id>
/next <agent_id>
/blocked <agent_id>
/approve <handoff_id>
```

Start it only for a bot that OpenClaw is not already polling:

```bash
./scripts/start_telegram_operator.sh --bot guardian
./scripts/stop_telegram_operator.sh
```

If OpenClaw already owns Telegram inbound, keep that path in OpenClaw and report lifecycle events back through `openclaw_event_ingest` or `cmd/openclaw-event-bridge`.

## Components

| Area | Paths |
| --- | --- |
| Sender service | `main.go`, `http_handler.go`, `worker.go`, `send_service.go` |
| Config builder | `cmd/config-builder/`, `internal/configbuilder/`, `scripts/config_builder.sh` |
| Truth-plane CLI and store | `cmd/orchestrator/`, `internal/orchestrator/`, `store.go` |
| MCP server | `cmd/clawside-mcp/`, `internal/toolserver/`, `scripts/start_mcp.sh` |
| A2A endpoint | `cmd/clawside-a2a/`, `cmd/clawside-a2a-example/`, `internal/a2aserver/` |
| A2A delivery bridge | `cmd/a2a-delivery/`, `internal/a2adelivery/`, `.claude/skills/openclaw-a2a-delivery/` |
| Swarm reference loop | `cmd/clawside-swarmd/`, `cmd/clawside-swarm-runner/` |
| OpenClaw evidence tools | `cmd/openclaw-*`, `scripts/verify_openclaw_mcp.sh`, `scripts/*evidence*.sh` |

## Safety and release boundaries

- Keep `SENDER_AUTH_KEY`, `CLAWSIDE_A2A_AUTH_KEY`, and Telegram bot tokens separate.
- Keep real config in `.env` and `configs/config.toml`; both are ignored by git.
- Do not put secrets, local absolute paths, private prompts, raw trajectory payloads, stdout/stderr, chat IDs, or bot tokens in public docs or release artifacts.
- `clawside` must not accept runtime launch fields such as arbitrary `command`, `args`, `cwd`, private prompt, token, session id, worker id, or sandbox fields through truth-plane templates or A2A surfaces.
- Release/tag/public work stays explicit: keep `./scripts/tag-release.sh --verify-only` unless the release/tag action is authorized.
- Before making the repository public, run `./scripts/github-readiness.sh <owner>/<repo>` and confirm secret scanning, push protection, private vulnerability reporting, branch protection or rulesets, and code scanning.
