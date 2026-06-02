"""AAD canonical 字节序编码（canonical bytes）。

镜像 Go ks-types kstypes.AADCanonicalBytes。三语言（Go / TS / Python）必须
字节级互通：

    u16_be(len(mcp_server_id)) || utf8(mcp_server_id)
      || u64_be(config_version)
      || u16_be(len(fingerprint)) || utf8(fingerprint)

用 Python struct.pack 镜像 Go binary.BigEndian。
"""
from __future__ import annotations

import struct


def aad_canonical_bytes(
    mcp_server_id: str, config_version: int, fingerprint: str
) -> bytes:
    """生成 AES-GCM AAD 的 canonical 字节串。

    参数：
      - mcp_server_id：utf-8 字符串（255 字节以内；上层 Schema 限制，本函数不强制校验）
      - config_version：u64 版本号（0 ≤ v ≤ 2^63-1；负数抛 struct.error）
      - fingerprint：公钥 fingerprint 字符串（典型 39 字节）

    返回：bytes 串，可直接传给 AES-GCM AEAD 的 aad 参数。

    字节序：所有长度前缀与版本号用大端（big-endian），与 Go binary.BigEndian 一致。
    """
    id_b = mcp_server_id.encode("utf-8")
    fp_b = fingerprint.encode("utf-8")
    return (
        struct.pack(">H", len(id_b))
        + id_b
        + struct.pack(">Q", config_version)
        + struct.pack(">H", len(fp_b))
        + fp_b
    )
