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
- `cmd/openclaw-mcp-smoke/`: local smoke verifier for OpenClaw consuming the clawside MCP v1 surface.
- `cmd/openclaw-tool-results-extract/`: local read-only CLI for extracting clawside tool structured results from OpenClaw trajectory.
- `cmd/openclaw-truth-plane-extract/`: local read-only CLI for extracting minimal truth-plane handoff/workflow/watch/ownership validation results from OpenClaw trajectory.
- `cmd/openclaw-truth-plane-progression-extract/`: local read-only CLI for extracting completed handoff progression validation results from OpenClaw trajectory.
- `cmd/openclaw-truth-plane-mutation-extract/`: local read-only CLI for extracting watch / ownership mutation validation results from OpenClaw trajectory.
- `cmd/openclaw-truth-plane-repair-extract/`: local read-only CLI for extracting repair invalidate-event replay validation results from OpenClaw trajectory.
- `cmd/openclaw-truth-plane-reopen-extract/`: local read-only CLI for extracting divergence/candidate/reopen handoff validation results from OpenClaw trajectory.
- `cmd/openclaw-truth-plane-continuity-extract/`: local read-only CLI for extracting truth-plane continuity validation results after reopening and continuing the same handoff from OpenClaw trajectory.
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
./scripts/extract_openclaw_tool_results.sh --events .openclaw/trajectory-exports/<export-dir>/events.jsonl
./scripts/extract_openclaw_truth_plane_results.sh --events .openclaw/trajectory-exports/<export-dir>/events.jsonl
./scripts/extract_openclaw_truth_plane_progression_results.sh --events .openclaw/trajectory-exports/<export-dir>/events.jsonl
./scripts/extract_openclaw_truth_plane_mutation_results.sh --events .openclaw/trajectory-exports/<export-dir>/events.jsonl
./scripts/extract_openclaw_truth_plane_repair_results.sh --events .openclaw/trajectory-exports/<export-dir>/events.jsonl
./scripts/extract_openclaw_truth_plane_reopen_results.sh --events .openclaw/trajectory-exports/<export-dir>/events.jsonl
./scripts/extract_openclaw_truth_plane_continuity_results.sh --events .openclaw/trajectory-exports/export-directory/events.jsonl
```

Notes:

- `./build.sh`: builds the root sender binary at `./clawside`.
- `./start.sh`: starts the sender service in the background and writes `logs/sender.pid` plus `logs/sender.log`.
- `./stop.sh`: stops the sender process recorded by `./start.sh`.
- `./restart.sh`: runs `./stop.sh` and then `./start.sh`.
- `./scripts/start.sh`: starts the sender in the foreground through `go run .`, useful for debugging or avoiding a binary build.
- `./scripts/start_mcp.sh`: starts the stdio MCP server for OpenClaw registration.
- `./scripts/extract_openclaw_tool_results.sh`: extracts clawside sender tool structured results from OpenClaw trajectory `events.jsonl`.
- `./scripts/extract_openclaw_truth_plane_results.sh`: extracts minimal truth-plane validation results from OpenClaw trajectory `events.jsonl`.
- `./scripts/extract_openclaw_truth_plane_progression_results.sh`: extracts completed progression validation results from OpenClaw trajectory `events.jsonl`.
- `./scripts/extract_openclaw_truth_plane_mutation_results.sh`: extracts watch / ownership mutation validation results from OpenClaw trajectory `events.jsonl`.
- `./scripts/extract_openclaw_truth_plane_repair_results.sh`: extracts repair invalidate-event replay validation results from OpenClaw trajectory `events.jsonl`.
- `./scripts/extract_openclaw_truth_plane_reopen_results.sh`: extracts divergence/candidate/reopen handoff validation results from OpenClaw trajectory `events.jsonl`.
- `./scripts/extract_openclaw_truth_plane_continuity_results.sh`: extracts truth-plane continuity validation results after reopening and continuing the same handoff from OpenClaw trajectory `events.jsonl`.

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

### Stage 3 truth-plane repair validation

To validate that OpenClaw really performs repair replay, ask the main agent to create a handoff, dispatch it, receive it, invalidate that accepted receive event, then extract and validate the repair record plus final replayed truth from the trajectory:

```text
Please create one test handoff through the registered clawside MCP tools, dispatch it, run receive, then invalidate the event from that receive call and query repair plus final handoff truth.

Call these tools in order:
1. handoff_create
2. handoff_dispatch
3. handoff_progress action=receive
4. repair_invalidate_event
5. repair_list
6. handoff_get

Use these creation parameters:
workflow_kind=manual_openclaw_truth_plane_repair_smoke
sender=agent:main
receiver=agent:planner
task_kind=truth_plane_repair_smoke
intent=verify OpenClaw can invalidate a clawside handoff event and observe replayed truth

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

Call repair_list with the same handoff_id.
Call handoff_get with the same handoff_id.

After the calls complete, output handoff_id, workflow_id, invalidated event_id, repair_id, repair action, and final handoff state.
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

```bash
openclaw sessions export-trajectory --agent main --session-key 'quoted descriptive session key' --json

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
