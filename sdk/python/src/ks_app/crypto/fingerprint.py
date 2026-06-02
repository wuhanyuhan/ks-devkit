"""公钥指纹。

镜像 Go ks-types kstypes.Fingerprint：

    fingerprint(pubkey) = sha256(pubkey)[:16].hex 分成 8 组 4 个字符，用 `:` 连接

范例：
    pubkey = bytes(32)
    → sha256(32 零).digest()[:16].hex = "66687aadf862bd776c8fc18b8e9f8e20"
    → "6668:7aad:f862:bd77:6c8f:c18b:8e9f:8e20"
"""
from __future__ import annotations

import hashlib

from .x25519 import X25519_PUBKEY_LEN


def fingerprint(pubkey: bytes) -> str:
    """计算 X25519 公钥的 fingerprint 字符串。

    pubkey 必须 32 字节；长度不符 → ValueError。
    """
    if len(pubkey) != X25519_PUBKEY_LEN:
        raise ValueError(
            f"crypto: fingerprint pubkey 长度 = {len(pubkey)}, 期望 {X25519_PUBKEY_LEN}"
        )
    h = hashlib.sha256(pubkey).digest()[:16]
    hex_str = h.hex()
    return ":".join(hex_str[i : i + 4] for i in range(0, 32, 4))
