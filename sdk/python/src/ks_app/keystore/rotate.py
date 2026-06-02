"""X25519 密钥轮换。

镜像 Go sdk/go/ksapp/keystore/rotate.go。两种模式：
  - print_only=True：env / Secret 模式，仅返回 base64 + fingerprint，不写文件。
  - print_only=False：文件模式，把当前 fallback_file 搬到 fallback_old，然后
    写新对到 fallback_file（失败回滚 rename，避免中间态）。

写失败回滚 rename 的原因（对齐 Go）：避免留下 "primary 不见、old 是上一代
primary" 的中间态，否则下一次 load 会触发自动 fallback 重生，运维感知不到丢失。
"""
from __future__ import annotations

import base64
import os
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
from pathlib import Path

from ..crypto import fingerprint, generate_x25519
from .loader import (
    DEFAULT_FALLBACK_FILE,
    DEFAULT_FALLBACK_OLD,
    Keypair,
    _file_exists,
    _write_mcp_key,
)

# old 密钥保留窗口推荐值（7 天）。实际清理由调用方（CLI / 定时任务）触发。
OLD_KEY_RETENTION_DAYS: int = 7
OLD_KEY_RETENTION: timedelta = timedelta(days=OLD_KEY_RETENTION_DAYS)


@dataclass
class RotateOptions:
    """控制 rotate 的行为。零值字段由 apply_defaults 用默认常量填充。"""

    fallback_file: str = ""
    fallback_old: str = ""
    print_only: bool = False

    def apply_defaults(self) -> "RotateOptions":
        return RotateOptions(
            fallback_file=self.fallback_file or DEFAULT_FALLBACK_FILE,
            fallback_old=self.fallback_old or DEFAULT_FALLBACK_OLD,
            print_only=self.print_only,
        )


@dataclass
class RotateResult:
    """rotate 的产物：新密钥对的 base64 + fingerprint，以及（文件模式）写入的
    文件清单（审计 + CLI 回显）。"""

    new_privkey_b64: str  # base64.b64encode（带 padding）
    new_pubkey_b64: str
    fingerprint: str
    files_written: list[str] = field(default_factory=list)  # print_only 模式为空


def rotate(opts: RotateOptions | None = None) -> RotateResult:
    """生成新 X25519 密钥对，按模式落盘或仅打印。

    None opts 等价于零值 RotateOptions（全部默认路径，print_only=False）。
    """
    o = (opts or RotateOptions()).apply_defaults()

    priv, pub = generate_x25519()
    fp = fingerprint(pub)
    res = RotateResult(
        new_privkey_b64=base64.b64encode(priv).decode("ascii"),
        new_pubkey_b64=base64.b64encode(pub).decode("ascii"),
        fingerprint=fp,
        files_written=[],
    )
    if o.print_only:
        return res

    # 文件模式：把当前 fallback_file 搬到 fallback_old（覆盖更旧的 .old）
    old_existed = _file_exists(o.fallback_file)
    if old_existed:
        # 确保目标父目录存在
        parent_old = Path(o.fallback_old).parent
        if str(parent_old) and str(parent_old) != ".":
            parent_old.mkdir(mode=0o700, parents=True, exist_ok=True)
        try:
            os.replace(o.fallback_file, o.fallback_old)
        except OSError as e:
            raise OSError(
                f"keystore: 搬迁旧密钥 {o.fallback_file} → {o.fallback_old} 失败: {e}"
            ) from e

    new_kp = Keypair(
        privkey=priv,
        pubkey=pub,
        fingerprint=fp,
        created_at=datetime.now(timezone.utc),
    )
    try:
        _write_mcp_key(o.fallback_file, new_kp)
    except Exception as e:
        # 回滚 rename，避免中间态
        if old_existed:
            try:
                os.replace(o.fallback_old, o.fallback_file)
            except OSError as rb_err:
                raise OSError(
                    f"keystore: 写新密钥到 {o.fallback_file} 失败（{e}）"
                    f"且回滚 rename 失败: {rb_err}"
                ) from e
        raise OSError(f"keystore: 写新密钥到 {o.fallback_file} 失败: {e}") from e

    if old_existed:
        res.files_written.append(o.fallback_old)
    res.files_written.append(o.fallback_file)
    return res


def prune_old(path: str = "") -> None:
    """删除指定路径的旧密钥文件（典型为 config/.mcp-key.old）。

    空字符串 path → 使用 DEFAULT_FALLBACK_OLD。

    文件不存在视为错误（抛 FileNotFoundError），让运维明确知道清理动作没生效
    （对齐 Go PruneOld 行为）。
    """
    if not path:
        path = DEFAULT_FALLBACK_OLD
    os.remove(path)  # 不存在抛 FileNotFoundError
