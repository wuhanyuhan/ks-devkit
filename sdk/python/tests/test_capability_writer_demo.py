"""examples/capability_writer_demo e2e 单测：拉起 demo app + 模拟 dispatcher。"""
import importlib.util
import sys
import time
from pathlib import Path

import jwt as pyjwt
import pytest
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from starlette.testclient import TestClient

EXAMPLE_DIR = Path(__file__).parent.parent / "examples" / "capability_writer_demo"


def _load_demo_module():
    spec = importlib.util.spec_from_file_location(
        "capability_writer_demo_main", EXAMPLE_DIR / "main.py",
    )
    mod = importlib.util.module_from_spec(spec)
    sys.modules["capability_writer_demo_main"] = mod
    spec.loader.exec_module(mod)
    return mod


@pytest.fixture(scope="module")
def keypair():
    key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    priv = key.private_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PrivateFormat.PKCS8,
        encryption_algorithm=serialization.NoEncryption(),
    ).decode()
    pub = key.public_key().public_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PublicFormat.SubjectPublicKeyInfo,
    ).decode()
    return priv, pub


@pytest.fixture
def demo_client(keypair):
    _, pub = keypair
    # 每次 fresh load，避免 capability 重复注册（model 模块级 app 实例只 import 一次）
    if "capability_writer_demo_main" in sys.modules:
        del sys.modules["capability_writer_demo_main"]
    mod = _load_demo_module()
    mod.app._scoped_jwt_test_keys = {"test-kid": pub}
    starlette_app = mod.app.create_app()
    return TestClient(starlette_app), mod


def _sign(priv: str, aud: str, *, sub="user-1") -> str:
    now = int(time.time())
    return pyjwt.encode(
        {
            "iss": "keystone", "aud": aud, "sub": sub,
            "iat": now, "exp": now + 60,
            "kx_caller_id": "ks-mcp-test", "kx_caller_kind": "app",
            "kx_chain_id": "chain-1", "kx_request_id": "req-1",
        },
        priv, algorithm="RS256", headers={"kid": "test-kid"},
    )


def test_list_articles_via_mcp_tool_call(demo_client):
    client, _ = demo_client
    resp = client.post("/mcp", json={
        "jsonrpc": "2.0", "id": 1, "method": "tools/call",
        "params": {
            "name": "list_articles",
            "arguments": {"page": 2},
            "_meta": {"ks_user_id": "user-1"},
        },
    })
    assert resp.status_code == 200
    body = resp.json()
    assert "result" in body


def test_create_article_via_http_endpoint(demo_client, keypair):
    client, _ = demo_client
    priv, _ = keypair
    token = _sign(priv, aud="ks-mcp-writer-demo.create_article")
    resp = client.post(
        "/capabilities/create_article",
        headers={"Authorization": f"Bearer {token}"},
        json={"topic": "AI", "generate_cover": False},
    )
    assert resp.status_code == 200
    body = resp.json()
    assert body["topic"] == "AI"
    assert body["owner"] == "user-1"
    assert body["chain_id"] == "chain-1"


def test_create_article_wrong_aud_rejected(demo_client, keypair):
    client, _ = demo_client
    priv, _ = keypair
    token = _sign(priv, aud="ks-mcp-writer-demo.OTHER")
    resp = client.post(
        "/capabilities/create_article",
        headers={"Authorization": f"Bearer {token}"},
        json={"topic": "AI"},
    )
    assert resp.status_code == 401
    assert resp.json()["error"] == "aud_mismatch"
