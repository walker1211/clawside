---
name: openclaw-a2a-delivery
description: Use this skill whenever the user wants a named OpenClaw agent, partner, or persona to send an actual outward message to the Telegram recipient — the current user, current chat, private chat, or an explicit chat_id — instead of the main agent replying in-session. Trigger for intents like “让 X 直接对我说”, “别转述”, “发给当前用户/chat_id”, or “走 sender bridge / 不走官方 announce”, especially when the user wants a real delivery attempt and a definite sent/failed/unsupported result. Do not use it for debugging sender/announce/Telegram issues, setup/configuration, or ordinary summarize-and-reply requests.
---

## Purpose

Provide a stable, explicit delivery path when the main agent needs a target agent identity to speak directly to the
current user.

This skill is a **sender bridge**. It does not implement Telegram delivery itself.

## Invocation model

- **Main agent explicit invocation only**.
- Do not auto-trigger from announce behavior, nested behavior, or background fallback logic.
- Use this skill intentionally when controlled, observable delivery is required.

## Delivery backend boundary

- **Existing sender service is the only delivery backend in this skill**.
- This skill calls sender endpoints:
    - `POST /send`
    - `GET /jobs/{job_id}`
- This skill **must not call Telegram API directly**.

## Input contract

Required:

- `target_agent` (string)
- `text` (string)

Optional:

- `chat_id` (int64, explicit override)
- `idempotency_key` (string)

### Input validation

- `target_agent`
    - must be non-empty
    - must resolve through built-in or configured mapping
- `text`
    - must be non-empty after trimming
    - must be <= 4096 chars
- `chat_id`
    - if provided, must be int64 > 0
- `idempotency_key`
    - if provided, must be non-empty after trimming

## Target user resolution

Use **context-first resolution with optional explicit override**:

1. If `chat_id` is provided, use it.
2. Otherwise resolve target user from current session context.
3. If unresolved or conflicting, fail before calling sender.

## Target-agent mapping

Use explicit `target_agent -> bot` mapping. Built-in defaults are:

- `planner -> planner`
- `engineer -> engineer`
- `researcher -> researcher`
- `archivist -> archivist`
- `guardian -> guardian`
- `closer -> closer`

Local startup may add or override mappings with `target_agent=bot` pairs through `--target-agent-map` or `CLAWSIDE_TARGET_AGENT_BOT_MAP`. Skill requests still provide `target_agent`; do not pass or invent a raw bot override per message.

Unknown `target_agent` must fail immediately.

## Concrete local entrypoint

The skill should invoke the local bridge CLI:

```bash
go run ./cmd/a2a-delivery \
  --target-agent <target_agent> \
  --text <text> \
  [--chat-id <chat_id>] \
  [--idempotency-key <idempotency_key>] \
  [--delivery-context-to <chat_id>] \
  [--direct-session-peer-chat-id <chat_id>] \
  [--inbound-sender-chat-id <chat_id>] \
  [--sender-auth-key <sender_auth_key>] \
  [--target-agent-map <target_agent=bot[,target_agent=bot...]>]
```

The command prints machine-readable JSON with the fixed delivery result contract.

## Bridge flow

1. Validate input.
2. Resolve `chat_id` (override first, then context).
3. Resolve `target_agent -> bot` via built-in or configured mapping.
4. Call sender `POST /send`.
5. Poll sender `GET /jobs/{job_id}`.
6. Return structured delivery result.

## Output contract

Return fixed fields:

- `status` (`sent` | `failed` | `retrying`)
- `job_id`
- `target_agent`
- `bot`
- `chat_id`
- `attempt_count`
- `last_error`

## Polling policy (v1 fixed defaults)

- interval: `2s`
- timeout: `15s`

Stop polling when:

- sender job is `sent`
- sender job is `failed`
- timeout reached

On timeout return:

- `status = retrying`
- preserve `job_id`
- include timeout detail in `last_error`

## Explicit non-goals

- No direct Telegram API usage.
- No replacement of sender as backend.
- No announce-chain repair attempt.
- No nested lane/session-chain repair attempt.
- No claim to fix OpenClaw official announce behavior.
