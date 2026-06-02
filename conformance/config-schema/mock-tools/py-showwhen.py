#!/usr/bin/env python3
"""ks-conf-py-showwhen — show_when DSL 编译 mock-tool（Python 侧）。

用法:
    echo "backend == 'github'" | python3 py-showwhen.py <field_name>

输入:
    stdin 是 DSL 源码（可能含换行，末尾 trim）。
    argv[1] 是受控字段名（用于 then.required / else.properties）。

输出 (stdout):
    编译后的 if_then_else 对象，canonical JSON 序列化
    （字段按字典序排序、无缩进、无尾随空格，ensure_ascii=False）。

退出码:
    - 0:  编译成功
    - 10: ValueError（允许的运行期错误，如 arithmetic / cross-level）
    - 11: SyntaxError（programmer error，如括号嵌套）
    - 2:  用法错

实现说明:
    绕过 ks_app 顶层 __init__（需要 uvicorn）。ksconfig/show_when.py 无相对导入，
    但为了一致性沿用 stub-package + load_submod 的 pattern。
"""
from __future__ import annotations

import importlib.util
import json
import sys
import types
from pathlib import Path

_HERE = Path(__file__).resolve().parent
_SDK_SRC = _HERE.parent.parent.parent / "sdk" / "python" / "src"


def _make_stub_pkg(name: str, path: Path) -> types.ModuleType:
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
_KSCONFIG_DIR = _KS_APP_DIR / "ksconfig"

if not (_KSCONFIG_DIR / "show_when.py").is_file():
    raise SystemExit(f"py-showwhen: 找不到 {_KSCONFIG_DIR / 'show_when.py'}")

_make_stub_pkg("ks_app", _KS_APP_DIR)
_make_stub_pkg("ks_app.ksconfig", _KSCONFIG_DIR)
_sw_mod = _load_submod(
    "ks_app.ksconfig.show_when", _KSCONFIG_DIR / "show_when.py"
)
compile_show_when = _sw_mod.compile_show_when


def main() -> int:
    if len(sys.argv) != 2:
        print(
            "usage: echo '<dsl>' | py-showwhen.py <field_name>",
            file=sys.stderr,
        )
        return 2
    field_name = sys.argv[1]
    dsl = sys.stdin.read().rstrip("\r\n\t ")

    try:
        if_then_else, _ = compile_show_when(dsl, field_name)
    except SyntaxError as e:
        print(f"SyntaxError: {e}", file=sys.stderr)
        return 11
    except (ValueError, TypeError) as e:
        print(f"parse error: {e}", file=sys.stderr)
        return 10

    sys.stdout.write(
        json.dumps(
            if_then_else,
            sort_keys=True,
            separators=(",", ":"),
            ensure_ascii=False,
        )
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
