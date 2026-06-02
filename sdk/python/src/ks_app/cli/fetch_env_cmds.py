"""ksapp fetch-env 子命令实现。

用法：
    ksapp fetch-env --gateway $KS_GATEWAY_URL --token $KS_APP_TOKEN
    ksapp fetch-env --gateway ... --token ... --format json
    ksapp fetch-env --gateway ... --token ... --format shell

输出三种 format：
- dotenv（默认）：BEGIN/END marker 包夹的 KEY=value 行（marker 用 # 注释前缀，
  确保被 dotenv 解析器跳过；同一文件多次注入用 marker 幂等覆盖）
- json：单一 JSON object，键值对
- shell：每行 export KEY="value"，可直接 `eval $(ksapp fetch-env ... --format shell)`

副作用：失败时打 stderr → sys.exit(1)；argparse 缺/错 arg → sys.exit(2)。
"""
from __future__ import annotations

import argparse
import json
import sys
from datetime import datetime, timezone
from typing import NoReturn, TextIO

from ..keystone_client import KeystoneSelfFetchError, SelfClient

# dotenv marker：用 box drawing 字符提高辨识度，# 前缀确保 dotenv 解析器忽略。
DOTENV_MARKER_BEGIN_FMT = "# ─── BEGIN KEYSTONE MANAGED (generated {ts}) ───"
DOTENV_MARKER_END = "# ─── END KEYSTONE MANAGED ───"

# dotenv 触发加引号的字符：空格 / tab / # / 双引号 / 反斜杠（其余 ASCII 不加引号）
DOTENV_QUOTE_CHARS = (" ", "\t", "#", '"', "\\")

# shell 双引号内需要转义的字符（防止 shell 展开 / 引号闭合）
SHELL_ESCAPE_CHARS = ("\\", '"', "$", "`")


# ── CLI 入口（argparse dispatch func） ─────────────────────────────


def cmd_fetch_env(args: argparse.Namespace) -> None:
    """fetch-env 子命令入口。"""
    try:
        env = SelfClient(args.gateway, args.token).fetch_env()
    except KeystoneSelfFetchError as e:
        exit_err(f"fetch keystone env failed: {e}")

    fmt = getattr(args, "format", "dotenv") or "dotenv"
    if fmt == "dotenv":
        render_dotenv(sys.stdout, env)
    elif fmt == "json":
        render_json(sys.stdout, env)
    elif fmt == "shell":
        render_shell(sys.stdout, env)
    else:
        # argparse choices 已拦截，此处兜底
        exit_err(f"unknown format: {fmt!r}")


# ── 可测 helper（render_xxx 接 TextIO，便于 capsys 与 StringIO 验证） ──


def render_dotenv(w: TextIO, env: dict[str, str]) -> None:
    """dotenv 输出：BEGIN/END marker 包夹，KEY=value 按 key 字母序。

    简单值不加引号；含空格 / # / " / \\ 用双引号包并转义 `\\` `"`。
    """
    ts = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    w.write(DOTENV_MARKER_BEGIN_FMT.format(ts=ts) + "\n")
    for k in sorted(env.keys()):
        w.write(f"{k}={_quote_dotenv(env[k])}\n")
    w.write(DOTENV_MARKER_END + "\n")


def render_json(w: TextIO, env: dict[str, str]) -> None:
    """JSON object 输出（缩进 2，UTF-8 不转义）。"""
    # sort_keys=True 让跨语言/跨运行 byte-equivalent，方便脚本 diff
    w.write(json.dumps(env, ensure_ascii=False, sort_keys=True, indent=2))
    w.write("\n")


def render_shell(w: TextIO, env: dict[str, str]) -> None:
    """shell 输出：每行 `export KEY="value"`，双引号内转义防展开。"""
    for k in sorted(env.keys()):
        w.write(f'export {k}="{_escape_shell_doublequoted(env[k])}"\n')


def _quote_dotenv(v: str) -> str:
    """按 dotenv 习惯决定是否加双引号。

    简单值（不含 DOTENV_QUOTE_CHARS）→ 原样返回；
    否则双引号包并转义 `\\` `"`（dotenv 规范无统一标准，本约定保证 python-dotenv /
    docker compose 主流解析器都能正确读回）。
    """
    if not any(c in v for c in DOTENV_QUOTE_CHARS):
        return v
    escaped = v.replace("\\", "\\\\").replace('"', '\\"')
    return f'"{escaped}"'


def _escape_shell_doublequoted(v: str) -> str:
    """转义双引号 shell 字符串内的特殊字符（防参数展开/命令注入）。"""
    out = v
    for ch in SHELL_ESCAPE_CHARS:
        out = out.replace(ch, "\\" + ch)
    return out


def exit_err(msg: str) -> NoReturn:
    """打错误到 stderr 并 sys.exit(1)。"""
    print(msg, file=sys.stderr)
    sys.exit(1)
