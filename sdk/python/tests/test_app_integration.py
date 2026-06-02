"""App 端到端测试：mock JWKS + 签 JWT → /mcp 通过/拒绝。"""
import base64
import json
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

import jwt as pyjwt
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from starlette.testclient import TestClient

from ks_app import App


def _rsa_keypair():
    priv = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    pub = priv.public_key()
    pub_numbers = pub.public_numbers()
    n = base64.urlsafe_b64encode(
        pub_numbers.n.to_bytes((pub_numbers.n.bit_length() + 7) // 8, "big")
    ).rstrip(b"=").decode()
    e = base64.urlsafe_b64encode(
        pub_numbers.e.to_bytes((pub_numbers.e.bit_length() + 7) // 8, "big")
    ).rstrip(b"=").decode()
    priv_pem = priv.private_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PrivateFormat.PKCS8,
        encryption_algorithm=serialization.NoEncryption(),
    )
    return priv_pem, {"kty": "RSA", "kid": "k1", "n": n, "e": e, "alg": "RS256"}


def _start_jwks_server(jwks_dict):
    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps(jwks_dict).encode())

        def log_message(self, *a, **k):
            pass

    srv = HTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv, f"http://127.0.0.1:{srv.server_port}"


def test_app_mcp_requires_jwt_when_keystone_auth(monkeypatch):
    _, pub_jwk = _rsa_keypair()
    srv, url = _start_jwks_server({"keys": [pub_jwk]})
    monkeypatch.setenv("KEYSTONE_JWKS_URL", url)
    monkeypatch.delenv("KS_APP_AUTH_MODE", raising=False)
    try:
        app = App("test-app", keystone_auth=True, version="0.3.0", manifest_path="nonexistent.yaml")

        @app.tool("hello", "打招呼")
        async def hello():
            return {"msg": "hi"}

        starlette_app = app.create_app()
        client = TestClient(starlette_app)
        r = client.post("/mcp", json={"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
        assert r.status_code == 401
    finally:
        srv.shutdown()


def test_app_mcp_accepts_valid_jwt(monkeypatch):
    priv_pem, pub_jwk = _rsa_keypair()
    srv, url = _start_jwks_server({"keys": [pub_jwk]})
    monkeypatch.setenv("KEYSTONE_JWKS_URL", url)
    monkeypatch.delenv("KS_APP_AUTH_MODE", raising=False)
    try:
        app = App("test-app", keystone_auth=True, version="0.3.0", manifest_path="nonexistent.yaml")

        @app.tool("hello", "打招呼")
        async def hello():
            return {"msg": "hi"}

        starlette_app = app.create_app()
        client = TestClient(starlette_app)
        token = pyjwt.encode({"sub": "alice"}, priv_pem, algorithm="RS256", headers={"kid": "k1"})
        r = client.post(
            "/mcp",
            headers={"Authorization": f"Bearer {token}"},
            json={"jsonrpc": "2.0", "id": 1, "method": "tools/list"},
        )
        assert r.status_code == 200
    finally:
        srv.shutdown()


def test_app_insecure_mode_skips_auth(monkeypatch):
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    app = App("test-app", keystone_auth=True, version="0.3.0", manifest_path="nonexistent.yaml")

    @app.tool("hello", "打招呼")
    async def hello():
        return {"msg": "hi"}

    starlette_app = app.create_app()
    client = TestClient(starlette_app)
    r = client.get("/healthz")
    assert r.status_code == 200
    r = client.post("/mcp", json={"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
    assert r.status_code == 200


def test_app_strict_by_default_raises_without_url(monkeypatch):
    """未配置 KEYSTONE_JWKS_URL + keystone_auth=True + 无 insecure → create_app 抛错。"""
    from ks_app.auth_resolver import AuthResolveError

    monkeypatch.delenv("KS_APP_AUTH_MODE", raising=False)
    monkeypatch.delenv("KEYSTONE_JWKS_URL", raising=False)
    app = App("test-app", keystone_auth=True, manifest_path="nonexistent.yaml")

    import pytest as _pytest
    with _pytest.raises(AuthResolveError):
        app.create_app()


def test_app_no_auth_by_default(monkeypatch):
    """不传 keystone_auth（默认 False）→ /mcp 裸放行。"""
    monkeypatch.delenv("KS_APP_AUTH_MODE", raising=False)
    monkeypatch.delenv("KEYSTONE_JWKS_URL", raising=False)
    app = App("test-app", manifest_path="nonexistent.yaml")

    @app.tool("hello", "")
    async def hello():
        return {"msg": "hi"}

    starlette_app = app.create_app()
    client = TestClient(starlette_app)
    r = client.post("/mcp", json={"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
    assert r.status_code == 200
