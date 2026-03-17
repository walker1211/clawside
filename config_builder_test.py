import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parent
TESTDATA = ROOT / "testdata"
BUILDER = ROOT / "scripts" / "config_builder.py"


def require_builder_exists() -> None:
    assert BUILDER.exists(), "builder helper script does not exist yet: config_builder.py"


def run_builder(input_json: Path, output_toml: Path) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [
            "python3",
            str(BUILDER),
            "--input",
            str(input_json),
            "--output",
            str(output_toml),
        ],
        cwd=ROOT,
        capture_output=True,
        text=True,
    )


def test_valid_config_generation(tmp_path: Path) -> None:
    require_builder_exists()
    output_toml = tmp_path / "config.toml"
    result = run_builder(TESTDATA / "openclaw.valid.json", output_toml)

    assert result.returncode == 0, (
        f"builder should succeed for valid input\nstdout:\n{result.stdout}\nstderr:\n{result.stderr}"
    )
    assert output_toml.exists(), "builder should generate output toml"
    assert output_toml.stat().st_mode & 0o777 == 0o600, "builder should write output with 0600 permissions"

    toml_text = output_toml.read_text(encoding="utf-8")

    assert 'address = "127.0.0.1:8787"' in toml_text
    assert 'database_path = "./sender.db"' in toml_text
    assert "default_max_attempts = 3" in toml_text
    assert 'worker_poll_interval = "1s"' in toml_text
    assert 'send_timeout = "15s"' in toml_text

    assert "[telegram]" in toml_text
    assert 'global_allow_user_ids = ["7098285098"]' in toml_text

    assert "[telegram.bots.planner]" in toml_text
    assert "[telegram.bots.engineer]" in toml_text

    assert 'account_id = "planner"' in toml_text
    assert 'token = "TOKEN_PLANNER"' in toml_text
    assert "allow_user_ids = []" in toml_text

    assert 'account_id = "coder"' in toml_text
    assert 'token = "TOKEN_CODER"' in toml_text


def test_duplicate_agent_conflict_fails(tmp_path: Path) -> None:
    require_builder_exists()
    output_toml = tmp_path / "config.toml"
    result = run_builder(TESTDATA / "openclaw.duplicate-agent.json", output_toml)

    assert result.returncode != 0, "builder should fail when agent/account mapping conflicts"
    combined = f"{result.stdout}\n{result.stderr}".lower()
    assert re.search(r"duplicate|conflict|conflicting", combined), (
        "expected duplicate/conflict mapping error message"
    )


def test_missing_token_fails(tmp_path: Path) -> None:
    require_builder_exists()
    output_toml = tmp_path / "config.toml"
    result = run_builder(TESTDATA / "openclaw.missing-token.json", output_toml)

    assert result.returncode != 0, "builder should fail when account botToken is missing"
    combined = f"{result.stdout}\n{result.stderr}".lower()
    assert re.search(r"missing.*token|bottoken|token.*missing", combined), (
        "expected missing token error message"
    )
