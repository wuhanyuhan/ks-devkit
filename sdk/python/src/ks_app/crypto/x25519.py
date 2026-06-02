"""X25519-ECDH 密钥交换 + HKDF-SHA256 KEK 派生。

镜像 Go sdk/go/ksapp/crypto/x25519.go。三语言（Go / TS / Python）必须字节级互通：
  - HKDF info 串："ksapp-config-v1"（HKDF_INFO 常量）
  - HKDF salt：32 字节全零
  - X25519 公私钥均为 32 字节

本模块只暴露原语；错误码包装（ERR_DECRYPT 等）由上层 endpoint handler 负责。
所有 runtime error（参数长度、解码失败等）统一抛 ValueError；敏感字节不进 message。
"""
from __future__ import annotations

from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric.x25519 import (
    X25519PrivateKey,
    X25519PublicKey,
)
from cryptography.hazmat.primitives.kdf.hkdf import HKDF

# HKDF 派生 KEK 时使用的 info 串。三语言（Go / TS / Python）完全一致，禁止修改。
HKDF_INFO: bytes = b"ksapp-config-v1"

# 固定字节长度常量。
X25519_PUBKEY_LEN: int = 32
X25519_PRIVKEY_LEN: int = 32
KEK_LEN: int = 32  # AES-256 密钥长度


def generate_x25519() -> tuple[bytes, bytes]:
    """生成一对随机 X25519 密钥。

    返回：(privkey 32B, pubkey 32B)
    """
    priv = X25519PrivateKey.generate()
    priv_bytes = priv.private_bytes(
        encoding=serialization.Encoding.Raw,
        format=serialization.PrivateFormat.Raw,
        encryption_algorithm=serialization.NoEncryption(),
    )
    pub_bytes = priv.public_key().public_bytes(
        encoding=serialization.Encoding.Raw,
        format=serialization.PublicFormat.Raw,
    )
    return priv_bytes, pub_bytes


def x25519_ecdh(privkey: bytes, peer_pubkey: bytes) -> bytes:
    """用本端 privkey 与对端 pubkey 执行 X25519-ECDH，返回 32 字节共享秘密。

    privkey / peer_pubkey 必须均为 32 字节，否则 ValueError。
    """
    if len(privkey) != X25519_PRIVKEY_LEN:
        raise ValueError(
            f"crypto: privkey 长度 = {len(privkey)}, 期望 {X25519_PRIVKEY_LEN}"
        )
    if len(peer_pubkey) != X25519_PUBKEY_LEN:
        raise ValueError(
            f"crypto: peer_pubkey 长度 = {len(peer_pubkey)}, 期望 {X25519_PUBKEY_LEN}"
        )
    priv_obj = X25519PrivateKey.from_private_bytes(privkey)
    pub_obj = X25519PublicKey.from_public_bytes(peer_pubkey)
    shared = priv_obj.exchange(pub_obj)
    return shared


def derive_pubkey_from_privkey(privkey: bytes) -> tuple[bytes, bytes]:
    """从 32 字节 X25519 私钥派生对应公钥。

    对齐 Go DeriveX25519Pub：返回规范化的 (privkey 副本, pubkey) 二元组，便于
    keystore 从持久化的私钥字节重建完整密钥对。

    长度错误抛 ValueError。
    """
    if len(privkey) != X25519_PRIVKEY_LEN:
        raise ValueError(
            f"crypto: privkey 长度 = {len(privkey)}, 期望 {X25519_PRIVKEY_LEN}"
        )
    priv_obj = X25519PrivateKey.from_private_bytes(privkey)
    priv_bytes = priv_obj.private_bytes(
        encoding=serialization.Encoding.Raw,
        format=serialization.PrivateFormat.Raw,
        encryption_algorithm=serialization.NoEncryption(),
    )
    pub_bytes = priv_obj.public_key().public_bytes(
        encoding=serialization.Encoding.Raw,
        format=serialization.PublicFormat.Raw,
    )
    return priv_bytes, pub_bytes


def derive_kek(shared: bytes) -> bytes:
    """用 HKDF-SHA256 从 X25519 共享秘密派生 32 字节 KEK。

    - salt = 32 字节全零（三语言一致）
    - info = HKDF_INFO 常量
    - 输出长度 = KEK_LEN (32)

    shared 为空 → ValueError。
    """
    if not shared:
        raise ValueError("crypto: shared secret 不能为空")
    kek = HKDF(
        algorithm=hashes.SHA256(),
        length=KEK_LEN,
        salt=bytes(32),  # 32 字节全零
        info=HKDF_INFO,
    ).derive(shared)
    return kek
