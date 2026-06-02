"""X25519 私钥三来源加载 + 双密钥并存。

镜像 Go sdk/go/ksapp/keystore/loader.go。

优先级互斥（高 → 低）：
  1. 环境变量 KSAPP_MCP_PRIVKEY_B64 [ + KSAPP_MCP_PRIVKEY_OLD_B64 ]
  2. K8s Secret 文件 KSAPP_MCP_PRIVKEY_FILE 或 /secrets/mcp-key [ + _OLD_FILE ]
  3. Fallback 文件 config/.mcp-key（首次自动生成 JSON） [ + .mcp-key.old ]
"""
from __future__ import annotations

import base64
import json
import os
from dataclasses import dataclass, field
from datetime import datetime, timezone
from enum import IntEnum
from pathlib import Path

from ..crypto import X25519_PRIVKEY_LEN, X25519_PUBKEY_LEN
from ..crypto import derive_pubkey_from_privkey, fingerprint, generate_x25519

# ---- 常量（三语言必须一致，禁止修改）----

ENV_PRIVKEY_B64 = "KSAPP_MCP_PRIVKEY_B64"
ENV_PRIVKEY_OLD_B64 = "KSAPP_MCP_PRIVKEY_OLD_B64"
ENV_PRIVKEY_FILE = "KSAPP_MCP_PRIVKEY_FILE"
ENV_PRIVKEY_OLD_FILE = "KSAPP_MCP_PRIVKEY_OLD_FILE"

DEFAULT_SECRET_FILE = "/secrets/mcp-key"
DEFAULT_SECRET_FILE_OLD = "/secrets/mcp-key-old"
DEFAULT_FALLBACK_FILE = "config/.mcp-key"
DEFAULT_FALLBACK_OLD = "config/.mcp-key.old"

KEY_FILE_MODE = 0o600
KEY_DIR_MODE = 0o700

# .mcp-key JSON 版本号（当前 v1）。
PERSISTED_KEY_VERSION = 1

# 文件损坏时备份用的后缀。
CORRUPTED_SUFFIX = ".corrupted"


# ---- Source 枚举 ----


class Source(IntEnum):
    """密钥来源。0 视为未初始化，从 1 起。"""

    ENV = 1
    SECRET_FILE = 2
    FALLBACK_FILE = 3

    def __str__(self) -> str:
        return {
            Source.ENV: "env",
            Source.SECRET_FILE: "secret-file",
            Source.FALLBACK_FILE: "fallback-file",
        }[self]


# ---- 数据结构 ----


@dataclass
class Keypair:
    """一对 X25519 密钥及其指纹。"""

    privkey: bytes  # 32 bytes X25519 私钥
    pubkey: bytes  # 32 bytes X25519 公钥
    fingerprint: str  # 公钥 fingerprint
    created_at: datetime  # UTC，仅 fallback 文件保留有效；env / secret 近似 Load 时间


@dataclass
class Keystore:
    """一次 Load 的完整结果：当前 Primary + 可选 Old 轮换密钥。"""

    primary: Keypair
    old: Keypair | None
    source: Source


@dataclass
class LoadOptions:
    """控制 Load 的来源路径。零值字段由 apply_defaults 用 env / 默认常量填充。"""

    secret_file: str = ""
    secret_file_old: str = ""
    fallback_file: str = ""
    fallback_old: str = ""

    def apply_defaults(self) -> "LoadOptions":
        """用环境变量与默认常量填充零值字段，返回补全后的副本。"""
        return LoadOptions(
            secret_file=self.secret_file
            or _env_or_default(ENV_PRIVKEY_FILE, DEFAULT_SECRET_FILE),
            secret_file_old=self.secret_file_old
            or _env_or_default(ENV_PRIVKEY_OLD_FILE, DEFAULT_SECRET_FILE_OLD),
            fallback_file=self.fallback_file or DEFAULT_FALLBACK_FILE,
            fallback_old=self.fallback_old or DEFAULT_FALLBACK_OLD,
        )


# ---- 公开 API ----


def load(opts: LoadOptions | None = None) -> Keystore:
    """按优先级（env → secret-file → fallback-file）加载 X25519 私钥。

    返回 Keystore：primary 必非 None；old 视来源是否提供。fallback 来源若文件
    不存在会自动生成新对并写入；若文件存在但损坏则备份为 .corrupted 后重生。

    None opts 等价于零值 LoadOptions（全部默认路径）。

    异常：
      - env / secret-file 解码或长度错误 → ValueError（运维管控，必须暴露）
      - fallback 文件 IO 失败（非损坏自愈分支）→ OSError
    """
    o = (opts or LoadOptions()).apply_defaults()

    # 优先级 1：env
    env_val = os.environ.get(ENV_PRIVKEY_B64, "")
    if env_val:
        primary = _keypair_from_b64_env(env_val)
        old: Keypair | None = None
        old_val = os.environ.get(ENV_PRIVKEY_OLD_B64, "")
        if old_val:
            old = _keypair_from_b64_env(old_val)
        return Keystore(primary=primary, old=old, source=Source.ENV)

    # 优先级 2：Secret 文件
    if _file_exists(o.secret_file):
        primary = _keypair_from_b64_file(o.secret_file)
        old = None
        if _file_exists(o.secret_file_old):
            # SecretFileOld 损坏视为致命（运维管控，必须暴露）
            old = _keypair_from_b64_file(o.secret_file_old)
        return Keystore(primary=primary, old=old, source=Source.SECRET_FILE)

    # 优先级 3：Fallback 文件
    primary = _load_or_generate_fallback(o.fallback_file)
    old = None
    if _file_exists(o.fallback_old):
        try:
            old = _read_mcp_key(o.fallback_old)
        except Exception:
            # FallbackOld 损坏静默忽略：dev / single-replica 自愈侧（有意区别于
            # SecretFileOld 路径的 fail-fast 语义）。
            old = None
    return Keystore(primary=primary, old=old, source=Source.FALLBACK_FILE)


# ---- 内部 helper ----


def _env_or_default(key: str, fallback: str) -> str:
    v = os.environ.get(key, "")
    return v if v else fallback


def _file_exists(path: str) -> bool:
    if not path:
        return False
    p = Path(path)
    return p.exists() and p.is_file()


def _decode_b64_privkey(s: str, url_first: bool) -> bytes:
    """解码 base64 编码的私钥并校验长度。

    对齐 Go decodeB64Privkey：失败时尝试另一编码做 fallback，便于人工手填两种
    base64 变体。url_first=True 优先 URL-safe（env），False 优先 Standard（文件）。

    Python base64 两种 API 都要求 padding；本函数自动补齐 '=' 到长度整除 4。
    """
    s = s.strip()
    if not s:
        raise ValueError("keystore: base64 字符串为空")

    def _pad(x: str) -> str:
        rem = len(x) % 4
        return x if rem == 0 else x + "=" * (4 - rem)

    encodings = (
        [base64.urlsafe_b64decode, base64.b64decode]
        if url_first
        else [base64.b64decode, base64.urlsafe_b64decode]
    )
    padded = _pad(s)
    last_err: Exception | None = None
    for decoder in encodings:
        try:
            b = decoder(padded)
        except Exception as e:  # noqa: BLE001
            last_err = e
            continue
        if len(b) != X25519_PRIVKEY_LEN:
            raise ValueError(
                f"keystore: privkey 长度 = {len(b)}, 期望 {X25519_PRIVKEY_LEN}"
            )
        return b
    raise ValueError(
        f"keystore: base64 解码失败（两种编码都试过）: {last_err}"
    )


def _read_b64_from_file(path: str) -> str:
    """读取 base64 文件内容（剥 trailing 空白）。"""
    with open(path, "rb") as f:
        data = f.read()
    return data.decode("utf-8").strip()


def _keypair_from_privkey(priv: bytes) -> Keypair:
    """由 32 字节私钥派生公钥并构造 Keypair（created_at = now UTC）。"""
    priv_copy, pub = derive_pubkey_from_privkey(priv)
    return Keypair(
        privkey=priv_copy,
        pubkey=pub,
        fingerprint=fingerprint(pub),
        created_at=datetime.now(timezone.utc),
    )


def _keypair_from_b64_env(s: str) -> Keypair:
    priv = _decode_b64_privkey(s, url_first=True)
    return _keypair_from_privkey(priv)


def _keypair_from_b64_file(path: str) -> Keypair:
    s = _read_b64_from_file(path)
    priv = _decode_b64_privkey(s, url_first=False)
    return _keypair_from_privkey(priv)


def _load_or_generate_fallback(path: str) -> Keypair:
    """处理 fallback 文件的加载或首次生成 + 损坏自愈。"""
    p = Path(path)
    if not p.exists():
        return _generate_and_write_fallback(path)
    if p.is_dir():
        raise ValueError(f"keystore: {path} 是目录，期望文件")
    try:
        return _read_mcp_key(path)
    except Exception:
        # 损坏：备份为 .corrupted，再递归重生
        backup = path + CORRUPTED_SUFFIX
        os.rename(path, backup)
        return _load_or_generate_fallback(path)


def _generate_and_write_fallback(path: str) -> Keypair:
    """在 path 处生成新密钥对，写文件后返回 Keypair。"""
    priv, pub = generate_x25519()
    kp = Keypair(
        privkey=priv,
        pubkey=pub,
        fingerprint=fingerprint(pub),
        created_at=datetime.now(timezone.utc),
    )
    parent = Path(path).parent
    if str(parent) and str(parent) != ".":
        parent.mkdir(mode=KEY_DIR_MODE, parents=True, exist_ok=True)
    _write_mcp_key(path, kp)
    return kp


def _write_mcp_key(path: str, kp: Keypair) -> None:
    """把 Keypair 以 JSON 形式原子写入 path（先写 .tmp 再 rename），权限 0600。"""
    created_at = kp.created_at
    if created_at is None:
        created_at = datetime.now(timezone.utc)
    # 对齐 Go time.RFC3339：省略微秒，用 Z 表示 UTC
    created_at_utc = created_at.astimezone(timezone.utc).replace(microsecond=0)
    created_at_str = created_at_utc.strftime("%Y-%m-%dT%H:%M:%SZ")
    payload = {
        "version": PERSISTED_KEY_VERSION,
        "pubkey": base64.b64encode(kp.pubkey).decode("ascii"),
        "privkey": base64.b64encode(kp.privkey).decode("ascii"),
        "created_at": created_at_str,
        "fingerprint": kp.fingerprint,
    }
    data = json.dumps(payload, indent=2).encode("utf-8")
    parent = Path(path).parent
    if str(parent) and str(parent) != ".":
        parent.mkdir(mode=KEY_DIR_MODE, parents=True, exist_ok=True)
    tmp = path + ".tmp"
    # 原子写：open + O_CREAT | O_WRONLY + 0o600，然后 rename
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, KEY_FILE_MODE)
    try:
        with os.fdopen(fd, "wb") as f:
            f.write(data)
    except Exception:
        try:
            os.remove(tmp)
        except OSError:
            pass
        raise
    try:
        os.replace(tmp, path)
    except Exception:
        try:
            os.remove(tmp)
        except OSError:
            pass
        raise


def _read_mcp_key(path: str) -> Keypair:
    """解析并校验 .mcp-key 文件，返回 Keypair。

    校验：
      - version 必须等于 PERSISTED_KEY_VERSION
      - pubkey / privkey base64 解码后必须 32 字节
      - fingerprint(pubkey) 必须等于 stored fingerprint（防篡改）
    """
    with open(path, "rb") as f:
        data = f.read()
    obj = json.loads(data.decode("utf-8"))
    if not isinstance(obj, dict):
        raise ValueError(f"keystore: {path} JSON 根必须是 object")
    ver = obj.get("version")
    if ver != PERSISTED_KEY_VERSION:
        raise ValueError(
            f"keystore: 不支持的 version = {ver}, 期望 {PERSISTED_KEY_VERSION}"
        )
    pub_b64 = obj.get("pubkey", "")
    priv_b64 = obj.get("privkey", "")
    if not isinstance(pub_b64, str) or not isinstance(priv_b64, str):
        raise ValueError("keystore: pubkey / privkey 必须是 base64 字符串")
    try:
        pub = base64.b64decode(pub_b64, validate=True)
    except Exception as e:
        raise ValueError(f"keystore: pubkey base64 解码失败: {e}") from e
    if len(pub) != X25519_PUBKEY_LEN:
        raise ValueError(
            f"keystore: pubkey 长度 = {len(pub)}, 期望 {X25519_PUBKEY_LEN}"
        )
    try:
        priv = base64.b64decode(priv_b64, validate=True)
    except Exception as e:
        raise ValueError(f"keystore: privkey base64 解码失败: {e}") from e
    if len(priv) != X25519_PRIVKEY_LEN:
        raise ValueError(
            f"keystore: privkey 长度 = {len(priv)}, 期望 {X25519_PRIVKEY_LEN}"
        )
    stored_fp = obj.get("fingerprint", "")
    exp_fp = fingerprint(pub)
    if exp_fp != stored_fp:
        raise ValueError(
            f"keystore: fingerprint 不匹配: stored={stored_fp}, computed={exp_fp}"
        )
    created_at_str = obj.get("created_at", "")
    try:
        # 兼容 RFC3339 的 Z 后缀
        if isinstance(created_at_str, str) and created_at_str.endswith("Z"):
            created_at = datetime.fromisoformat(
                created_at_str[:-1] + "+00:00"
            )
        else:
            created_at = datetime.fromisoformat(created_at_str)
    except Exception:
        created_at = datetime.fromtimestamp(0, tz=timezone.utc)
    return Keypair(
        privkey=priv,
        pubkey=pub,
        fingerprint=stored_fp,
        created_at=created_at.astimezone(timezone.utc),
    )
