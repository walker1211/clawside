#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

DEFAULT_INPUT = Path("~/.openclaw/openclaw.json").expanduser()
DEFAULT_OUTPUT = Path("config.toml")
DEFAULT_ADDRESS = "127.0.0.1:8787"
DEFAULT_DATABASE_PATH = "./sender.db"
DEFAULT_MAX_ATTEMPTS = 3
DEFAULT_WORKER_POLL_INTERVAL = "1s"
DEFAULT_SEND_TIMEOUT = "15s"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Build sender config.toml from openclaw.json")
    parser.add_argument("--input", default=str(DEFAULT_INPUT), help="Path to openclaw.json")
    parser.add_argument("--output", default=str(DEFAULT_OUTPUT), help="Path to write config.toml")
    return parser.parse_args()


def load_source(path: Path) -> dict:
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError as exc:
        raise ValueError(f"openclaw json not found: {path}") from exc
    except json.JSONDecodeError as exc:
        raise ValueError(f"parse openclaw json: {exc}") from exc

    if not isinstance(data, dict):
        raise ValueError("openclaw json root must be an object")
    return data


def normalize_user_ids(raw: object, field_name: str) -> list[str]:
    if raw is None:
        return []
    if not isinstance(raw, list):
        raise ValueError(f"{field_name} must be a list")

    values: list[str] = []
    for item in raw:
        text = str(item).strip()
        try:
            parsed = int(text, 10)
        except ValueError as exc:
            raise ValueError(f"invalid user id {item!r} in {field_name}") from exc
        values.append(str(parsed))
    return values


def collect_routes(source: dict) -> dict[str, str]:
    bindings = source.get("bindings")
    if not isinstance(bindings, list):
        raise ValueError("bindings must be a list")

    routes: dict[str, str] = {}
    account_owners: dict[str, str] = {}
    for binding in bindings:
        if not isinstance(binding, dict):
            raise ValueError("binding entries must be objects")
        if binding.get("type") != "route":
            continue

        match = binding.get("match")
        if not isinstance(match, dict):
            raise ValueError("route binding match must be an object")
        if match.get("channel") != "telegram":
            continue

        bot_name = str(binding.get("agentId", "")).strip()
        account_id = str(match.get("accountId", "")).strip()
        if not bot_name:
            raise ValueError("telegram route binding missing agentId")
        if not account_id:
            raise ValueError(f"telegram route binding for {bot_name} missing accountId")
        if bot_name in routes:
            raise ValueError(f"duplicate/conflicting telegram route for bot {bot_name}")
        owner = account_owners.get(account_id)
        if owner is not None:
            raise ValueError(
                f"conflicting telegram route for account {account_id}: {owner} and {bot_name}"
            )

        routes[bot_name] = account_id
        account_owners[account_id] = bot_name

    if not routes:
        raise ValueError("missing telegram route binding")
    return routes


def build_config_model(source: dict) -> tuple[list[str], list[dict[str, object]]]:
    channels = source.get("channels")
    if not isinstance(channels, dict):
        raise ValueError("channels must be an object")

    telegram = channels.get("telegram")
    if not isinstance(telegram, dict):
        raise ValueError("channels.telegram must be an object")

    global_allow_user_ids = normalize_user_ids(
        telegram.get("groupAllowFrom"), "channels.telegram.groupAllowFrom"
    )

    accounts = telegram.get("accounts")
    if not isinstance(accounts, dict):
        raise ValueError("channels.telegram.accounts must be an object")

    routes = collect_routes(source)
    bots: list[dict[str, object]] = []
    for bot_name in sorted(routes):
        account_id = routes[bot_name]
        account = accounts.get(account_id)
        if not isinstance(account, dict):
            raise ValueError(f"missing telegram account {account_id} for bot {bot_name}")

        token = str(account.get("botToken", "")).strip()
        if not token:
            raise ValueError(f"missing botToken for telegram account {account_id} (bot {bot_name})")

        bots.append(
            {
                "name": bot_name,
                "enabled": True,
                "account_id": account_id,
                "token": token,
                "allow_user_ids": [],
            }
        )

    if not bots:
        raise ValueError("no telegram bots generated")
    return global_allow_user_ids, bots


def render_string(value: str) -> str:
    return json.dumps(value, ensure_ascii=False)


def render_string_list(values: list[str]) -> str:
    return "[" + ", ".join(render_string(value) for value in values) + "]"


def render_table_key(key: str) -> str:
    if re.fullmatch(r"[A-Za-z0-9_-]+", key):
        return key
    return render_string(key)


def render_toml(global_allow_user_ids: list[str], bots: list[dict[str, object]]) -> str:
    lines = [
        f"address = {render_string(DEFAULT_ADDRESS)}",
        f"database_path = {render_string(DEFAULT_DATABASE_PATH)}",
        f"default_max_attempts = {DEFAULT_MAX_ATTEMPTS}",
        f"worker_poll_interval = {render_string(DEFAULT_WORKER_POLL_INTERVAL)}",
        f"send_timeout = {render_string(DEFAULT_SEND_TIMEOUT)}",
        "",
        "[telegram]",
        f"global_allow_user_ids = {render_string_list(global_allow_user_ids)}",
    ]

    for bot in bots:
        lines.extend(
            [
                "",
                f"[telegram.bots.{render_table_key(str(bot['name']))}]",
                f"enabled = {str(bool(bot['enabled'])).lower()}",
                f"account_id = {render_string(str(bot['account_id']))}",
                f"token = {render_string(str(bot['token']))}",
                f"allow_user_ids = {render_string_list(list(bot['allow_user_ids']))}",
            ]
        )

    return "\n".join(lines) + "\n"


def main() -> int:
    args = parse_args()
    input_path = Path(args.input).expanduser()
    output_path = Path(args.output).expanduser()

    try:
        source = load_source(input_path)
        global_allow_user_ids, bots = build_config_model(source)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(render_toml(global_allow_user_ids, bots), encoding="utf-8")
        output_path.chmod(0o600)
    except ValueError as exc:
        print(str(exc), file=sys.stderr)
        return 1
    except OSError as exc:
        print(f"write config file: {exc}", file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
