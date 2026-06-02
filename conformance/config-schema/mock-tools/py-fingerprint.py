#!/usr/bin/env python3
"""ks-conf-py-fingerprint — 公钥指纹 mock-tool（Python 侧）。

用法:
    python3 py-fingerprint.py <pubkey_hex>

输入:
    32 字节 X25519 公钥的 hex（64 字符，大小写不限）。

输出 (stdout):
    spec-v1 §4.2 fingerprint 字符串（8 段 × 4 hex × ':'），例如
    ``6668:7aad:f862:bd77:6c8f:c18b:8e9f:8e20``。

实现说明:
    fingerprint.py 用相对导入 (from .x25519 import X25519_PUBKEY_LEN)，所以
    不能单独 spec_from_file_location 加载，需要注册成 ks_app.crypto 的 package
    成员。这里手工构造最小 package 模拟：
      1. 创建空的 ks_app 包（伪 __init__，避免 ks_app/__init__.py 被 importlib
         拉进 uvicorn 依赖）
      2. 创建 ks_app.crypto 包，同理伪 __init__
      3. 按路径加载 ks_app.crypto.x25519 / ks_app.crypto.fingerprint
    这样就可以用真实 SDK 代码，而不拉全量 ks_app 顶层依赖。
"""
from __future__ import annotations

import importlib.util
import sys
import types
from pathlib import Path

_HERE = Path(__file__).resolve().parent
_SDK_SRC = _HERE.parent.parent.parent / "sdk" / "python" / "src"


def _make_stub_pkg(name: str, path: Path) -> types.ModuleType:
    """构造空的 namespace package，使相对导入工作。"""
    pkg = types.ModuleType(name)
    pkg.__path__ = [str(path)]
    sys.modules[name] = pkg
    return pkg


def _load_submod(name: str, path: Path) -> types.ModuleType:
    spec = importlib.util.spec_from_file_location(name, path)
    mod = importlib.util.module_from_spec(spec)
    sys.modules[name] = mod
    spec.loader.exec_module(mod)  # type: ignore[union-attr]
    return mod


_KS_APP_DIR = _SDK_SRC / "ks_app"
_CRYPTO_DIR = _KS_APP_DIR / "crypto"

if not (_CRYPTO_DIR / "fingerprint.py").is_file():
    raise SystemExit(f"py-fingerprint: 找不到 {_CRYPTO_DIR / 'fingerprint.py'}")

_make_stub_pkg("ks_app", _KS_APP_DIR)
_make_stub_pkg("ks_app.crypto", _CRYPTO_DIR)
_load_submod("ks_app.crypto.x25519", _CRYPTO_DIR / "x25519.py")
_fp_mod = _load_submod("ks_app.crypto.fingerprint", _CRYPTO_DIR / "fingerprint.py")
fingerprint = _fp_mod.fingerprint


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: py-fingerprint.py <pubkey_hex>", file=sys.stderr)
        return 2
    try:
        pub = bytes.fromhex(sys.argv[1])
    except ValueError as e:
        print(f"pubkey hex 解析失败：{e}", file=sys.stderr)
        return 2

    # 对齐三端退出码：fingerprint() 对非 32 字节会抛 ValueError，Python 默认把
    # 未捕获异常映射为 rc=1，与 Go/TS 的 rc=2 不一致。这里显式 catch → rc=2
    # （"用法错"语义，SPEC 退出码表的统一入口）。
    try:
        fp = fingerprint(pub)
    except ValueError as e:
        print(f"pubkey 长度错：{e}", file=sys.stderr)
        return 2
    sys.stdout.write(fp)
    return 0


if __name__ == "__main__":
    sys.exit(main())
