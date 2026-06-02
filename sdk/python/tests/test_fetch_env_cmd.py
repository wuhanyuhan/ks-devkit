"""ksapp fetch-env 子命令测试。

覆盖 dotenv / json / shell 三种 format 渲染 + fetch 失败 exit 1 + argparse 失败 exit 2。
"""
from __future__ import annotations

import json

import httpx
import pytest
import respx

from ks_app.cli.__main__ import main


GATEWAY = "http://gw:9988"
TOKEN = "ks-app:42:1:1:abc"
SAMPLE_ENV = {
    "DB_HOST": "keystone-mysql",
    "DB_PORT": "3306",
    "DB_USER": "ksapp_writer",
    "DB_PASSWORD": "p@ssw0rd",
    "HMAC_SECRET": "hex32-deadbeef",
}


def _mock_keystone_ok(env: dict[str, str] | None = None) -> None:
    """respx 装一个返回标准 envelope 的 keystone mock。"""
    respx.get(f"{GATEWAY}/v1/apps/self/resources").mock(
        return_value=httpx.Response(
            200,
            json={
                "code": 0,
                "data": {
                    "app_id": "ks-mcp-writer",
                    "version": "1.0.0",
                    "install_id": 42,
                    "env": env if env is not None else SAMPLE_ENV,
                },
            },
        )
    )


# ── dotenv 格式 ────────────────────────────────────────────────────


@respx.mock
def test_fetch_env_dotenv_format(capsys):
    """--format dotenv（也是默认）→ stdout 含 BEGIN/END marker + 所有键值。"""
    _mock_keystone_ok()
    main(["fetch-env", "--gateway", GATEWAY, "--token", TOKEN, "--format", "dotenv"])
    out = capsys.readouterr().out

    assert "BEGIN KEYSTONE MANAGED" in out
    assert "END KEYSTONE MANAGED" in out
    for k, v in SAMPLE_ENV.items():
        # 行内必须含 key=value（值可能有引号，先看 key + value 子串都在即可）
        assert k in out
        assert v in out


@respx.mock
def test_fetch_env_default_format_is_dotenv(capsys):
    """不传 --format 时默认 dotenv（带 marker）。"""
    _mock_keystone_ok()
    main(["fetch-env", "--gateway", GATEWAY, "--token", TOKEN])
    out = capsys.readouterr().out
    assert "BEGIN KEYSTONE MANAGED" in out


@respx.mock
def test_fetch_env_dotenv_keys_sorted(capsys):
    """dotenv 输出 keys 按字母序排列，跨语言/跨运行可对齐。"""
    _mock_keystone_ok(env={"ZEBRA": "z", "ALPHA": "a", "MIDDLE": "m"})
    main(["fetch-env", "--gateway", GATEWAY, "--token", TOKEN, "--format", "dotenv"])
    out = capsys.readouterr().out

    # 抽取键值行（排除 marker / 空行）
    kv_lines = [
        line for line in out.splitlines()
        if "=" in line and "KEYSTONE MANAGED" not in line
    ]
    keys_in_order = [line.split("=", 1)[0] for line in kv_lines]
    assert keys_in_order == ["ALPHA", "MIDDLE", "ZEBRA"]


@respx.mock
def test_fetch_env_dotenv_quotes_special_chars(capsys):
    """值含空格 / # / " / \\ → 用双引号包并转义。"""
    _mock_keystone_ok(env={
        "PLAIN": "abc123",
        "WITH_SPACE": "hello world",
        "WITH_HASH": "abc#comment",
        "WITH_QUOTE": 'val"x',
    })
    main(["fetch-env", "--gateway", GATEWAY, "--token", TOKEN, "--format", "dotenv"])
    out = capsys.readouterr().out

    assert "PLAIN=abc123" in out  # 简单值不加引号
    assert 'WITH_SPACE="hello world"' in out
    assert 'WITH_HASH="abc#comment"' in out
    assert 'WITH_QUOTE="val\\"x"' in out


# ── json 格式 ────────────────────────────────────────────────────


@respx.mock
def test_fetch_env_json_format(capsys):
    """--format json → stdout 是合法 JSON object，等于 SAMPLE_ENV。"""
    _mock_keystone_ok()
    main(["fetch-env", "--gateway", GATEWAY, "--token", TOKEN, "--format", "json"])
    out = capsys.readouterr().out

    parsed = json.loads(out)
    assert parsed == SAMPLE_ENV


# ── shell 格式 ────────────────────────────────────────────────────


@respx.mock
def test_fetch_env_shell_format(capsys):
    """--format shell → 每行 export KEY="value"，值含特殊字符要转义。"""
    _mock_keystone_ok(env={"DB_HOST": "mysql", "DB_PASS": 'p@$$"x"'})
    main(["fetch-env", "--gateway", GATEWAY, "--token", TOKEN, "--format", "shell"])
    out = capsys.readouterr().out

    assert 'export DB_HOST="mysql"' in out
    # $ 和 " 要转义为 \$ 和 \"
    assert 'export DB_PASS="p@\\$\\$\\"x\\""' in out


# ── 失败路径 ────────────────────────────────────────────────────


@respx.mock
def test_fetch_env_fetch_error_exits_1(capsys):
    """keystone 503 → sys.exit(1) + stderr 写错误。"""
    respx.get(f"{GATEWAY}/v1/apps/self/resources").mock(
        return_value=httpx.Response(503, text="upstream down")
    )

    with pytest.raises(SystemExit) as exc:
        main(["fetch-env", "--gateway", GATEWAY, "--token", TOKEN])

    assert exc.value.code == 1
    err = capsys.readouterr().err
    # 错误消息应到 stderr，含状态码线索
    assert "503" in err or "fetch" in err.lower()


def test_fetch_env_unknown_format_exits_2(capsys):
    """--format 取非枚举值 → argparse 报错 → sys.exit(2)。"""
    with pytest.raises(SystemExit) as exc:
        main(["fetch-env", "--gateway", GATEWAY, "--token", TOKEN, "--format", "xml"])
    assert exc.value.code == 2


def test_fetch_env_missing_gateway_exits_2(capsys):
    """缺 --gateway → argparse 报错 → sys.exit(2)。"""
    with pytest.raises(SystemExit) as exc:
        main(["fetch-env", "--token", TOKEN])
    assert exc.value.code == 2


def test_fetch_env_missing_token_exits_2(capsys):
    """缺 --token → argparse 报错 → sys.exit(2)。"""
    with pytest.raises(SystemExit) as exc:
        main(["fetch-env", "--gateway", GATEWAY])
    assert exc.value.code == 2
