"""config 子命令实现（镜像 Go sdk/go/ksapp/cli/config.go）。

路径（相对 cwd，便于测试 monkeypatch.chdir 隔离副作用）：
  - config/.local-dek     — 32 字节独立 DEK
  - config/mcp-config.enc — [version u8][nonce 12][AES-GCM ct+tag]
  - config/.status        — via_cli / unconfigured / 其他

测试可测性原则：副作用从 CLI 入口剥离到 do_config_* helper，测试直接调。
"""
from __future__ import annotations

import argparse
import json
import os
import sys
from typing import Any, NoReturn, TextIO

import yaml

from ..keystore import (
    decrypt_config_from_file,
    encrypt_config_to_file,
    load_or_generate_dek,
)

# ---- 文件系统权限位（private — 当前用户读写）----
FILE_PERM_PRIVATE = 0o600
DIR_PERM_PRIVATE = 0o700

# ---- 配置文件路径（相对 cwd）----
CONFIG_DIR = "config"
CONFIG_DEK_PATH = f"{CONFIG_DIR}/.local-dek"
CONFIG_ENC_PATH = f"{CONFIG_DIR}/mcp-config.enc"
CONFIG_STATUS_PATH = f"{CONFIG_DIR}/.status"

# ---- 状态值常量（参见 sdk/python/src/ks_app/app.py configStatus 枚举）----
STATUS_VIA_CLI = "via_cli"
STATUS_UNCONFIGURED = "unconfigured"

# ---- 脱敏尾段长度：显示敏感字段最后 N 个字符 ----
REDACT_TAIL_LEN = 4

# ---- 敏感字段关键字（不分大小写 contains 匹配）----
SENSITIVE_KEYWORDS: tuple[str, ...] = ("key", "secret", "token", "password", "api")


# ---- 入口（argparse 分派；func 由 __main__.py set_defaults 注册）----


def cmd_config_set(args: argparse.Namespace) -> None:
    """config set 入口。支持 --file（批量）或 --key/--value（单字段）。

    失败走 exit_err → sys.exit(1)；成功 print 反馈。
    """
    file_path = getattr(args, "file", None)
    if file_path:
        try:
            do_config_set_from_file(file_path)
        except Exception as e:  # noqa: BLE001
            exit_err(f"{e}")
        print("配置已从文件导入（via_cli）")
        return

    key = getattr(args, "key", None)
    value = getattr(args, "value", None)
    if not key or not value:
        exit_err("--key 与 --value 或 --file 必填其一")

    try:
        do_config_set_kv(key, value)
    except Exception as e:  # noqa: BLE001
        exit_err(f"{e}")
    print("配置已更新（via_cli）")


def cmd_config_show(args: argparse.Namespace) -> None:
    """config show 入口。读配置并渲染到 stdout（敏感字段脱敏）。"""
    try:
        cfg = load_current_config_map()
    except Exception as e:  # noqa: BLE001
        exit_err(f"读取配置失败: {e}")
    render_config(sys.stdout, cfg)


def cmd_config_reset(args: argparse.Namespace) -> None:
    """config reset 入口。删除 enc 文件（幂等）+ 写 status = unconfigured。"""
    try:
        os.remove(CONFIG_ENC_PATH)
    except FileNotFoundError:
        pass  # 幂等
    except OSError as e:
        exit_err(f"删除加密配置失败: {e}")

    try:
        write_config_status(STATUS_UNCONFIGURED)
    except OSError as e:
        exit_err(f"写状态文件失败: {e}")
    print("配置已重置 (unconfigured)")


# ---- 可测 helper（副作用显式，供测试直接调）----


def do_config_set_kv(key: str, value: str) -> None:
    """读现有配置（不存在以空 dict 开始）→ 更新单字段 → 写回加密 → 状态 via_cli。

    错误按阶段包装（读取/保存/写状态），便于运维从消息前缀定位失败环节。
    """
    try:
        current = load_current_config_map()
    except Exception as e:  # noqa: BLE001
        raise RuntimeError(f"读取配置失败: {e}") from e
    if current is None:
        current = {}
    current[key] = value
    try:
        save_config_map(current)
    except Exception as e:  # noqa: BLE001
        raise RuntimeError(f"保存失败: {e}") from e
    try:
        write_config_status(STATUS_VIA_CLI)
    except Exception as e:  # noqa: BLE001
        raise RuntimeError(f"写状态文件失败: {e}") from e


def do_config_set_from_file(path: str) -> None:
    """从 YAML / JSON 文件批量导入。扩展名 .yaml / .yml → YAML，其他 → JSON。"""
    try:
        with open(path, "rb") as f:
            data = f.read()
    except OSError as e:
        raise RuntimeError(f"读文件失败: {e}") from e
    cfg: dict[str, Any]
    if path.endswith(".yaml") or path.endswith(".yml"):
        try:
            loaded = yaml.safe_load(data)
        except yaml.YAMLError as e:
            raise ValueError(f"YAML 解析失败: {e}") from e
        if loaded is None:
            cfg = {}
        elif isinstance(loaded, dict):
            cfg = loaded
        else:
            raise ValueError(f"YAML 解析失败: 根必须是 mapping，实际 {type(loaded).__name__}")
    else:
        try:
            loaded = json.loads(data.decode("utf-8"))
        except (json.JSONDecodeError, UnicodeDecodeError) as e:
            raise ValueError(f"JSON 解析失败: {e}") from e
        if not isinstance(loaded, dict):
            raise ValueError(f"JSON 解析失败: 根必须是 object，实际 {type(loaded).__name__}")
        cfg = loaded
    try:
        save_config_map(cfg)
    except Exception as e:  # noqa: BLE001
        raise RuntimeError(f"保存失败: {e}") from e
    try:
        write_config_status(STATUS_VIA_CLI)
    except Exception as e:  # noqa: BLE001
        raise RuntimeError(f"写状态文件失败: {e}") from e


def render_config(w: TextIO, cfg: dict[str, Any] | None) -> None:
    """把配置 dict 以 "key: value" 形式写入 w，敏感字段脱敏。

    cfg is None → 输出 "(未配置)"。
    key 按字母序排序（保证跨语言 conformance 字节级可比对）。

    注意：f"{k:<20}" 按 Python 字符数 pad，等价 Go `%-20s` **仅在 ASCII key 下**
    成立。CJK key 在两端字节不对齐；
    当前三语言 conformance 套件仅做 AAD canonical / fingerprint /
    payload 加解密比对，不比对 CLI 文本输出。
    """
    if cfg is None:
        w.write("(未配置)\n")
        return
    for k in sorted(cfg.keys()):
        v = cfg[k]
        v_str = _fmt_value(v)
        if is_sensitive_key(k):
            w.write(f"{k:<20}: ***{tail_n(v_str, REDACT_TAIL_LEN)}（已脱敏）\n")
        else:
            w.write(f"{k:<20}: {v_str}\n")


def load_current_config_map() -> dict[str, Any] | None:
    """解密并解析 mcp-config.enc 为 dict。文件不存在 → 返回 None（首次调用场景）。

    方案 A：先 os.path.exists 预检查，避开 decrypt_config_from_file 抛异常的
    wrap chain 不确定性（dek.py 第 135 行 open() 抛 FileNotFoundError）。
    """
    if not os.path.exists(CONFIG_ENC_PATH):
        return None
    dek = load_or_generate_dek(CONFIG_DEK_PATH)
    data = decrypt_config_from_file(CONFIG_ENC_PATH, dek)
    obj = json.loads(data.decode("utf-8"))
    if not isinstance(obj, dict):
        raise ValueError(f"mcp-config.enc 根必须是 object，实际 {type(obj).__name__}")
    return obj


def save_config_map(cfg: dict[str, Any]) -> None:
    """把 cfg 序列化为 JSON 后用 DEK 加密写入 mcp-config.enc。

    注意：ensure_ascii=False 保留 UTF-8 原字节（对齐 Go json.Marshal 默认 CJK 不转义），
    但 Python 默认**不转义** `<` / `>` / `&`，Go `json.Marshal` **默认转义** 为
    `\\u003c` / `\\u003e` / `\\u0026`（historical behavior，除非调用方设 Encoder
    SetEscapeHTML(false)）。配置值含这三个字符时两端 ciphertext 会分叉。
    conformance 只用 AAD canonical 比对，不经 JSON 字符转义路径。
    """
    dek = load_or_generate_dek(CONFIG_DEK_PATH)
    data = json.dumps(cfg, ensure_ascii=False).encode("utf-8")
    encrypt_config_to_file(CONFIG_ENC_PATH, dek, data)


def write_config_status(status: str) -> None:
    """写 config/.status 文件，记录当前配置来源状态。父目录不存在自动建。"""
    os.makedirs(CONFIG_DIR, mode=DIR_PERM_PRIVATE, exist_ok=True)
    fd = os.open(
        CONFIG_STATUS_PATH,
        os.O_WRONLY | os.O_CREAT | os.O_TRUNC,
        FILE_PERM_PRIVATE,
    )
    try:
        with os.fdopen(fd, "wb") as f:
            f.write(status.encode("utf-8"))
    except Exception:
        try:
            os.remove(CONFIG_STATUS_PATH)
        except OSError:
            pass
        raise


def is_sensitive_key(k: str) -> bool:
    """判断 key 是否命中敏感关键字（大小写不敏感 contains 匹配）。"""
    lower = k.lower()
    return any(needle in lower for needle in SENSITIVE_KEYWORDS)


def tail_n(s: str, n: int) -> str:
    """返回 s 末尾 n 个字符；len(s) <= n 时返回 s 本身。"""
    if len(s) <= n:
        return s
    return s[-n:]


# ---- 内部小工具 ----


def _fmt_value(v: Any) -> str:
    """把任意配置值格式化为字符串（镜像 Go %v 对标量的行为）。

    - bool → "true"/"false"（对齐 Go fmt.Sprintf("%v", true)）
    - None → "<nil>"（罕见；不应在配置中出现）
    - 其他 → str(v)
    """
    if isinstance(v, bool):
        return "true" if v else "false"
    if v is None:
        return "<nil>"
    return str(v)


def exit_err(msg: str) -> NoReturn:
    """打印错误消息到 stderr 并退出码 1（CLI 入口的错误终止 helper）。

    返回 NoReturn 让 type checker 知道此函数永不返回（sys.exit(1) 抛 SystemExit），
    避免下游代码被误判为 unreachable 的虚假 dead-code 警告。
    """
    print(msg, file=sys.stderr)
    sys.exit(1)
