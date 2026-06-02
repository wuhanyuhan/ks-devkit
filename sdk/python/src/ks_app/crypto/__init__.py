"""ks_app.crypto — 端到端加密通道低层原语。

暴露三组原语：
  - X25519-ECDH + HKDF-SHA256：generate_x25519 / x25519_ecdh / derive_kek /
    derive_pubkey_from_privkey
  - AES-256-GCM：encrypt_aes_gcm / decrypt_aes_gcm
  - AAD canonical bytes + pubkey fingerprint：aad_canonical_bytes / fingerprint

三语言（Go / TS / Python）字节级互通，常量必须一致：
  - HKDF_INFO = b"ksapp-config-v1"
  - AES_GCM_NONCE_LEN = 12
  - KEK_LEN / X25519_PUBKEY_LEN / X25519_PRIVKEY_LEN = 32
"""
from .aad import aad_canonical_bytes
from .aesgcm import AES_GCM_NONCE_LEN, decrypt_aes_gcm, encrypt_aes_gcm
from .fingerprint import fingerprint
from .x25519 import (
    HKDF_INFO,
    KEK_LEN,
    X25519_PRIVKEY_LEN,
    X25519_PUBKEY_LEN,
    derive_kek,
    derive_pubkey_from_privkey,
    generate_x25519,
    x25519_ecdh,
)

__all__ = [
    "HKDF_INFO",
    "AES_GCM_NONCE_LEN",
    "KEK_LEN",
    "X25519_PRIVKEY_LEN",
    "X25519_PUBKEY_LEN",
    "aad_canonical_bytes",
    "decrypt_aes_gcm",
    "derive_kek",
    "derive_pubkey_from_privkey",
    "encrypt_aes_gcm",
    "fingerprint",
    "generate_x25519",
    "x25519_ecdh",
]
