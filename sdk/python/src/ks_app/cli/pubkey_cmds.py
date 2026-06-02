"""pubkey 子命令实现（镜像 Go sdk/go/ksapp/cli/pubkey.go）。

ksapp pubkey                          — 显示当前 X25519 公钥 + 指纹
ksapp pubkey rotate [--print-only]    — 密钥轮换（--print-only 仅打印不落盘）
ksapp pubkey prune-old                — 清除 .mcp-key.old
"""
from __future__ import annotations

import argparse
import base64
import sys
from typing import TextIO

from ..keystore import (
    Keystore,
    RotateOptions,
    RotateResult,
    load,
    prune_old,
    rotate,
)
from .config_cmds import exit_err


# ---- 入口（argparse 分派）----


def cmd_pubkey_show(args: argparse.Namespace) -> None:
    """pubkey 无子命令时调用。显示 source / fingerprint / pubkey（+ 可选 old）。"""
    try:
        ks = load()
    except Exception as e:  # noqa: BLE001
        exit_err(f"keystore load: {e}")
    render_keystore(sys.stdout, ks)


def cmd_pubkey_rotate(args: argparse.Namespace) -> None:
    """pubkey rotate 入口。解析 --print-only 后调 keystore.rotate。"""
    print_only = bool(getattr(args, "print_only", False))
    try:
        r = rotate(RotateOptions(print_only=print_only))
    except Exception as e:  # noqa: BLE001
        exit_err(f"rotate: {e}")
    render_rotate_result(sys.stdout, r, print_only)


def cmd_pubkey_prune_old(args: argparse.Namespace) -> None:
    """pubkey prune-old 入口。清除 fallback 模式下的 .mcp-key.old。"""
    try:
        prune_old("")
    except Exception as e:  # noqa: BLE001
        exit_err(f"prune-old: {e}")
    print("已清除 .mcp-key.old")


# ---- 可测 helper（渲染从 CLI 入口剥离）----


def render_keystore(w: TextIO, ks: Keystore) -> None:
    """把 Keystore 的公钥信息渲染到 w，便于测试直接调。

    字节级等价 Go CLI：
      - source 形如 "fallback-file"（Source.__str__）
      - pubkey base64 用 StdEncoding（Python base64.b64encode 带 padding）
    """
    w.write(f"source:      {ks.source}\n")
    w.write(f"fingerprint: {ks.primary.fingerprint}\n")
    w.write(f"pubkey:      {base64.b64encode(ks.primary.pubkey).decode('ascii')}\n")
    if ks.old is not None:
        w.write(f"old_fingerprint: {ks.old.fingerprint}（过渡期）\n")


def render_rotate_result(w: TextIO, r: RotateResult, print_only: bool) -> None:
    """把 RotateResult 渲染为运维友好的多行文本。

    print_only = True → 提示运维搬旧密钥到 _OLD_B64；
    False → 打印已写入的文件清单。
    """
    w.write("=== 新密钥对 ===\n")
    w.write(f"KSAPP_MCP_PRIVKEY_B64={r.new_privkey_b64}\n")
    w.write(f"pubkey (base64): {r.new_pubkey_b64}\n")
    w.write(f"fingerprint:     {r.fingerprint}\n")
    if print_only:
        w.write("\n")
        w.write("注意: 运维需把当前 PRIVKEY_B64 搬到 KSAPP_MCP_PRIVKEY_OLD_B64，\n")
        w.write("      并把上面的新值写入 KSAPP_MCP_PRIVKEY_B64，然后 Rolling Update 重启。\n")
    else:
        w.write(f"\n已写入: {_format_str_slice(r.files_written)}\n")


def _format_str_slice(items: list[str]) -> str:
    """格式化字符串列表为 Go fmt.Sprintf("%v", slice) 等价输出。

    Go %v 对 []string 输出 `[elem1 elem2]`（空格分隔，无引号）；空 slice → `[]`。
    Python str(list) 默认 `['elem1', 'elem2']` 不等价；镜像 Go 字面格式供字节级比对。
    """
    return "[" + " ".join(items) + "]"
