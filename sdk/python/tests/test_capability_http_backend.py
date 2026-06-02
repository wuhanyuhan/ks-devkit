"""http_endpoint backend wiring + scoped JWT 拒绝 wrong-aud 集成测试。"""
import textwrap
import time
from pathlib import Path

import jwt as pyjwt
import pytest
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from starlette.testclient import TestClient

from ks_app import App


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


def _write_manifest(tmp_path: Path) -> str:
    p = tmp_path / "manifest.yaml"
    p.write_text(textwrap.dedent("""
        provides:
          capabilities:
            - name: foo
              execution_mode: sync
              timeout_ms: 30000
              backend:
                kind: http_endpoint
                path: /capabilities/foo
                method: POST
    """), encoding="utf-8")
    return str(p)


def _sign(priv: str, aud: str, *, sub="u-100", exp_delta=60) -> str:
    now = int(time.time())
    return pyjwt.encode(
        {
            "iss": "keystone", "aud": aud, "sub": sub,
            "iat": now, "exp": now + exp_delta,
            "kx_caller_id": "ks-mcp-writer", "kx_caller_kind": "app",
            "kx_chain_id": "chain-1", "kx_request_id": "req-1",
        },
        priv, algorithm="RS256", headers={"kid": "test-kid"},
    )


def _build_app(tmp_path: Path, pub: str) -> App:
    app = App("ks-mcp-x", manifest_path=_write_manifest(tmp_path))

    @app.capability("foo")
    async def foo(ctx, args):
        return {"echo": args.get("input", ""), "user_id": ctx.user_id, "caller_id": ctx.caller_id}

    app._scoped_jwt_test_keys = {"test-kid": pub}
    return app


def test_http_endpoint_valid_token_routes_to_handler(tmp_path, keypair):
    priv, pub = keypair
    app = _build_app(tmp_path, pub)
    client = TestClient(app.create_app())

    token = _sign(priv, aud="ks-mcp-x.foo")
    resp = client.post(
        "/capabilities/foo",
        headers={"Authorization": f"Bearer {token}"},
        json={"input": "hi"},
    )
    assert resp.status_code == 200
    body = resp.json()
    assert body["echo"] == "hi"
    assert body["user_id"] == "u-100"
    assert body["caller_id"] == "ks-mcp-writer"


def test_http_endpoint_wrong_aud_rejected(tmp_path, keypair):
    priv, pub = keypair
    app = _build_app(tmp_path, pub)
    client = TestClient(app.create_app())

    token = _sign(priv, aud="ks-mcp-x.OTHER")
    resp = client.post(
        "/capabilities/foo",
        headers={"Authorization": f"Bearer {token}"},
        json={"input": "hi"},
    )
    assert resp.status_code == 401
    assert resp.json()["error"] == "aud_mismatch"


def test_http_endpoint_missing_bearer_rejected(tmp_path, keypair):
    _, pub = keypair
    app = _build_app(tmp_path, pub)
    client = TestClient(app.create_app())

    resp = client.post("/capabilities/foo", json={"input": "hi"})
    assert resp.status_code == 401
    assert resp.json()["error"] == "missing_bearer"


def test_http_endpoint_expired_token_rejected(tmp_path, keypair):
    priv, pub = keypair
    app = _build_app(tmp_path, pub)
    client = TestClient(app.create_app())

    token = _sign(priv, aud="ks-mcp-x.foo", exp_delta=-60)
    resp = client.post(
        "/capabilities/foo",
        headers={"Authorization": f"Bearer {token}"},
        json={"input": "hi"},
    )
    assert resp.status_code == 401
    assert resp.json()["error"] == "token_expired"


def test_http_endpoint_missing_backend_path_rejected(tmp_path, keypair):
    """http_endpoint backend 但 manifest 没写 backend.path → 启动期失败。"""
    _, pub = keypair
    p = tmp_path / "manifest.yaml"
    p.write_text(textwrap.dedent("""
        provides:
          capabilities:
            - name: foo
              execution_mode: sync
              backend:
                kind: http_endpoint
                method: POST
    """), encoding="utf-8")
    app = App("ks-mcp-x", manifest_path=str(p))

    @app.capability("foo")
    async def foo(ctx, args):
        return {}

    with pytest.raises(ValueError, match="backend.path"):
        app.create_app()
