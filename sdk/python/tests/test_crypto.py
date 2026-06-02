"""crypto 模块测试：基础 10 条 + AAD golden 12 条 + fingerprint golden 8 条。

golden 从 conformance/config-schema/testvectors.json 加载；symlink 挂在
tests/testdata/testvectors.json。要求字节级对齐 Go + TS。
"""
from __future__ import annotations

import json
from pathlib import Path

import pytest
from cryptography.exceptions import InvalidTag

from ks_app.crypto import (
    AES_GCM_NONCE_LEN,
    KEK_LEN,
    aad_canonical_bytes,
    decrypt_aes_gcm,
    derive_kek,
    derive_pubkey_from_privkey,
    encrypt_aes_gcm,
    fingerprint,
    generate_x25519,
    x25519_ecdh,
)

TESTVECTORS_PATH = Path(__file__).parent / "testdata" / "testvectors.json"


def _load_vectors() -> dict:
    with TESTVECTORS_PATH.open() as f:
        return json.load(f)


def _load_aad_vectors() -> list[dict]:
    return _load_vectors()["aad_canonical"]


def _load_fingerprint_vectors() -> list[dict]:
    return _load_vectors()["fingerprint"]


# ---- 基础 10 条 ----


def test_x25519_roundtrip():
    """X25519 ECDH：A×B == B×A。"""
    a_priv, a_pub = generate_x25519()
    b_priv, b_pub = generate_x25519()
    assert len(a_priv) == 32 and len(a_pub) == 32
    shared_a = x25519_ecdh(a_priv, b_pub)
    shared_b = x25519_ecdh(b_priv, a_pub)
    assert shared_a == shared_b
    assert len(shared_a) == 32


def test_x25519_invalid_privkey_length():
    """privkey 长度 != 32 抛 ValueError。"""
    with pytest.raises(ValueError, match="privkey"):
        x25519_ecdh(b"\x00" * 33, b"\x00" * 32)


def test_x25519_invalid_pubkey_length():
    """peer_pubkey 长度 != 32 抛 ValueError。"""
    with pytest.raises(ValueError, match="peer_pubkey"):
        x25519_ecdh(b"\x00" * 32, b"\x00" * 31)


def test_derive_kek_deterministic():
    """同 shared 派生两次 → 同 KEK；长度 32。"""
    shared = b"\x11" * 32
    k1 = derive_kek(shared)
    k2 = derive_kek(shared)
    assert k1 == k2
    assert len(k1) == KEK_LEN == 32


def test_derive_kek_different_shared_different_kek():
    """不同 shared → 不同 KEK。"""
    k1 = derive_kek(b"\x11" * 32)
    k2 = derive_kek(b"\x22" * 32)
    assert k1 != k2


def test_aesgcm_roundtrip():
    """加密后解密 == plaintext。"""
    kek = b"\xaa" * 32
    plaintext = b"hello secret config payload"
    aad = aad_canonical_bytes(
        "ks-mcp-test", 1, "0000:0000:0000:0000:0000:0000:0000:0000"
    )
    ct, nonce = encrypt_aes_gcm(kek, plaintext, aad)
    assert len(nonce) == AES_GCM_NONCE_LEN == 12
    pt = decrypt_aes_gcm(kek, nonce, ct, aad)
    assert pt == plaintext


def test_aesgcm_wrong_aad_fails():
    """加密用 aad1，解密用 aad2 → InvalidTag。"""
    kek = b"\xbb" * 32
    pt = b"payload"
    aad1 = aad_canonical_bytes("ks-mcp-test", 1, "a" * 39)
    aad2 = aad_canonical_bytes("ks-mcp-test", 2, "a" * 39)
    ct, nonce = encrypt_aes_gcm(kek, pt, aad1)
    with pytest.raises(InvalidTag):
        decrypt_aes_gcm(kek, nonce, ct, aad2)


def test_aesgcm_wrong_kek_fails():
    """加密用 kek1，解密用 kek2 → InvalidTag。"""
    kek1 = b"\x01" * 32
    kek2 = b"\x02" * 32
    aad = b"\x00"
    ct, nonce = encrypt_aes_gcm(kek1, b"payload", aad)
    with pytest.raises(InvalidTag):
        decrypt_aes_gcm(kek2, nonce, ct, aad)


def test_derive_pubkey_from_privkey():
    """从 privkey 派生 pubkey 应该与 generate_x25519 的 pubkey 一致。"""
    priv, pub_original = generate_x25519()
    priv_copy, pub_derived = derive_pubkey_from_privkey(priv)
    assert priv_copy == priv
    assert pub_derived == pub_original


def test_fingerprint_all_zero():
    """fingerprint(bytes(32)) 的确定值（来自 testvectors）。"""
    got = fingerprint(bytes(32))
    assert got == "6668:7aad:f862:bd77:6c8f:c18b:8e9f:8e20"


# ---- Golden vectors：AAD canonical bytes（12 条）----


@pytest.mark.parametrize("vec", _load_aad_vectors(), ids=lambda v: v["name"])
def test_aad_canonical_vectors(vec):
    """字节级对齐 Go / TS：12 条 AAD canonical 向量。"""
    got = aad_canonical_bytes(
        vec["mcp_server_id"],
        vec["config_version"],
        vec["fingerprint"],
    )
    expected = bytes.fromhex(vec["expected_bytes_hex"].replace(" ", ""))
    assert got == expected, (
        f"AAD canonical 字节不匹配（vec={vec['name']}）: "
        f"got={got.hex()}, want={expected.hex()}"
    )


# ---- Golden vectors：fingerprint（8 条）----


@pytest.mark.parametrize(
    "vec", _load_fingerprint_vectors(), ids=lambda v: v["name"]
)
def test_fingerprint_vectors(vec):
    """字节级对齐 Go / TS：8 条 fingerprint 向量。"""
    pk = bytes.fromhex(vec["pubkey_hex"])
    got = fingerprint(pk)
    assert got == vec["expected_fingerprint"], (
        f"fingerprint 不匹配（vec={vec['name']}）: "
        f"got={got}, want={vec['expected_fingerprint']}"
    )
