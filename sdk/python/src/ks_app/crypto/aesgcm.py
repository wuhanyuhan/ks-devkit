"""AES-256-GCM 认证加密。

镜像 Go sdk/go/ksapp/crypto/aesgcm.go。nonce 长度 12 字节，tag 16 字节附加在
ciphertext 末尾（cryptography 的 AESGCM.encrypt 默认行为）。

错误码包装（ERR_DECRYPT）由上层 endpoint handler 负责；本层的解密失败抛
`cryptography.exceptions.InvalidTag`（由上游库抛出），交由调用方处理。
参数长度错误统一抛 ValueError，敏感字节不进 message。
"""
from __future__ import annotations

import os

from cryptography.hazmat.primitives.ciphers.aead import AESGCM

from .x25519 import KEK_LEN

# AES-256-GCM 推荐 nonce 长度（12 字节）。三语言一致。
AES_GCM_NONCE_LEN: int = 12


def encrypt_aes_gcm(
    kek: bytes, plaintext: bytes, aad: bytes | None
) -> tuple[bytes, bytes]:
    """用 AES-256-GCM 加密 plaintext，附加 aad 做认证。

    参数：
      - kek：32 字节 AES-256 密钥（由 derive_kek 派生，或独立 DEK）
      - plaintext：待加密明文
      - aad：附加认证数据（典型由 aad_canonical_bytes 生成；落盘场景可为 None）

    返回：
      - ciphertext：AES-GCM 密文 + 16 字节 tag（合并为单 bytes，与 Go 兼容）
      - nonce：12 字节随机 nonce（每次重新生成）

    kek 长度错误 → ValueError。
    """
    if len(kek) != KEK_LEN:
        raise ValueError(f"crypto: kek 长度 = {len(kek)}, 期望 {KEK_LEN}")
    nonce = os.urandom(AES_GCM_NONCE_LEN)
    aesgcm = AESGCM(kek)
    ciphertext = aesgcm.encrypt(nonce, plaintext, aad)
    return ciphertext, nonce


def decrypt_aes_gcm(
    kek: bytes, nonce: bytes, ciphertext: bytes, aad: bytes | None
) -> bytes:
    """用 AES-256-GCM 解密 ciphertext，并校验 aad。

    参数：
      - kek：32 字节 AES-256 密钥（与加密端一致）
      - nonce：12 字节 nonce
      - ciphertext：AES-GCM 密文（含 16 字节 tag）
      - aad：附加认证数据（必须与加密时完全一致）

    异常：
      - kek / nonce 长度错误 → ValueError
      - GCM tag 校验失败（含 aad 不一致、密文被改、密钥错误）→
        `cryptography.exceptions.InvalidTag`（由上游库抛出，透传）

    不在本层包装 ERR_DECRYPT；错误码语义由上层 endpoint handler 决定。
    """
    if len(kek) != KEK_LEN:
        raise ValueError(f"crypto: kek 长度 = {len(kek)}, 期望 {KEK_LEN}")
    if len(nonce) != AES_GCM_NONCE_LEN:
        raise ValueError(f"crypto: nonce 长度 = {len(nonce)}, 期望 {AES_GCM_NONCE_LEN}")
    aesgcm = AESGCM(kek)
    plaintext = aesgcm.decrypt(nonce, ciphertext, aad)
    return plaintext
