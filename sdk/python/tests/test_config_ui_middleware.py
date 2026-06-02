"""ConfigUIJWTMiddleware 集成测试：镜像 Go sdk/go/ksapp/auth/config_ui_middleware_test.go。

覆盖 9 条用例：
- Missing Authorization / Wrong Scheme / Invalid Token / Wrong Type → 401
- EnvNotSet → 500
- ServerIDMismatch / ClaimTypeUnsupported → 403
- Success (float/string claim) → 200 + claims 注入 request.state.config_ui_claims
"""
import base64
import json
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

import jwt as pyjwt
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from starlette.applications import Starlette
from starlette.middleware import Middleware
from starlette.responses import JSONResponse
from starlette.routing import Route
from starlette.testclient import TestClient

from ks_app.auth.config_ui_middleware import ConfigUIJWTMiddleware
from ks_app.auth.jwks_verifier import JWKSVerifier


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


def _make_app(verifier) -> Starlette:
    async def config_endpoint(request):
        claims = getattr(request.state, "config_ui_claims", None)
        return JSONResponse(
            {
                "type": claims.get("type") if claims else None,
                "sub": claims.get("sub") if claims else None,
            }
        )

    return Starlette(
        routes=[Route("/config-ui/", config_endpoint, methods=["GET"])],
        middleware=[Middleware(ConfigUIJWTMiddleware, verifier=verifier)],
    )


def test_require_config_ui_jwt_missing_authorization(monkeypatch):
    monkeypatch.setenv("KSAPP_SERVER_ID", "42")
    _, pub_jwk = _rsa_keypair()
    srv, url = _start_jwks_server({"keys": [pub_jwk]})
    try:
        v = JWKSVerifier(url)
        app = _make_app(v)
        client = TestClient(app)
        r = client.get("/config-ui/")
        assert r.status_code == 401
        assert "error" in r.json()
    finally:
        srv.shutdown()


def test_require_config_ui_jwt_wrong_scheme(monkeypatch):
    monkeypatch.setenv("KSAPP_SERVER_ID", "42")
    _, pub_jwk = _rsa_keypair()
    srv, url = _start_jwks_server({"keys": [pub_jwk]})
    try:
        v = JWKSVerifier(url)
        app = _make_app(v)
        client = TestClient(app)
        r = client.get("/config-ui/", headers={"Authorization": "Basic dXNlcjpwYXNz"})
        assert r.status_code == 401
        assert "error" in r.json()
    finally:
        srv.shutdown()


def test_require_config_ui_jwt_invalid_token(monkeypatch):
    """签发 token 的 kid 不在 JWKS → 401"""
    monkeypatch.setenv("KSAPP_SERVER_ID", "42")
    priv_pem, pub_jwk = _rsa_keypair()
    srv, url = _start_jwks_server({"keys": [pub_jwk]})
    try:
        v = JWKSVerifier(url)
        app = _make_app(v)
        client = TestClient(app)
        token = pyjwt.encode(
            {"type": "mcp_config_ui", "mcp_server_id": "42"},
            priv_pem,
            algorithm="RS256",
            headers={"kid": "unknown-kid"},
        )
        r = client.get("/config-ui/", headers={"Authorization": f"Bearer {token}"})
        assert r.status_code == 401
        assert "error" in r.json()
    finally:
        srv.shutdown()


def test_require_config_ui_jwt_wrong_type(monkeypatch):
    monkeypatch.setenv("KSAPP_SERVER_ID", "42")
    priv_pem, pub_jwk = _rsa_keypair()
    srv, url = _start_jwks_server({"keys": [pub_jwk]})
    try:
        v = JWKSVerifier(url)
        app = _make_app(v)
        client = TestClient(app)
        token = pyjwt.encode(
            {"type": "developer", "mcp_server_id": "42"},
            priv_pem,
            algorithm="RS256",
            headers={"kid": "k1"},
        )
        r = client.get("/config-ui/", headers={"Authorization": f"Bearer {token}"})
        assert r.status_code == 401
        assert "error" in r.json()
    finally:
        srv.shutdown()


def test_require_config_ui_jwt_env_not_set(monkeypatch):
    """KSAPP_SERVER_ID 未配 → 500"""
    monkeypatch.delenv("KSAPP_SERVER_ID", raising=False)
    priv_pem, pub_jwk = _rsa_keypair()
    srv, url = _start_jwks_server({"keys": [pub_jwk]})
    try:
        v = JWKSVerifier(url)
        app = _make_app(v)
        client = TestClient(app)
        token = pyjwt.encode(
            {"type": "mcp_config_ui", "mcp_server_id": "42"},
            priv_pem,
            algorithm="RS256",
            headers={"kid": "k1"},
        )
        r = client.get("/config-ui/", headers={"Authorization": f"Bearer {token}"})
        assert r.status_code == 500
        assert "error" in r.json()
    finally:
        srv.shutdown()


def test_require_config_ui_jwt_server_id_mismatch(monkeypatch):
    """env=100 vs claim=42 → 403"""
    monkeypatch.setenv("KSAPP_SERVER_ID", "100")
    priv_pem, pub_jwk = _rsa_keypair()
    srv, url = _start_jwks_server({"keys": [pub_jwk]})
    try:
        v = JWKSVerifier(url)
        app = _make_app(v)
        client = TestClient(app)
        token = pyjwt.encode(
            {"type": "mcp_config_ui", "mcp_server_id": 42},
            priv_pem,
            algorithm="RS256",
            headers={"kid": "k1"},
        )
        r = client.get("/config-ui/", headers={"Authorization": f"Bearer {token}"})
        assert r.status_code == 403
        assert "error" in r.json()
    finally:
        srv.shutdown()


def test_require_config_ui_jwt_success_float_claim(monkeypatch):
    """mcp_server_id 为 float(42.0) → 200 + claims 注入 request.state"""
    monkeypatch.setenv("KSAPP_SERVER_ID", "42")
    priv_pem, pub_jwk = _rsa_keypair()
    srv, url = _start_jwks_server({"keys": [pub_jwk]})
    try:
        v = JWKSVerifier(url)
        app = _make_app(v)
        client = TestClient(app)
        # pyjwt.encode 序列化 float 到 JSON number，decode 回解成 float
        token = pyjwt.encode(
            {"type": "mcp_config_ui", "mcp_server_id": 42.0, "sub": "user-7"},
            priv_pem,
            algorithm="RS256",
            headers={"kid": "k1"},
        )
        r = client.get("/config-ui/", headers={"Authorization": f"Bearer {token}"})
        assert r.status_code == 200
        body = r.json()
        assert body["type"] == "mcp_config_ui"
        assert body["sub"] == "user-7"
    finally:
        srv.shutdown()


def test_require_config_ui_jwt_success_string_claim(monkeypatch):
    monkeypatch.setenv("KSAPP_SERVER_ID", "42")
    priv_pem, pub_jwk = _rsa_keypair()
    srv, url = _start_jwks_server({"keys": [pub_jwk]})
    try:
        v = JWKSVerifier(url)
        app = _make_app(v)
        client = TestClient(app)
        token = pyjwt.encode(
            {"type": "mcp_config_ui", "mcp_server_id": "42"},
            priv_pem,
            algorithm="RS256",
            headers={"kid": "k1"},
        )
        r = client.get("/config-ui/", headers={"Authorization": f"Bearer {token}"})
        assert r.status_code == 200
    finally:
        srv.shutdown()


def test_require_config_ui_jwt_claim_type_unsupported(monkeypatch):
    """mcp_server_id 为 dict（非 int/float/str）→ 403（对齐 Go default case）"""
    monkeypatch.setenv("KSAPP_SERVER_ID", "42")
    priv_pem, pub_jwk = _rsa_keypair()
    srv, url = _start_jwks_server({"keys": [pub_jwk]})
    try:
        v = JWKSVerifier(url)
        app = _make_app(v)
        client = TestClient(app)
        token = pyjwt.encode(
            {"type": "mcp_config_ui", "mcp_server_id": {"nested": "value"}},
            priv_pem,
            algorithm="RS256",
            headers={"kid": "k1"},
        )
        r = client.get("/config-ui/", headers={"Authorization": f"Bearer {token}"})
        assert r.status_code == 403
        assert "error" in r.json()
    finally:
        srv.shutdown()
