# clawside

`clawside` is a local MCP sidecar / truth layer for OpenClaw.

It currently provides a local Telegram sender, handoff/workflow orchestration foundations, and a productized OpenClaw-consumable MCP + skill v1 surface.

[中文](./README.zh-CN.md) | [English](./README.en.md)

## Quick Start

```bash
export SENDER_AUTH_KEY='your-local-sender-key'
./scripts/config_builder.sh
./build.sh
./start.sh
```

The sender listens on `127.0.0.1:8787` by default.

For managed local lifecycle:

```bash
./stop.sh
./restart.sh
```

For the MCP server:

```bash
./scripts/start_mcp.sh --db ./sender.db
```

Optional A2A routing overrides use `target_agent=bot` pairs, for example `CLAWSIDE_TARGET_AGENT_BOT_MAP='qa=guardian' ./scripts/start_mcp.sh --db ./sender.db`. Delivery calls still provide `target_agent`; callers do not pass arbitrary bot names. Semantic A2A retry idempotency requires an explicit stable `idempotency_key`; omitted keys are generated as unique nonce-based delivery attempts. MCP sender observability tools provide read-only health, readiness, stats, job-list, and job-status inspection through the configured sender sidecar.

## Configuration

Recommended local setup:

```bash
export SENDER_AUTH_KEY='your-local-sender-key'
./scripts/config_builder.sh
```

This reads `~/.openclaw/openclaw.json` and writes `configs/config.toml` with mode `600`.

A non-secret example is available at:

```text
configs/config.example.toml
```

`configs/config.toml`, `.env`, `sender.db`, logs, and local runtime data are ignored by git.
