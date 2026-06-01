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
- **MCP + skill v1 surface**: tools OpenClaw can install, register, and consume for handoffs, workflows, watches, repairs, agent coordination, and A2A delivery.
- **Agent coordination policy**: registry-backed work projections with heartbeat defaults, stale/offline owner signals, and expired lease suggestions.
- **A2A compatibility endpoint**: experimental Agent Card + JSON-RPC access for coordination queries, standard task status lookup, controlled idempotent inbound task creation, and read-only task event streaming.

This version now productizes the minimal v1 into an installable, registerable, and verifiable MCP server + skill suite.

## Operator and integrator guide

Clawside is a durable coordination bridge, not a swarm runtime. Clawside does not launch model workers, runtime sessions, or sandboxes; OpenClaw, Claude, Kimi, or another runtime remains responsible for model execution.

Use the project from three roles:

| Role | Use Clawside for | Primary entrypoints |
| --- | --- | --- |
| Local operator | Run local sender, MCP server, smoke checks, release evidence, and diagnostics. | `./start.sh`, `./scripts/start_mcp.sh`, `./scripts/verify_openclaw_mcp.sh` |
| External A2A client | Discover the Agent Card, create/query/cancel controlled tasks, and subscribe to read-only task events. | `cmd/clawside-a2a`, `cmd/clawside-a2a-example`, `./scripts/verify_clawside_a2a.sh` |
| Swarm/runtime integrator | Register agents, query `next_work` / `blocked_work`, progress handoffs, and consume dependency/reviewer gates. | MCP tools, `./scripts/verify_openclaw_mcp.sh --collaboration-template-smoke` |

Which verifier should I run?

| Situation | Command |
| --- | --- |
| Day-to-day local development | `./scripts/ci-local.sh clean` |
| Private validation/readiness aggregate | `./scripts/verify_private_readiness.sh` |
| Private real OpenClaw evidence closure | `./scripts/close_private_openclaw_external_runtime_evidence.sh --export-dir <export-dir>` |
| Final private/local closure checklist | `./scripts/final_closure_checklist.sh --external-runtime-evidence ./external-runtime-evidence.json --evidence-bundle ./release-evidence/<bundle-dir> --tag v0.0.0-dry-run --repo <owner>/<repo>` |
| A2A compatibility and external client readiness | `./scripts/verify_clawside_a2a.sh` |
| MCP tool surface and OpenClaw sidecar smoke | `SENDER_AUTH_KEY=<local-sender-key> ./scripts/verify_openclaw_mcp.sh` |
| Multi-project upstream/downstream/reviewer rehearsal | `./scripts/verify_openclaw_mcp.sh --profile private-coordination --json` |
| External runtime trajectory evidence dogfood | `./scripts/verify_openclaw_mcp.sh --profile external-runtime-evidence --sender-base-url "" --mcp-command "" --openclaw-external-runtime-evidence ./external-runtime-evidence.json --json` |
| Release-grade evidence verification | `SENDER_AUTH_KEY=<local-sender-key> ./scripts/verify_openclaw_mcp.sh --profile release-evidence` |
| GitHub public readiness before opening the repository | `./scripts/github-readiness.sh <owner>/<repo>` |

### Current integration status

- **Private dogfood rehearsal:** the current private dogfood path is documented as a repeatable local rehearsal. Treat it as private operational evidence for Clawside's truth-plane / MCP sidecar behavior, not as public release evidence.
- **External swarm/runtime integration guide:** this README now documents how an external runtime launches its own workers and uses Clawside as a coordination sidecar.
- **Release remains deferred:** until there is explicit authorization, do not make the repository public, do not change GitHub settings, do not create a release, and do not create or push a tag. Keep public-readiness checks read-only with `./scripts/github-readiness.sh <owner>/<repo>`.

### Private dogfood rehearsal sequence

Use this sequence to rehearse the private runtime path without publishing release evidence. Clawside records durable truth-plane state; it does not launch model workers, runtime sessions, or sandboxes.

```bash
./scripts/ci-local.sh clean
./scripts/verify_clawside_a2a.sh
./scripts/verify_openclaw_mcp.sh --profile private-coordination --json
./scripts/github-readiness.sh <owner>/<repo>
```

Expected results while the repository remains private:

- `./scripts/ci-local.sh clean`: local clean CI should be green.
- `./scripts/verify_clawside_a2a.sh`: A2A readiness should be green.
- `./scripts/verify_openclaw_mcp.sh --profile private-coordination --json`: MCP private coordination rehearsal should be green.
- `./scripts/github-readiness.sh <owner>/<repo>`: GitHub readiness can be expected red while the repository is private or required GitHub settings are disabled; do not report the repository as public-ready until this script exits 0.

The rehearsal covers the current truth-plane bridge behavior: agent registry, symbolic `project://...` references, `next_work`, `blocked_work`, reviewer gates, dependency gates, `workflow_status`, and `coordination_evidence_summary`. The `private-coordination` profile expands to the truth-plane-only coordination checks behind `--multi-agent-coordination-smoke`, `--collaboration-template-smoke`, `--external-runtime-smoke`, and `--private-multi-project-dogfood-smoke`, with sender checks and delivery disabled. It does not call `message/send` or `message/stream`, does not trigger sender or Telegram delivery, and does not accept runtime launch fields.

### External swarm/runtime integration sequence

An external swarm/runtime integrator should treat Clawside as a coordination sidecar. The runtime owns model execution, worker/session/sandbox lifecycle, scheduling, and task execution. Clawside owns durable truth, workflow/handoff state, ownership, watches, repair, divergence, dependency gates, reviewer gates, evidence, and A2A-compatible read/query/cancel surfaces.

Recommended sequence:

1. Register the Clawside MCP server in the runtime that owns execution:

```text
command: <repo-root>/scripts/start_mcp.sh
args: --db <repo-root>/sender.db
env: SENDER_AUTH_KEY=<local-sender-key>
```

2. Register runtime-owned agents with symbolic project references:

```text
agent_register actor=agent:planner project_refs=project://example/upstream capabilities=planning
agent_register actor=agent:implementer project_refs=project://example/downstream capabilities=implementation
agent_register actor=agent:reviewer project_refs=project://example/review capabilities=review
```

3. Create durable work with `collaboration_template_apply` or `handoff_create`. Templates and handoffs create workflow truth only; they must not accept `command`, `args`, local paths, private prompts, tokens, session ids, worker launch fields, sender delivery, or Telegram delivery.

4. Poll executable and blocked work:

```text
next_work agent_id=agent:planner project_ref=project://example/upstream
blocked_work project_ref=project://example/downstream
```

5. Let the runtime-owned worker progress the handoff:

```text
handoff_progress action=receive handoff_id=<handoff-id>
handoff_progress action=claim handoff_id=<handoff-id>
handoff_progress action=start handoff_id=<handoff-id>
handoff_progress action=checkpoint handoff_id=<handoff-id>
handoff_progress action=complete handoff_id=<handoff-id>
```

Use `submit`, `review`, `request_revision`, and `approve` for reviewer-gated workflows. Use protocol actions, not projected state names such as `started` or `completed`.

6. Read final truth and evidence:

```text
handoff_get handoff_id=<handoff-id>
workflow_status workflow_id=<workflow-id>
coordination_evidence_summary workflow_id=<workflow-id> include_agents=true
```

To exercise the runtime-owned loop locally without launching workers, run:

```bash
./scripts/verify_openclaw_mcp.sh --sender-base-url "" --collaboration-template-smoke --external-runtime-smoke --json
```

#### Minimal external runtime sample

`cmd/clawside-external-runtime-sample` is a minimal truth-plane-only sample for runtimes such as Claude, Kimi, OpenClaw, or another swarm. It registers runtime-owned agents with symbolic refs (`project://sample/external-runtime/upstream`, `project://sample/external-runtime/downstream`, `project://sample/external-runtime/review`), creates upstream and downstream handoffs, checks `next_work` and `blocked_work`, progresses protocol actions, then reads `workflow_status` and `coordination_evidence_summary`.

```bash
go run ./cmd/clawside-external-runtime-sample --db ./sender.db
```

Safety boundaries: the sample does not launch model workers, does not start runtime sessions, does not start sandboxes, does not trigger sender or Telegram delivery, and does not accept arbitrary command/args/cwd/local path/private prompt/token/session/worker launch fields.

#### Real OpenClaw trajectory external runtime evidence dogfood validation

Use this path when the external runtime has run outside Clawside and exported an OpenClaw trajectory `events.jsonl`. `cmd/openclaw-external-runtime-evidence-extract/` reads bounded envelope metadata plus Clawside MCP tool results from that trajectory, writes evidence with `schema_version=p37.external-runtime-trajectory.v1` and `trajectory_provenance.source_kind=openclaw_events_jsonl_export`, and requires at least one non-Clawside trajectory event; it does not store or print raw trajectory payloads, replay payloads, or launch anything.

Recommended sequence:

1. Run the external runtime outside Clawside. The runtime owns workers, sessions, sandboxes, scheduling, model execution, and task execution.
2. From that runtime, register/use Clawside MCP tools such as `agent_register`, `handoff_create`, `next_work`, `blocked_work`, `handoff_progress`, `workflow_status`, and `coordination_evidence_summary`.
3. Export the OpenClaw trajectory events as `events.jsonl`.
4. Extract the bounded evidence JSON with read-only provenance:

```bash
./scripts/extract_openclaw_external_runtime_evidence.sh --events <events-jsonl> --output ./external-runtime-evidence.json
```

5. Validate the evidence read-only:

```bash
./scripts/verify_openclaw_mcp.sh \
  --profile external-runtime-evidence \
  --sender-base-url "" \
  --mcp-command "" \
  --openclaw-external-runtime-evidence ./external-runtime-evidence.json \
  --json
```

Or run the one-step local dogfood wrapper, which performs the same extraction and read-only validation:

```bash
./scripts/dogfood_openclaw_external_runtime_evidence.sh \
  --events <events-jsonl> \
  --output ./external-runtime-evidence.json
```

The preflight finder adds a read-only check for local OpenClaw exports. It scans only the ignored repo-local path `.openclaw/trajectory-exports/<export-dir>/events.jsonl`, prints only bounded file metadata and the next command, and does not print raw trajectory payloads:

```bash
./scripts/preflight_openclaw_external_runtime_evidence.sh
```

After selecting an export, ask preflight to show the exact end-to-end wrapper command:

```bash
./scripts/preflight_openclaw_external_runtime_evidence.sh \
  --events ./.openclaw/trajectory-exports/<export-dir>/events.jsonl \
  --output ./external-runtime-evidence.json
```

The suitability report runs between preflight and the dogfood wrapper. It uses `schema_version=p40.external-runtime-suitability.v1` and reports `suitable=true` only when the trajectory has the required Clawside MCP tool chain, lifecycle gates, non-Clawside trajectory observation, lifecycle order, and no forbidden launch or delivery tools:

```bash
./scripts/report_openclaw_external_runtime_evidence_suitability.sh \
  --events ./.openclaw/trajectory-exports/<export-dir>/events.jsonl
```

The suitability report lists only bounded `missing_tools`, `missing_gates`, `forbidden_tools`, counts, observed event types, and a placeholder next command. It does not print raw trajectory payloads, does not launch runtime/session/sandbox/model workers, and does not trigger sender/Telegram delivery. If the report returns `suitable=true`, run the dogfood wrapper explicitly for the actual extraction plus read-only validation step:

```bash
./scripts/dogfood_openclaw_external_runtime_evidence.sh \
  --events ./.openclaw/trajectory-exports/<export-dir>/events.jsonl \
  --output ./external-runtime-evidence.json
```

Repeatable real-export workflow packages the real OpenClaw rerun without turning Clawside into an OpenClaw launcher. Run it without arguments to print the sanitized checklist, then follow the checklist steps to Run OpenClaw externally, export redacted trajectory data to `.openclaw/trajectory-exports/<export-dir>/events.jsonl`, and run the same script with explicit paths:

```bash
./scripts/rerun_openclaw_external_runtime_evidence_workflow.sh
./scripts/rerun_openclaw_external_runtime_evidence_workflow.sh \
  --events ./.openclaw/trajectory-exports/<export-dir>/events.jsonl \
  --output ./external-runtime-evidence.json
```

The script runs local bounded verification only after an export exists: preflight, then the suitability report, then the dogfood wrapper only when `suitable=true`. If the trajectory is not suitable, it prints the bounded gap report and `dogfood wrapper was not run`. It does not launch OpenClaw/Claude/Kimi runtime, sessions, sandboxes, or model workers; it does not trigger sender/Telegram delivery; and it uses only placeholders in public docs.

The private validation/readiness aggregate is for local pre-public checks:

```bash
./scripts/verify_private_readiness.sh
```

It runs `./scripts/ci-local.sh clean`, `./scripts/verify_clawside_a2a.sh`, `./scripts/verify_openclaw_mcp.sh --profile private-coordination --json`, read-only fixture validation with `--profile external-runtime-evidence` and `testdata/openclaw-smoke/stage0-5/external-runtime-evidence.json`, and the `./scripts/rerun_openclaw_external_runtime_evidence_workflow.sh` checklist. It does not make the repository public, does not create tags or releases, does not push, does not change GitHub settings, does not trigger sender/Telegram delivery, and does not launch OpenClaw/Claude/Kimi runtimes, sessions, sandboxes, or model workers. GitHub readiness remains explicit and read-only via `./scripts/github-readiness.sh <owner>/<repo>`.

The private real OpenClaw external-runtime evidence loop closes before public/release. First run OpenClaw externally and export a redacted trajectory to `.openclaw/trajectory-exports/<export-dir>/events.jsonl`, then run:

```bash
./scripts/close_private_openclaw_external_runtime_evidence.sh --export-dir <export-dir>
```

The script derives repo-local events/output paths and runs `./scripts/verify_private_readiness.sh`, then `./scripts/rerun_openclaw_external_runtime_evidence_workflow.sh --events ./.openclaw/trajectory-exports/<export-dir>/events.jsonl --output ./external-runtime-evidence.json`. It does not make the repository public, does not create tags or releases, does not push, does not mutate GitHub settings, does not launch OpenClaw/Claude/Kimi runtimes, sessions, sandboxes, or model workers, does not trigger sender/Telegram delivery, and does not accept arbitrary `--events` / `--output`, command/path/prompt/token/session/worker/sender/Telegram fields.

### Final private/local closure checklist

For private/local closure before any public release work, run:

```bash
./scripts/final_closure_checklist.sh \
  --external-runtime-evidence ./external-runtime-evidence.json \
  --evidence-bundle ./release-evidence/<bundle-dir> \
  --tag v0.0.0-dry-run \
  --repo <owner>/<repo>
```

This composes `./scripts/public_readiness_dry_run.sh` and `./scripts/release_evidence_dry_run.sh`. It is a dry-run checklist only: it does not make the repository public, does not push, does not create tags or releases, does not mutate GitHub settings, does not launch runtimes, and does not trigger sender/Telegram delivery. A private repository can still report `PUBLIC_READINESS_GAP` until GitHub public-readiness settings are available and explicitly configured outside this script.

The evidence contract records only IDs, the required tool set, `schema_version`, `trajectory_provenance`, `no_sender_delivery=true`, and `no_runtime_launch_by_clawside=true`; it also requires dependency, reviewer, downstream-ready, completed-workflow, `coordination_evidence_summary`, `non_clawside_event_count > 0`, and `lifecycle_order_verified=true` gates. This flow does not launch model workers, does not start runtime sessions, does not start sandboxes, does not trigger sender delivery, does not trigger Telegram delivery, does not store or print raw trajectory payloads, and does not accept arbitrary commands/local paths/private prompts/tokens/sessions/stdout/stderr/chat IDs/worker/runtime/sandbox launch fields. `SENDER_AUTH_KEY` and `CLAWSIDE_A2A_AUTH_KEY` remain separate and are not reused by this dogfood path.

Before making any public-readiness or release claim, keep verification read-only:

```bash
./scripts/github-readiness.sh <owner>/<repo>
```

### Private Telegram operator entrypoint

`cmd/clawside-telegram-operator` is a separate inbound long-polling operator for private dogfood. It opens the same truth-plane SQLite DB, reads the configured Telegram bot token and allowlist, accepts only allowlisted Telegram users in a private chat, and replies with bounded hand-written summaries.

Help is available without config, DB, network, or sender auth:

```bash
go run ./cmd/clawside-telegram-operator help
```

Start the operator with an already generated `configs/config.toml`:

```bash
CLAWSIDE_TELEGRAM_OPERATOR_BOT=guardian \
CLAWSIDE_TELEGRAM_OPERATOR_DB_PATH=./sender.db \
CLAWSIDE_TELEGRAM_OPERATOR_BASE_URL=https://api.telegram.org \
go run ./cmd/clawside-telegram-operator --config configs/config.toml
```

Supported Telegram slash commands:

```text
/health
/status <workflow_id>
/next <agent_id>
/blocked <agent_id>
/approve <handoff_id>
```

For a repeatable private dogfood run, start the operator with the lifecycle script, seed a submitted review handoff, then approve it from a Telegram private chat:

```bash
./scripts/start_telegram_operator.sh --bot guardian
go run ./cmd/clawside-dogfood-seed --db ./sender.db --reviewer user:telegram:<user_id>
# Send the printed /status <workflow_id> and /approve <handoff_id> commands in Telegram.
./scripts/stop_telegram_operator.sh
```

Safety boundaries: this operator does not use `SENDER_AUTH_KEY`, does not call `message/send` or `message/stream`, does not start model workers, runtime sessions, or sandboxes, and does not accept arbitrary `command`, `args`, `cwd`, local paths, private prompts, tokens, sessions, stdout, stderr, sender delivery fields, or Telegram delivery fields. `/approve` records a protocol approval as `user:telegram:<user_id>`.

## Minimal usage path

1. Generate the local sender config:

```bash
cp .example.env .env
# edit .env and set SENDER_AUTH_KEY to a local random key
./scripts/config_builder.sh
```

2. Build and start the sender:

```bash
./build.sh
./start.sh
```

3. Register the clawside MCP server in OpenClaw:

```text
command: <repo-root>/scripts/start_mcp.sh
args: --db <repo-root>/sender.db
env: SENDER_AUTH_KEY=<local-sender-key>
```

4. Run the local read-only verifier:

```bash
SENDER_AUTH_KEY=<local-sender-key> ./scripts/verify_openclaw_mcp.sh
```

5. Generate the coordination evidence summary, then build and verify a release evidence bundle from OpenClaw trajectory:

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

6. Optional tag preflight only. Release remains deferred, so keep `--verify-only` unless there is explicit authorization:

```bash
./scripts/tag-release.sh --verify-only --evidence-bundle <bundle-dir> vX.Y.Z
```

Do not remove `--verify-only`, create a release, make the repository public, change GitHub settings, or create/push a tag without explicit authorization.

## Components

- `cmd/config-builder/`: Go CLI that generates the derived sender config.
- `cmd/orchestrator/`: low-level orchestrator debug / operation entrypoint.
- `cmd/clawside-mcp/`: stdio MCP server entrypoint.
- `cmd/clawside-a2a/`: experimental A2A compatibility HTTP endpoint for controlled queries, inbound task creation, and read-only task events.
- `cmd/clawside-telegram-operator/`: private inbound Telegram operator for fixed truth-plane slash commands.
- `cmd/clawside-dogfood-seed/`: local private dogfood seed CLI for generating review handoffs for Telegram approval.
- `cmd/openclaw-mcp-smoke/`: local smoke verifier for OpenClaw consuming the clawside MCP v1 surface.
- `cmd/openclaw-dispatch/`: local helper that adapts `handoff_dispatch adapter=openclaw` requests to an OpenClaw-compatible CLI command.
- `cmd/openclaw-tool-results-extract/`: local read-only CLI for extracting clawside tool structured results from OpenClaw trajectory.
- `cmd/openclaw-truth-plane-extract/`: local read-only CLI for extracting minimal truth-plane handoff/workflow/watch/ownership validation results from OpenClaw trajectory.
- `cmd/openclaw-truth-plane-progression-extract/`: local read-only CLI for extracting completed handoff progression validation results from OpenClaw trajectory.
- `cmd/openclaw-truth-plane-mutation-extract/`: local read-only CLI for extracting watch / ownership mutation validation results from OpenClaw trajectory.
- `cmd/openclaw-truth-plane-repair-extract/`: local read-only CLI for extracting repair invalidate/backfill replay validation results from OpenClaw trajectory.
- `cmd/openclaw-truth-plane-reopen-extract/`: local read-only CLI for extracting divergence/candidate/reopen handoff validation results from OpenClaw trajectory.
- `cmd/openclaw-truth-plane-divergence-extract/`: local read-only CLI for extracting divergence/candidate/E2E completed truth validation results from OpenClaw trajectory.
- `cmd/openclaw-truth-plane-continuity-extract/`: local read-only CLI for extracting truth-plane continuity validation results after reopening and continuing the same handoff from OpenClaw trajectory.
- `cmd/openclaw-truth-plane-delivery-extract/`: local read-only CLI for extracting handoff + A2A delivery sender job validation results from OpenClaw trajectory.
- `cmd/a2a-delivery/`: A2A delivery bridge CLI.
- `internal/configbuilder/`: extracts the minimal sender config from OpenClaw source config.
- `internal/orchestrator/`: handoff, workflow, event, watch, repair, and adapter foundations.
- `internal/toolserver/`: MCP tool handlers.
- `internal/a2aserver/`: Agent Card and controlled JSON-RPC compatibility server.
- `internal/a2adelivery/`: A2A delivery bridge, polling, and orchestration logic.
- `main.go` + `http_handler.go` + `worker.go`: sender service entrypoint, HTTP API, and delivery worker.
- `.claude/skills/openclaw-a2a-delivery/`: OpenClaw A2A delivery skill definition.

## Installation and configuration

### 1. Generate the derived sender config

Recommended setup from OpenClaw config:

```bash
cp .example.env .env
# edit .env and set SENDER_AUTH_KEY to a local random key
./scripts/config_builder.sh
```

Local scripts load `.env` automatically without overriding explicit environment variables or CLI flags. By default, this reads `~/.openclaw/openclaw.json` and writes:

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

Notes:

- `./build.sh`: builds the root sender binary at `./clawside`.
- `./scripts/secret-scan.sh`: scans tracked files and optional git history for high-risk secrets, with redacted output that does not print full secrets.
- `./scripts/ci-local.sh clean`: builds a temporary clean directory from tracked files, then runs secret scan, gofmt, vet, tests, and build.
- `./scripts/install-hooks.sh`: installs this repository's `.git/hooks/pre-push`, which runs clean local CI before push by default.
- `./scripts/tag-release.sh --help`: shows local release tag guard usage; read the Stage 8 local release guard notes before creating a tag.
- `./start.sh`: starts the sender service in the background and writes `logs/sender.pid` plus `logs/sender.log`.
- `./stop.sh`: stops the sender process recorded by `./start.sh`.
- `./restart.sh`: runs `./stop.sh` and then `./start.sh`.
- `./scripts/start.sh`: starts the sender in the foreground through `go run .`, useful for debugging or avoiding a binary build.
- `./scripts/start_mcp.sh`: starts the stdio MCP server for OpenClaw registration.
- `./scripts/extract_openclaw_tool_results.sh`: extracts clawside sender tool structured results from OpenClaw trajectory `events.jsonl`.
- `./scripts/extract_openclaw_truth_plane_results.sh`: extracts minimal truth-plane validation results from OpenClaw trajectory `events.jsonl`.
- `./scripts/extract_openclaw_truth_plane_progression_results.sh`: extracts completed progression validation results from OpenClaw trajectory `events.jsonl`.
- `./scripts/extract_openclaw_truth_plane_mutation_results.sh`: extracts watch / ownership mutation validation results from OpenClaw trajectory `events.jsonl`.
- `./scripts/extract_openclaw_truth_plane_repair_results.sh`: extracts repair invalidate/backfill replay validation results from OpenClaw trajectory `events.jsonl`.
- `./scripts/extract_openclaw_truth_plane_reopen_results.sh`: extracts divergence/candidate/reopen handoff validation results from OpenClaw trajectory `events.jsonl`.
- `./scripts/extract_openclaw_truth_plane_divergence_results.sh`: extracts divergence/candidate/E2E completed truth validation results from OpenClaw trajectory `events.jsonl`.
- `./scripts/extract_openclaw_truth_plane_continuity_results.sh`: extracts truth-plane continuity validation results after reopening and continuing the same handoff from OpenClaw trajectory `events.jsonl`.
- `./scripts/extract_openclaw_truth_plane_delivery_results.sh`: extracts handoff + A2A delivery sender job validation results from OpenClaw trajectory `events.jsonl`.

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

`/jobs` and MCP `sender_job_list` use sender-internal status names: `pending`, `sending`, `retry`, `failed`, and `sent`. `retrying` in A2A bridge output is a delivery-result semantic status that maps to sender `pending` / `sending` / `retry` queue states.

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
- `agent_register`
- `agent_list`
- `next_work`
- `blocked_work`
- `collaboration_template_list`
- `collaboration_template_apply`
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

Recommended reading:

- `handoff_create`: creates a new handoff / workflow starting point.
- `handoff_get`: returns current handoff truth and timeline.
- `handoff_dispatch`: records a dispatch attempt and transport request so pure MCP flows can dispatch before receiving.
- `handoff_progress`: applies protocol actions such as `receive`, `claim`, `start`, `submit`, `approve`, and `complete`.
- `workflow_status`: returns one workflow aggregate view.
- `workflow_list`: lists all workflows with projected handoffs.
- `agent_register`: registers or updates an agent's capabilities and heartbeat; omitted heartbeat defaults to server time.
- `agent_list`: lists registered agents by capability, project ref, task kind, or status.
- `next_work`: lists executable handoffs and includes non-blocking liveness / lease warnings plus suggestions.
- `blocked_work`: lists handoffs with deterministic dependency, watch, reviewer, liveness, and lease reasons plus suggestions.
- `collaboration_template_list`: lists built-in truth-plane templates (`upstream_downstream_review`, `review_gate`, `fanout_review`) with graph pattern, roles, dependencies, acceptance criteria, and safety boundaries; `fanout_review` models `upstream -> {downstream, reviewer}`.
- `collaboration_template_apply`: applies a built-in template by creating durable workflow / handoff / dependency / watch records only. It accepts an optional `idempotency_key`; retrying with the same normalized payload returns the original workflow / handoffs with `replayed: true`, while reusing the key with a changed payload is rejected.
- `watch_list`: lists watches attached to a handoff.
- `watch_run`: runs due watch checks at a provided RFC3339 timestamp.
- `watch_update`: updates a watch deadline, status, or escalation policy.
- `ownership_get`: returns the ownership binding for a handoff.
- `ownership_update`: updates handoff ownership fields and keeps the ownership binding synchronized.
- `repair_list`: lists repair records, optionally filtered by handoff.
- `repair_invalidate_event`: invalidates an accepted event and replays handoff truth.
- `repair_backfill_event`: backfills an accepted event and replays handoff truth.
- `repair_reopen_handoff`: reopens a terminal handoff and replays truth.
- `repair_candidate_list`: lists repair candidates for a handoff.
- `divergence_record`: records an observer divergence signal for a handoff.
- `divergence_list`: lists observer divergence hints for a handoff.
- `sender_health`: checks sender process health.
- `sender_ready`: checks whether the sender is ready to process delivery work.
- `sender_stats`: returns sender queue counts and worker timing fields.
- `sender_job_list`: lists sender jobs by status with a bounded limit.
- `sender_job_get`: returns a single sender job status.
- `a2a_deliver`: performs real outward delivery through the existing sender bridge after resolving `target_agent` through built-in or configured mapping.

Boundaries:

- This is a minimal v1 tool surface, not the full truth-plane MCP product surface.
- Deeper truth-plane operations beyond invalidate/backfill/reopen repair still live outside the v1 MCP surface.
- `a2a_deliver` and `sender_*` observability tools depend on the local sender sidecar.
- `sender_*` observability tools are read-only and do not expose raw message text, raw idempotency keys, or Telegram bot tokens.
- `handoff_*` / `workflow_*` / `agent_*` / `*_work` / `watch_*` / `ownership_get` / `repair_*` / `divergence_list` depend on `--db` pointing to the same sqlite truth store.
- Agent liveness and lease policy are projection signals only: they do not launch workers, execute commands, clear owners, steal leases, or mutate handoff lifecycle state automatically.
- Collaboration templates are built-in and truth-plane-only: catalog metadata is descriptive, and apply creates durable workflow / handoff records without accepting command, args, local paths, prompts, tokens, session ids, worker launch fields, sender delivery, or Telegram delivery.

To register with OpenClaw, configure it as a stdio MCP server and point `command` to this repository's `scripts/start_mcp.sh`.

## A2A compatibility endpoint

`clawside-a2a` is an experimental compatibility endpoint for A2A-style discovery, coordination queries, standard task status lookup, controlled inbound task creation, and read-only task event streaming. It exposes an Agent Card, a small JSON-RPC allowlist, and an SSE task stream backed by the same sqlite truth store as the MCP server.

Start it with a separate A2A auth key:

```bash
CLAWSIDE_A2A_AUTH_KEY=<local-a2a-key> \
  go run ./cmd/clawside-a2a --db ./sender.db --addr 127.0.0.1:8789
```

Show command help:

```bash
go run ./cmd/clawside-a2a --help
```

After the server is running, verify the endpoint as an external A2A client:

```bash
CLAWSIDE_A2A_AUTH_KEY=<local-a2a-key> \
  go run ./cmd/clawside-a2a self-test --base-url http://127.0.0.1:8789
```

`self-test` fetches the Agent Card, calls JSON-RPC with bearer auth, creates one controlled inbound task, reads `tasks/get`, reads the SSE task stream, then calls `tasks/cancel`. It only writes one local truth-plane self-test handoff and marks it failed; it does not launch runtime sessions, workers, sender delivery, or Telegram delivery.

For a repeatable local or CI readiness gate, run the wrapper script instead:

```bash
./scripts/verify_clawside_a2a.sh
```

The wrapper builds temporary A2A binaries, creates a temporary sqlite DB and A2A auth key, starts a localhost server, runs `self-test`, runs the external example client, and cleans up. It uses only the local truth-plane endpoint and does not launch runtime sessions, workers, sender delivery, or Telegram delivery. For external client adoption evidence, this wrapper is the local compatibility readiness path; broader release readiness still uses the existing OpenClaw MCP smoke and release evidence profiles.

For release-before contract evidence, validate the sanitized fixture at `testdata/openclaw-smoke/stage0-5/a2a-contract-results.json` with `scripts/verify_openclaw_mcp.sh --openclaw-a2a-contract-results <sanitized-a2a-contract-results.json>`. That path validates the Agent Card, JSON-RPC method matrix, task projections, SSE contract, and safety boundaries without adding A2A contract output to the default release evidence bundle.

For a runnable external-client-shaped Go example, use `clawside-a2a-example` against a running endpoint:

```bash
CLAWSIDE_A2A_AUTH_KEY=<local-a2a-key> \
  go run ./cmd/clawside-a2a-example --base-url http://127.0.0.1:8789
```

The example uses only Agent Card discovery, `/a2a/rpc`, `tasks/get`, path-based SSE, and `tasks/cancel`. It creates one controlled truth-plane handoff and cancels it by default; it does not launch workers, sessions, sender delivery, Telegram delivery, or local commands.

External client minimum checklist:

- Read `/.well-known/agent-card.json` first and require the advertised endpoint hints before calling RPC.
- Use bearer auth from `CLAWSIDE_A2A_AUTH_KEY` or an equivalent secret store; do not pass A2A auth keys on argv.
- Treat `/a2a/rpc` as the only JSON-RPC endpoint and `/a2a/tasks/{handoffID}/events` as the only SSE endpoint.
- Implement only the advertised method matrix. Do not call `message/send`, `message/stream`, push-notification methods, raw `handoff_create`, runtime/session/sandbox/worker APIs, sender delivery, or Telegram delivery.
- On SSE reconnect, call `tasks/get` first, then resubscribe; do not depend on historical replay from `Last-Event-ID`.
- Keep logs sanitized: do not log auth keys, raw request bodies, command/args/local paths, private prompts/tokens, stdout/stderr, sender jobs, or delivery jobs.

External client troubleshooting:

| Diagnostic prefix | Likely cause | Safe next step |
| --- | --- | --- |
| `auth check: CLAWSIDE_A2A_AUTH_KEY is required` | Client process has no A2A bearer key in env. | Set the env var from a secret store; do not add an argv flag. |
| `auth check: server rejected bearer auth` | Key is wrong for this endpoint, missing on the server, or hitting a different service. | Verify the server key and base URL without printing the key. |
| `base_url check` | Base URL is not a valid `http` or `https` URL. | Fix scheme/host and remove query/fragment. |
| `base_url/connectivity check` | Endpoint is down, wrong port/path, DNS/proxy issue, or timeout. | Check `/healthz` and rerun `./scripts/verify_clawside_a2a.sh` locally. |
| `agent_card check: unsupported metadata` | The endpoint is not the expected Clawside A2A compatibility surface or advertises unsupported methods. | Compare the Agent Card with the method matrix below. |
| `rpc check` | JSON-RPC transport succeeded but the method returned HTTP/RPC/malformed-result failure. | Use the safe `error.data.code` and method name; do not log request bodies. |
| `sse check` | Task event stream returned bad status/content-type, timed out, or emitted malformed data. | Refresh with `tasks/get`, then resubscribe to the path-based SSE endpoint. |

External client sequence:

1. Fetch the public Agent Card. Its `metadata` block advertises endpoint hints, the stable method matrix, SSE guidance, and safety boundaries.

   ```bash
   curl -sS http://127.0.0.1:8789/.well-known/agent-card.json
   ```

2. Call JSON-RPC methods through `/a2a/rpc` with bearer auth:

   ```bash
   curl -sS http://127.0.0.1:8789/a2a/rpc \
     -H "Authorization: Bearer $CLAWSIDE_A2A_AUTH_KEY" \
     -H "Content-Type: application/json" \
     -d '{"jsonrpc":"2.0","id":"1","method":"clawside.workflow.list","params":{}}'
   ```

3. Read standard-style task status for a Clawside handoff with `tasks/get`:

   ```bash
   curl -sS http://127.0.0.1:8789/a2a/rpc \
     -H "Authorization: Bearer $CLAWSIDE_A2A_AUTH_KEY" \
     -H "Content-Type: application/json" \
     -d '{"jsonrpc":"2.0","id":"1","method":"tasks/get","params":{"id":"hf_example","historyLength":0}}'
   ```

4. Subscribe to read-only task projection events for a Clawside handoff:

   ```bash
   curl -N http://127.0.0.1:8789/a2a/tasks/hf_example/events?historyLength=1 \
     -H "Authorization: Bearer $CLAWSIDE_A2A_AUTH_KEY" \
     -H "Accept: text/event-stream"
   ```

5. On reconnect, call `tasks/get` first to refresh the current task snapshot, then resubscribe to the SSE stream. The stream sends `retry: 3000` and an `id:` cursor for the current truth-plane projection, but it does not guarantee historical replay from `Last-Event-ID`.

Create one controlled inbound task. This creates one root workflow / handoff and returns the same task view that `tasks/get` can later read:

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

Cancel a controlled inbound task. This marks the corresponding Clawside handoff failed in the truth-plane only; it does not stop processes, sessions, workers, or sender delivery:

```bash
curl -sS http://127.0.0.1:8789/a2a/rpc \
  -H "Authorization: Bearer $CLAWSIDE_A2A_AUTH_KEY" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":"1","method":"tasks/cancel","params":{"id":"hf_example"}}'
```

Method matrix:

| Method | Transport | Mode | Notes |
| --- | --- | --- | --- |
| `clawside.workflow.list` | JSON-RPC `/a2a/rpc` | read | List workflows with projected handoffs. |
| `clawside.workflow.status` | JSON-RPC `/a2a/rpc` | read | Read one workflow and projected handoffs. |
| `clawside.handoff.get` | JSON-RPC `/a2a/rpc` | read | Read current handoff truth and timeline. |
| `clawside.agent.list` | JSON-RPC `/a2a/rpc` | read | List registered agents by safe filters. |
| `clawside.work.next` | JSON-RPC `/a2a/rpc` | read | List executable handoffs for an agent/filter. |
| `clawside.work.blocked` | JSON-RPC `/a2a/rpc` | read | List blocked handoffs with reasons and suggestions. |
| `clawside.task.create` | JSON-RPC `/a2a/rpc` | controlled write | Create one idempotent inbound workflow/handoff without runtime or delivery execution. |
| `tasks/cancel` | JSON-RPC `/a2a/rpc` | controlled write | Mark the corresponding Clawside handoff failed in the truth-plane only; does not stop processes, sessions, workers, or sender delivery. |
| `tasks/get` | JSON-RPC `/a2a/rpc` | read | Return an A2A-style task projection for a Clawside handoff id. |
| `tasks/events` | SSE `/a2a/tasks/{handoffID}/events` | stream | Path-based stream only; not a JSON-RPC method. |

JSON-RPC contract: unsupported methods return `-32601`; invalid or unsafe params return `-32602`; parse/invalid request errors use JSON-RPC standard error codes; missing or wrong bearer auth fails at HTTP `401/403` before dispatch. JSON-RPC error objects include a safe machine-readable `error.data.code` such as `parse_error`, `invalid_request`, `method_not_found`, `invalid_params`, `not_found`, or `internal_error`; missing caller-provided workflow/handoff/task ids map to `-32602` with `not_found`. Error payloads are stable and do not echo command, args, local paths, private prompts, tokens, stdout/stderr, sender jobs, delivery jobs, raw SQL/internal errors, or missing resource ids.

Task status projection is stable: `created`, `dispatched`, and `submitted` map to `submitted`; `received`, `claimed`, `started`, `checkpointed`, and `reviewed` map to `working`; `completed` maps to `completed`; `failed` and `expired` map to `failed`.

Boundaries: this endpoint only allows controlled truth-plane mutations (`clawside.task.create` and `tasks/cancel`) plus read-only task event streaming. `tasks/cancel` only marks the corresponding handoff failed; it does not stop runtime processes, managed sessions, workers, sender delivery, or Telegram delivery. It does not implement the full Google A2A message runtime, raw `handoff_create`, `message/send`, push notifications, sandboxing, managed OpenClaw / Claude sessions, command / args / local path / runtime session / private prompt / worker launch parameters, sender delivery, or dynamic mutating JSON-RPC mapping.

## OpenClaw MCP smoke verifier

`openclaw-mcp-smoke` is a local, repeatable smoke verifier for OpenClaw consuming the clawside MCP v1 surface. It checks local config, sender `healthz` / `readyz` / `stats`, the MCP tool list, and optional real `target_agent=main` delivery through MCP `a2a_deliver`.

By default, it never modifies OpenClaw / Claude config, never calls Telegram directly, and never performs real delivery. It only prints read-only MCP registration guidance. Prefer passing secrets through the `SENDER_AUTH_KEY` environment variable, and do not paste secrets into shared logs.

Basic check:

```bash
SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh
```

For machine-readable output:

```bash
SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh --json
```

To validate the built-in collaboration template catalog and the multi-project agent rehearsal path, enable the opt-in template smoke. It checks `upstream_downstream_review`, `review_gate`, and `fanout_review` catalog metadata, registers upstream/downstream/reviewer agents on symbolic `project://...` refs, applies `upstream_downstream_review`, verifies project-filtered `next_work` / `blocked_work` dependency gating, validates downstream readiness after upstream completion, validates reviewer readiness after downstream completion, and checks idempotent template replay. This is truth-plane coordination evidence only: it does not call `message/send` or `message/stream`, send push notifications, launch runtime sessions/sandboxes/workers, trigger sender/Telegram delivery, or accept command/args/local paths/private prompts/session IDs/tokens/job IDs.

```bash
SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh --collaboration-template-smoke
```

To run a real OpenClaw dispatch smoke through `handoff_dispatch adapter=openclaw`, point the MCP server at the local `openclaw-dispatch` helper and choose an agent that exists in your OpenClaw config. This invokes a real `openclaw agent` run, so it can consume model quota and write local OpenClaw session state:

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

To include an OpenClaw-side read-only tool call checklist, pass `--openclaw-tool-call-checklist`. It only describes manual calls OpenClaw should make from its runtime / session for `sender_health`, `sender_ready`, and `sender_stats`, plus how to judge the returned results; it does not execute or fake OpenClaw calls:

```bash
SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh --openclaw-tool-call-checklist
```

After manually calling the three tools from the OpenClaw runtime / session according to the checklist, prefer extracting the original `structuredContent` from OpenClaw trajectory `events.jsonl`, then pass it to `--openclaw-tool-results` for read-only validation. This path does not execute or fake OpenClaw calls, and it does not depend on whether the final TG / dashboard reply exposes the raw structured result:

```bash
openclaw sessions export-trajectory --agent main --session-key '<session-key>' --json

./scripts/extract_openclaw_tool_results.sh \
  --events .openclaw/trajectory-exports/<export-dir>/events.jsonl \
  --output /tmp/openclaw-tool-results.json

SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --openclaw-tool-results /tmp/openclaw-tool-results.json
```

### Minimal truth-plane tool-chain validation

Ask OpenClaw web / TG to make one real tool-chain call sequence and verify OpenClaw can consume handoff truth, workflow status, watch list, and ownership binding:

```text
Please create one test handoff through the registered clawside MCP tools, then query its handoff truth, workflow status, watch list, and ownership binding.

Call these tools in order:
1. handoff_create
2. handoff_get
3. workflow_status
4. watch_list
5. ownership_get

Use these creation parameters:
workflow_kind=manual_openclaw_truth_plane_smoke
sender=agent:main
receiver=agent:planner
task_kind=truth_plane_smoke
intent=verify OpenClaw can consume clawside truth-plane tools

After the calls complete, output handoff_id and workflow_id.
```

After the calls complete, export the trajectory, extract truth-plane validation results, and pass them to the smoke verifier for read-only validation:

```bash
openclaw sessions export-trajectory --agent main --session-key '<session-key>' --json

./scripts/extract_openclaw_truth_plane_results.sh \
  --events .openclaw/trajectory-exports/<export-dir>/events.jsonl \
  --output /tmp/openclaw-truth-plane-results.json

SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --openclaw-truth-plane-results /tmp/openclaw-truth-plane-results.json
```

To validate that OpenClaw really progresses a handoff state, ask the main agent to create a handoff and progress it to `completed`, then extract and validate the trajectory result:

```text
Please create one test handoff through the registered clawside MCP tools, progress it all the way to completed, then query the final handoff truth and workflow status.

Call these tools in order:
1. handoff_create
2. handoff_dispatch
3. handoff_progress action=receive
4. handoff_progress action=claim
5. handoff_progress action=start
6. handoff_progress action=checkpoint
7. handoff_progress action=complete
8. handoff_get
9. workflow_status

Use these creation parameters:
workflow_kind=manual_openclaw_truth_plane_progression_smoke
sender=agent:main
receiver=agent:planner
task_kind=truth_plane_progression_smoke
required_for_workflow_completion=true
intent=verify OpenClaw can progress clawside truth-plane handoff state

Use these dispatch parameters:
handoff_id=<created handoff_id>
adapter=manual
target=agent:planner

Use the receiver as the progress actor: agent:planner.

After the calls complete, output handoff_id, workflow_id, final handoff state, and final workflow status.
```

```bash
openclaw sessions export-trajectory --agent main --session-key '<session-key>' --json

./scripts/extract_openclaw_truth_plane_progression_results.sh \
  --events .openclaw/trajectory-exports/<export-dir>/events.jsonl \
  --output /tmp/openclaw-truth-plane-progression-results.json

SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --openclaw-truth-plane-progression-results /tmp/openclaw-truth-plane-progression-results.json
```

### Stage 2 truth-plane mutation validation

To validate that OpenClaw really mutates watch and ownership state, ask the main agent to create a handoff, update watch and ownership fields, then extract and validate the trajectory result:

```text
Please create one test handoff through the registered clawside MCP tools, mutate watch and ownership state for the same handoff, then query the final results.

Call these tools in order:
1. handoff_create
2. watch_list
3. watch_update
4. ownership_update
5. watch_list
6. ownership_get

Use these creation parameters:
workflow_kind=manual_openclaw_truth_plane_mutation_smoke
sender=agent:main
receiver=agent:planner
task_kind=truth_plane_mutation_smoke
intent=verify OpenClaw can mutate clawside truth-plane watch and ownership state

Use the created handoff_id for the first watch_list call.

For watch_update, use the first watch id returned by the first watch_list call and set:
status=disabled
deadline_at=2026-05-07T12:30:00Z
escalation_policy=manual-smoke-escalation

For ownership_update, use the created handoff_id and set:
current_owner=agent:operator
lease_holder=agent:operator
reviewer_actor=agent:reviewer
escalation_owner=user:ops
fallback_owner=agent:planner
leased_at=2026-05-07T12:00:00Z
lease_expires_at=2026-05-07T12:30:00Z

Call watch_list again with the same handoff_id.
Call ownership_get with the same handoff_id.

After the calls complete, output handoff_id, workflow_id, updated watch_id, watch status, watch deadline_at, watch escalation_policy, current_owner, lease_holder, reviewer_actor, escalation_owner, fallback_owner, leased_at, and lease_expires_at.
```

```bash
openclaw sessions export-trajectory --agent main --session-key '<session-key>' --json

./scripts/extract_openclaw_truth_plane_mutation_results.sh \
  --events .openclaw/trajectory-exports/<export-dir>/events.jsonl \
  --output /tmp/openclaw-truth-plane-mutation-results.json

SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --openclaw-truth-plane-mutation-results /tmp/openclaw-truth-plane-mutation-results.json
```

### Stage 3 truth-plane repair invalidate/backfill validation

To validate that OpenClaw really performs repair replay, ask the main agent to create a handoff, dispatch it, receive it, invalidate that accepted receive event, backfill an equivalent accepted receive event, then extract and validate both repair records plus final replayed truth from the trajectory:

```text
Please create one test handoff through the registered clawside MCP tools, dispatch it, run receive, invalidate the event from that receive call, backfill a replacement received event, and query repair plus final handoff truth.

Call these tools in order:
1. handoff_create
2. handoff_dispatch
3. handoff_progress action=receive
4. repair_invalidate_event
5. repair_backfill_event
6. repair_list
7. handoff_get

Use these creation parameters:
workflow_kind=manual_openclaw_truth_plane_repair_smoke
sender=agent:main
receiver=agent:planner
task_kind=truth_plane_repair_smoke
intent=verify OpenClaw can invalidate and backfill a clawside handoff event and observe replayed truth

Use these dispatch parameters:
handoff_id=<created handoff_id>
adapter=manual
target=agent:planner

Use these handoff_progress parameters:
action=receive
handoff_id=<created handoff_id>
actor=agent:planner

For repair_invalidate_event, use the event id returned by handoff_progress(receive) and set:
event_id=<receive event id>
reason=manual repair smoke invalidate receive event
actor=agent:main

For repair_backfill_event, use the same handoff and workflow and set:
workflow_id=<created workflow_id>
handoff_id=<created handoff_id>
type=received
subject_actor=agent:planner
producer_actor=agent:planner
requested_by=agent:main
reason=manual repair smoke backfill receive event

Call repair_list with the same handoff_id.
Call handoff_get with the same handoff_id.

After the calls complete, output handoff_id, workflow_id, invalidated event_id, invalidate repair_id, backfill repair_id, both repair actions, and final handoff state. The final handoff state should be received.
```

```bash
openclaw sessions export-trajectory --agent main --session-key '<session-key>' --json

./scripts/extract_openclaw_truth_plane_repair_results.sh \
  --events .openclaw/trajectory-exports/<export-dir>/events.jsonl \
  --output /tmp/openclaw-truth-plane-repair-results.json

SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --openclaw-truth-plane-repair-results /tmp/openclaw-truth-plane-repair-results.json
```

### Stage 4 truth-plane divergence/candidate/reopen smoke validation

To validate that OpenClaw really observes divergence, lists repair candidates, and reopens a completed handoff, ask the main agent to fully progress one handoff, then query divergence and candidates before reopening it:

```text
Please create one test handoff through the registered clawside MCP tools, dispatch it, progress it to completed according to the protocol, then query divergence and repair candidates, reopen that completed handoff, and query repair, final handoff truth, and workflow status.

Call these tools in order:
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

Use these creation parameters:
workflow_kind=manual_openclaw_truth_plane_reopen_smoke
sender=agent:main
receiver=agent:planner
task_kind=truth_plane_reopen_smoke
intent=verify OpenClaw can list divergence and repair candidates, then reopen a clawside handoff

Use these dispatch parameters:
set handoff_id to the handoff_id returned by handoff_create
adapter=manual
target=agent:planner

For every handoff_progress call, set handoff_id to the handoff_id returned by handoff_create, set actor=agent:planner, and run the actions in the order above.
Call divergence_list, repair_candidate_list, repair_list, and handoff_get with the handoff_id returned by handoff_create.
For repair_reopen_handoff, use handoff_id, reason, and actor: set handoff_id to the handoff_id returned by handoff_create, set reason to `manual repair smoke reopen completed handoff`, and set actor=agent:main.
Call workflow_status with the workflow_id returned by handoff_create.

After the calls complete, output handoff_id, workflow_id, divergence_id, candidate_id, reopened handoff_id, repair_id, final handoff state, and workflow status.
```

```bash
openclaw sessions export-trajectory --agent main --session-key '<session-key>' --json

./scripts/extract_openclaw_truth_plane_reopen_results.sh \
  --events .openclaw/trajectory-exports/<export-dir>/events.jsonl \
  --output /tmp/openclaw-truth-plane-reopen-results.json

SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --openclaw-truth-plane-reopen-results /tmp/openclaw-truth-plane-reopen-results.json
```

Expected result summary includes:

```text
openclaw_truth_plane_reopen_results: ok
```

Local extraction command example:

```bash
./scripts/extract_openclaw_truth_plane_reopen_results.sh --events PATH --output /tmp/openclaw-truth-plane-reopen-results.json
```

Local verifier command example:

```bash
./scripts/verify_openclaw_mcp.sh --openclaw-truth-plane-reopen-results /tmp/openclaw-truth-plane-reopen-results.json
```

### Stage 5 truth-plane continuity smoke validation

To validate that OpenClaw can continue the same handoff after reopening it and drive it back to completed truth, ask the main agent to fully progress one handoff, observe divergence and repair candidates, reopen it, then dispatch and progress the same handoff again:

```text
Please create one test handoff through the registered clawside MCP tools, dispatch it, progress it to completed according to the protocol, then query divergence and repair candidates, reopen that completed handoff, dispatch the same handoff again, run receive, claim, start, checkpoint, and complete again, then query handoff truth and workflow status.

Call these tools in order:
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

Use these creation parameters:
workflow_kind=manual_openclaw_truth_plane_continuity_smoke
sender=agent:main
receiver=agent:planner
task_kind=truth_plane_continuity_smoke
required_for_workflow_completion=true
intent=verify OpenClaw can reopen a completed clawside handoff and continue it to completed truth again

For the first handoff_dispatch call, set handoff_id to the handoff_id returned by handoff_create, adapter=manual, and target=agent:planner.
For the first handoff_progress sequence, use the handoff_id returned by handoff_create, set actor=agent:planner, and run receive, claim, start, checkpoint, and complete in order.
Call divergence_list and repair_candidate_list with the handoff_id returned by handoff_create.
For repair_reopen_handoff, use the same handoff_id, set reason to `manual continuity smoke reopen completed handoff`, and set actor=agent:main.
For the second handoff_dispatch call, use the same handoff_id, adapter=manual, and target=agent:planner.
For the second handoff_progress sequence, use the same handoff_id, set actor=agent:planner, and run receive, claim, start, checkpoint, and complete in order.
Call handoff_get with the same handoff_id. Call workflow_status with the workflow_id returned by handoff_create.

After the calls complete, output handoff_id, workflow_id, repair_id, repair action, reopened_state, post-reopen final handoff state, post-reopen final workflow status, and whether divergence_list / repair_candidate_list returned structuredContent.
```

In the export and extraction examples, replace `full session key` with the actual full session key and replace `export-directory` with the actual export directory name printed by `openclaw sessions export-trajectory`; `export-directory` is not a literal path segment.

```bash
openclaw sessions export-trajectory --agent main --session-key 'full session key' --json

./scripts/extract_openclaw_truth_plane_continuity_results.sh \
  --events .openclaw/trajectory-exports/export-directory/events.jsonl \
  --output /tmp/openclaw-truth-plane-continuity-results.json

SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --openclaw-truth-plane-continuity-results /tmp/openclaw-truth-plane-continuity-results.json
```

Expected result summary includes:

```text
openclaw_truth_plane_continuity_results: ok
```

Local extraction command example:

```bash
./scripts/extract_openclaw_truth_plane_continuity_results.sh --events PATH --output /tmp/openclaw-truth-plane-continuity-results.json
```

Local verifier command example:

```bash
./scripts/verify_openclaw_mcp.sh --openclaw-truth-plane-continuity-results /tmp/openclaw-truth-plane-continuity-results.json
```

### Stage 6 smoke profile validation

Stage 6 groups the Stage 0-5 validation paths into explicit profiles so operators do not need to remember a long argument list for each run.

Quick local health check:

```bash
SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh --profile quick
```

Full truth-plane evidence gate:

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

`truth-plane-full` requires every Stage 0-5 plus Stage 12 divergence and Stage 13 delivery evidence OpenClaw trajectory extractor JSON path, plus `coordination-evidence-summary.json`, explicitly. Missing evidence fails the run instead of being treated as skipped.

Release-grade read-only evidence gate: prefer the Stage 11 bundle-first flow to generate nine trajectory result JSON files, copy `coordination-evidence-summary.json`, and write `verify-release-evidence.sh`; the long command below remains the advanced manual fallback.

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

Pre-release local gate with real delivery:

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

`release-evidence` runs local Go readiness checks before the full OpenClaw MCP smoke check and stays read-only. `release` adds the explicit real delivery gate on top. Real delivery still goes through the sender backend only and never calls the Telegram API directly.

### Stage 7 fixtures profile regression validation

Stage 7 adds repository-owned golden evidence fixtures so local and CI runs can regression-test Stage 0-5 OpenClaw MCP verifier behavior without depending on private `.openclaw/trajectory-exports` paths.

Shortest command:

```bash
SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh --profile fixtures
```

`fixtures` reads bundled extracted JSON samples from:

```text
testdata/openclaw-smoke/stage0-5/
```

`fixtures` also validates the bundled A2A contract fixture at `testdata/openclaw-smoke/stage0-5/a2a-contract-results.json`. This check is read-only: it validates the Agent Card, public method matrix, JSON-RPC error data codes, `clawside.task.create` / `tasks/get` / `tasks/cancel` task projection, SSE task event shape, and safety boundaries. To validate another sanitized contract result, pass `--openclaw-a2a-contract-results <a2a-contract-results.json>`.

These fixtures are for local / CI regression. They prove the verifier still accepts stable golden evidence; they are not release acceptance evidence and do not prove that a fresh real OpenClaw trajectory still produces the same results.

Release or full acceptance still uses:

- `truth-plane-full`: pass all nine trajectory result JSON files extracted from real OpenClaw trajectory evidence plus `coordination-evidence-summary.json` explicitly;
- `release-evidence`: run the read-only release-grade gate from a real trajectory evidence bundle or explicit trajectory JSON files plus coordination evidence summary;
- `release`: start from real evidence, including coordination evidence summary, and explicitly enable `--deliver-main` and `--chat-id`.

Real delivery still goes through the sender backend only and never calls the Telegram API directly.

### Stage 8 local release guard

Stage 8 adds a local release guard for repeating the same safety checks before tagging and pushing. It is not GitHub Actions, and it does not directly create a GitHub Release.

Run clean local CI:

```bash
./scripts/ci-local.sh clean
```

`clean` mode builds a temporary directory from `git ls-files` tracked files, then runs:

1. `scripts/secret-scan.sh`
2. `scripts/secret-scan.sh --history`
3. `gofmt` check
4. `go vet ./...`
5. `go test -count=1 ./...`
6. `./build.sh`

Install the pre-push hook:

```bash
./scripts/install-hooks.sh
```

Verify the local release tag gate without creating or pushing a tag:

```bash
./scripts/tag-release.sh --evidence-bundle ./release-evidence/openclaw-v0.1.0 v0.1.0
```

`tag-release.sh` requires a clean worktree, a tag name starting with `v`, a non-existing tag, and an explicit release evidence bundle through `--evidence-bundle DIR` or `CLAWSIDE_RELEASE_EVIDENCE_BUNDLE=DIR`. The script first re-verifies the bundle manifest and `verify-release-evidence.sh` read-only, then runs `scripts/ci-local.sh clean`. It defaults to verify-only and does not create or push a tag unless `--authorize-tag-push` is passed after explicit release authorization. Pushing the tag does not directly create a GitHub Release; Stage 8 does not add a GitHub Actions release workflow.

### Stage 9 remote CI and release workflow

Stage 9 adds GitHub Actions remote CI and a `v*` tag-triggered release workflow on top of the Stage 8 local release guard. Stage 8 is the local pre-tag guard; Stage 9 is the remote preflight, build, checksum, and GitHub Release layer after the tag reaches GitHub.

Remote CI runs on `push` and `pull_request`:

1. `scripts/secret-scan.sh`
2. `scripts/secret-scan.sh --history`
3. `gofmt` check
4. `go vet ./...`
5. `go test -count=1 ./...`

The release workflow runs only on `v*` tags, builds linux amd64/arm64, darwin amd64/arm64, and windows amd64 artifacts, generates checksums, and creates or updates the GitHub Release. Each release archive includes the binary, `LICENSE`, root README, multilingual READMEs, `.example.env`, and `configs/config.example.toml`; it excludes `.env`, `configs/config.toml`, databases, logs, and `.openclaw/trajectory-exports`.

Before making the repository public, run the read-only GitHub readiness verifier. It reports Secret scanning / push protection, Private vulnerability reporting, main branch protection or ruleset required status checks, and CodeQL/code scanning status; it does not change GitHub settings:

```bash
./scripts/github-readiness.sh
./scripts/github-readiness.sh <owner>/<repo>
```

If the repository is still private or the account plan does not expose these settings, the verifier may fail with actionable messages. Make the GitHub settings change manually, then rerun the script until it passes.

Recommended release path after explicit release authorization:

```bash
scripts/ci-local.sh clean
scripts/tag-release.sh --authorize-tag-push --evidence-bundle ./release-evidence/openclaw-vX.Y.Z vX.Y.Z
```

`tag-release.sh` first read-only re-verifies the provided release evidence bundle, then runs the local clean CI gate. With `--authorize-tag-push`, it then creates and pushes the `v*` tag to GitHub. The GitHub Actions release workflow then builds artifacts remotely and creates or updates the GitHub Release. Stage 9 implementation and local verification do not run push, tag, or release; those shared-state operations still require explicit authorization.

To read-only check a local MCP registration config, pass the JSON config path explicitly. The check only reads the file and compares it with the current registration guidance; it never writes or patches config:

```bash
SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh --registration-config /path/to/mcp.json
```

To verify real delivery, explicitly pass `--deliver-main` and `--chat-id`; delivery goes through the configured sender/main bot path:

```bash
SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh --deliver-main --chat-id <telegram_chat_id>
```

The wrapper defaults to `configs/config.toml`, `sender.db`, `http://127.0.0.1:8787`, and `scripts/start_mcp.sh`. To customize the MCP startup command, use:

```bash
./scripts/verify_openclaw_mcp.sh --mcp-command ./scripts/start_mcp.sh
```

### Stage 10 repair backfill MCP validation

Stage 10 extends the existing Stage 3 repair evidence path instead of adding a separate `truth_plane_backfill` channel. The same `--openclaw-truth-plane-repair-results` verifier now requires `repair_backfill_event` evidence after `repair_invalidate_event`, validates `manual repair smoke backfill receive event`, and expects final handoff truth to be `received`.

The bundled fixtures profile includes this backfill replay evidence in `testdata/openclaw-smoke/stage0-5/repair-results.json`.

### Stage 11 release evidence gate

Stage 11 splits release acceptance into a read-only release-grade evidence gate and an explicitly authorized real delivery gate. Use `fixtures` for regression only, use `truth-plane-full` when you want to validate real trajectory evidence without local readiness checks, and use `release-evidence` before tagging when the real OpenClaw trajectory extracts should be treated as release-grade evidence.

Before building a release evidence bundle, generate the coordination evidence summary from the local orchestrator truth store. This file is a pre-generated sanitized artifact, not a trajectory extractor output:

```bash
go run ./cmd/orchestrator workflow evidence \
  --db <sqlite-db> \
  --workflow-id <workflow-id> \
  --include-agents \
  > <coordination-evidence-summary.json>
```

The recommended bundle-first path builds a local evidence bundle from real trajectory exports and the pre-generated coordination summary, then uses `--verify` to immediately run the read-only release-grade verifier:

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

The bundle command invokes existing extractors for the nine trajectory-derived result files and copies the pre-generated `coordination-evidence-summary.json`; it does not open the orchestrator DB while bundling. It writes `manifest.json`, ten evidence artifacts, and `verify-release-evidence.sh`. `--verify` runs the read-only release-grade verification path, does not include `--deliver-main` or `--chat-id`, and never calls the Telegram API directly. `verify-release-evidence.sh` locates the evidence artifacts from the script directory, first runs `openclaw-release-evidence-bundle verify-manifest` to verify the existence, metadata, and SHA256 values in `manifest.json`, then re-runs `scripts/verify_openclaw_mcp.sh --profile release-evidence` with all evidence paths, including `--coordination-evidence-summary`. The bundle can therefore be moved within the same repository and verified again. `release-evidence/openclaw-vX.Y.Z` is a local generated directory and is ignored by git by default.

A2A compatibility evidence is validated separately from the default release evidence bundle: run `./scripts/verify_clawside_a2a.sh` for the local endpoint readiness path, or validate the sanitized contract fixture at `testdata/openclaw-smoke/stage0-5/a2a-contract-results.json` with `scripts/verify_openclaw_mcp.sh --openclaw-a2a-contract-results <sanitized-a2a-contract-results.json>`. The default release bundle does not include `a2a-contract-results.json`; it stays limited to the release evidence artifacts plus `coordination-evidence-summary.json` and `verify-release-evidence.sh`. This A2A check is not runtime/session/sandbox/worker or sender delivery evidence and does not imply support for `message/send`, `message/stream`, or push notifications.

For a two-step flow, you can also run the generated verifier script manually:

```bash
./release-evidence/openclaw-vX.Y.Z/verify-release-evidence.sh
```

Before release, you can run the safe check that does not create a tag or push:

```bash
scripts/tag-release.sh --verify-only --evidence-bundle ./release-evidence/openclaw-vX.Y.Z vX.Y.Z
```

You can also pass the same directory through the environment:

```bash
CLAWSIDE_RELEASE_EVIDENCE_BUNDLE=./release-evidence/openclaw-vX.Y.Z scripts/tag-release.sh --verify-only vX.Y.Z
```

Advanced manual fallback can still pass the nine trajectory result JSON files and coordination summary explicitly:

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

Only after explicit authorization, run the real delivery gate with `--profile release --deliver-main --chat-id <telegram_chat_id>`. That path still uses `scripts/verify_openclaw_mcp.sh`, goes through the sender backend, and does not call Telegram directly.

### Diagnostic support bundle

When a reviewer or operator needs local readiness diagnostics, build a read-only diagnostic bundle:

```bash
./scripts/build_openclaw_diagnostic_bundle.sh \
  --output-dir ./diagnostic-bundles/local

./diagnostic-bundles/local/verify-diagnostic-bundle.sh
```

The command writes `manifest.json`, `smoke-report.json`, `sender-health.json`, `sender-ready.json`, `sender-stats.json`, `sender-jobs.json`, `registration-guidance.json`, `environment-summary.json`, and `verify-diagnostic-bundle.sh`. It only reads smoke, registration guidance, and sender observability; it does not perform real delivery and does not write OpenClaw or Claude config. `SENDER_AUTH_KEY` is inherited only from the local environment, secrets are redacted, and `diagnostic-bundles/` is a local generated directory ignored by git by default.

### Stage 12 divergence / E2E closure validation

Stage 12 splits divergence observation into its own evidence path instead of relying only on reopen/continuity validation. After dispatching one handoff, `divergence_record` records a `transport_accepted` observer signal, then the same handoff is progressed all the way to `completed`; `divergence_list` observes the `transport_accepted` divergence, `repair_candidate_list` verifies a `missing_authoritative_progress` candidate, and final `handoff_get` plus `workflow_status` prove the E2E truth still closes at `completed`. The export, extractor, and verifier steps remain read-only evidence handling.

```text
Create one test handoff through the registered clawside MCP tools, dispatch it, record one transport_accepted observer divergence signal, progress it to completed according to the protocol, then query divergence, repair candidates, handoff truth, and workflow status.

Call these tools in order:
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

For divergence_record, use the workflow_id / handoff_id returned by handoff_create, type=transport_accepted, and producer_actor=system:adapter.

After the calls complete, output handoff_id, workflow_id, divergence_id, candidate_id, signal_type, candidate reason, final handoff state, and final workflow status.
```

```bash
openclaw sessions export-trajectory --agent main --session-key '<session-key>' --json

./scripts/extract_openclaw_truth_plane_divergence_results.sh \
  --events .openclaw/trajectory-exports/<export-dir>/events.jsonl \
  --output /tmp/openclaw-truth-plane-divergence-results.json

SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --openclaw-truth-plane-divergence-results /tmp/openclaw-truth-plane-divergence-results.json
```

Expected result summary includes:

```text
openclaw_truth_plane_divergence_results: ok
```

### Stage 13 handoff + A2A delivery evidence validation

Stage 13 validates that OpenClaw can tie one clawside handoff to one A2A delivery sender job through MCP-visible sender job evidence. It does not change the handoff state machine, does not record delivery as authoritative progress, and does not directly call Telegram APIs; delivery evidence comes only from sender job query results visible through MCP tools.

```text
Create one test handoff through the registered clawside MCP tools, dispatch it, trigger one A2A delivery, read delivery evidence through sender job query tools, then query handoff truth and workflow status.

Call these tools in order:
1. handoff_create
2. handoff_dispatch
3. a2a_deliver
4. sender_job_get
5. sender_job_list
6. handoff_get
7. workflow_status

Parameters:
workflow_kind=manual_openclaw_truth_plane_delivery_smoke
sender=agent:main
receiver=agent:planner
task_kind=truth_plane_delivery_smoke
required_for_workflow_completion=false
intent=verify OpenClaw can tie clawside handoff truth to A2A delivery evidence
dispatch adapter=manual target=agent:planner
a2a_deliver target_agent=planner text=manual Stage 13 delivery smoke for <created handoff_id> chat_id=<telegram_chat_id>
sender_job_get uses the job_id returned by a2a_deliver; sender_job_list uses status=sent limit=10; handoff_get / workflow_status use the same handoff_id / workflow_id.

After the calls complete, output handoff_id, workflow_id, delivery job_id, sender job status, handoff state, and workflow status.
```

```bash
openclaw sessions export-trajectory --agent main --session-key '<session-key>' --json

./scripts/extract_openclaw_truth_plane_delivery_results.sh \
  --events .openclaw/trajectory-exports/<export-dir>/events.jsonl \
  --output /tmp/openclaw-truth-plane-delivery-results.json

SENDER_AUTH_KEY=... ./scripts/verify_openclaw_mcp.sh \
  --openclaw-truth-plane-delivery-results /tmp/openclaw-truth-plane-delivery-results.json
```

Expected result summary includes:

```text
openclaw_truth_plane_delivery_results: ok
```

## A2A delivery bridge CLI

After the sender sidecar is running, you can trigger a direct targeted delivery:

```bash
go run ./cmd/a2a-delivery \
  --target-agent planner \
  --text "Please send the result directly to me" \
  --chat-id <telegram_chat_id> \
  --sender-auth-key "$SENDER_AUTH_KEY"
```

If you do not pass `--chat-id`, you can pass session context fields and let the bridge resolve the target user:

```bash
go run ./cmd/a2a-delivery \
  --target-agent engineer \
  --text "Please sync the current status to the current session user" \
  --delivery-context-to <telegram_chat_id> \
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
