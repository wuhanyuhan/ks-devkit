"""ScopedJWTVerifier 单元测试。"""
import time

import jwt as pyjwt
import pytest
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa

from ks_app.errors import TokenAudienceMismatch, TokenExpired, TokenInvalid
from ks_app.scoped_jwt import ScopedClaims, ScopedJWTVerifier


@pytest.fixture(scope="module")
def rsa_keypair():
    key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    priv_pem = key.private_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PrivateFormat.PKCS8,
        encryption_algorithm=serialization.NoEncryption(),
    )
    pub_pem = key.public_key().public_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PublicFormat.SubjectPublicKeyInfo,
    )
    return priv_pem.decode(), pub_pem.decode()


def _sign(priv_pem: str, claims: dict, *, kid="test-kid") -> str:
    return pyjwt.encode(claims, priv_pem, algorithm="RS256", headers={"kid": kid})


def _make_verifier(pub_pem: str) -> ScopedJWTVerifier:
    """绕开 JWKS 拉取，直接注入 PEM。"""
    v = ScopedJWTVerifier(jwks_url="")
    v._static_keys = {"test-kid": pub_pem}
    return v


def test_valid_scoped_jwt_returns_claims(rsa_keypair):
    priv, pub = rsa_keypair
    now = int(time.time())
    token = _sign(priv, {
        "iss": "keystone",
        "aud": "ks-mcp-x.foo",
        "sub": "user-100",
        "iat": now, "exp": now + 60,
        "kx_caller_id": "ks-mcp-writer",
        "kx_caller_kind": "app",
        "kx_chain_id": "chain-1",
        "kx_request_id": "req-1",
    })
    v = _make_verifier(pub)
    claims = v.verify(token, expected_aud="ks-mcp-x.foo")
    assert isinstance(claims, ScopedClaims)
    assert claims.user_id == "user-100"
    assert claims.caller_id == "ks-mcp-writer"
    assert claims.caller_kind == "app"
    assert claims.chain_id == "chain-1"
    assert claims.canonical_name == "ks-mcp-x.foo"


def test_wrong_aud_raises_audience_mismatch(rsa_keypair):
    priv, pub = rsa_keypair
    now = int(time.time())
    token = _sign(priv, {
        "iss": "keystone", "aud": "ks-mcp-x.OTHER",
        "sub": "u", "iat": now, "exp": now + 60,
    })
    v = _make_verifier(pub)
    with pytest.raises(TokenAudienceMismatch):
        v.verify(token, expected_aud="ks-mcp-x.foo")


def test_expired_token_raises_token_expired(rsa_keypair):
    priv, pub = rsa_keypair
    now = int(time.time())
    token = _sign(priv, {
        "iss": "keystone", "aud": "ks-mcp-x.foo",
        "sub": "u", "iat": now - 120, "exp": now - 60,
    })
    v = _make_verifier(pub)
    with pytest.raises(TokenExpired):
        v.verify(token, expected_aud="ks-mcp-x.foo")


def test_invalid_signature_raises_token_invalid(rsa_keypair):
    priv, pub = rsa_keypair
    other_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    other_pub = other_key.public_key().public_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PublicFormat.SubjectPublicKeyInfo,
    ).decode()

    now = int(time.time())
    token = _sign(priv, {
        "iss": "keystone", "aud": "ks-mcp-x.foo",
        "sub": "u", "iat": now, "exp": now + 60,
    })
    v = _make_verifier(other_pub)
    with pytest.raises(TokenInvalid):
        v.verify(token, expected_aud="ks-mcp-x.foo")


def test_malformed_token_raises_token_invalid(rsa_keypair):
    _, pub = rsa_keypair
    v = _make_verifier(pub)
    with pytest.raises(TokenInvalid):
        v.verify("not.a.jwt", expected_aud="x")


def test_missing_required_claim_raises_token_invalid(rsa_keypair):
    """缺 exp / iat / aud / sub 之一 → TokenInvalid。"""
    priv, pub = rsa_keypair
    now = int(time.time())
    # 缺 sub
    token = _sign(priv, {
        "iss": "keystone", "aud": "ks-mcp-x.foo",
        "iat": now, "exp": now + 60,
    })
    v = _make_verifier(pub)
    with pytest.raises(TokenInvalid):
        v.verify(token, expected_aud="ks-mcp-x.foo")


def test_unknown_kid_raises_token_invalid(rsa_keypair):
    priv, pub = rsa_keypair
    now = int(time.time())
    token = pyjwt.encode(
        {"iss": "keystone", "aud": "x", "sub": "u", "iat": now, "exp": now + 60},
        priv, algorithm="RS256", headers={"kid": "unknown-kid"},
    )
    v = _make_verifier(pub)
    with pytest.raises(TokenInvalid):
        v.verify(token, expected_aud="x")
