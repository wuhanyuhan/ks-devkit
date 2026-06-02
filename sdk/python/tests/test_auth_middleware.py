"""JWKSAuthMiddleware 集成测试：覆盖 401 拒绝、裸放行、claims 注入。"""
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

from ks_app.auth.context import get_claims
from ks_app.auth.jwks_verifier import JWKSVerifier
from ks_app.auth.middleware import JWKSAuthMiddleware


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


def _make_app(verifier, protected_path: str = "/mcp") -> Starlette:
    async def mcp_endpoint(request):
        claims = get_claims()
        return JSONResponse({"sub": claims.get("sub") if claims else None})

    async def public_endpoint(request):
        return JSONResponse({"public": True})

    return Starlette(
        routes=[
            Route("/mcp", mcp_endpoint, methods=["POST"]),
            Route("/public", public_endpoint, methods=["GET"]),
        ],
        middleware=[
            Middleware(
                JWKSAuthMiddleware,
                verifier=verifier,
                protected_path=protected_path,
            ),
        ],
    )


def test_middleware_rejects_missing_authorization():
    v = JWKSVerifier("http://unused")
    app = _make_app(v)
    client = TestClient(app)
    r = client.post("/mcp")
    assert r.status_code == 401
    assert "error" in r.json()


def test_middleware_rejects_malformed_bearer():
    v = JWKSVerifier("http://unused")
    app = _make_app(v)
    client = TestClient(app)
    r = client.post("/mcp", headers={"Authorization": "Basic xxx"})
    assert r.status_code == 401


def test_middleware_allows_public_routes_without_auth():
    v = JWKSVerifier("http://unused")
    app = _make_app(v)
    client = TestClient(app)
    r = client.get("/public")
    assert r.status_code == 200
    assert r.json() == {"public": True}


def test_middleware_allows_valid_token_and_injects_claims():
    priv_pem, pub_jwk = _rsa_keypair()
    srv, url = _start_jwks_server({"keys": [pub_jwk]})
    try:
        v = JWKSVerifier(url)
        app = _make_app(v)
        client = TestClient(app)
        token = pyjwt.encode({"sub": "alice"}, priv_pem, algorithm="RS256", headers={"kid": "k1"})
        r = client.post("/mcp", headers={"Authorization": f"Bearer {token}"})
        assert r.status_code == 200
        assert r.json() == {"sub": "alice"}
    finally:
        srv.shutdown()


def test_middleware_allows_config_ui_token_when_server_id_matches(monkeypatch):
    monkeypatch.setenv("KSAPP_SERVER_ID", "42")
    priv_pem, pub_jwk = _rsa_keypair()
    srv, url = _start_jwks_server({"keys": [pub_jwk]})
    try:
        v = JWKSVerifier(url)
        app = _make_app(v)
        client = TestClient(app)
        token = pyjwt.encode(
            {"sub": "alice", "type": "mcp_config_ui", "mcp_server_id": 42.0},
            priv_pem,
            algorithm="RS256",
            headers={"kid": "k1"},
        )
        r = client.post("/mcp", headers={"Authorization": f"Bearer {token}"})
        assert r.status_code == 200
    finally:
        srv.shutdown()


def test_middleware_rejects_config_ui_token_when_server_id_mismatches(monkeypatch):
    monkeypatch.setenv("KSAPP_SERVER_ID", "42")
    priv_pem, pub_jwk = _rsa_keypair()
    srv, url = _start_jwks_server({"keys": [pub_jwk]})
    try:
        v = JWKSVerifier(url)
        app = _make_app(v)
        client = TestClient(app)
        token = pyjwt.encode(
            {"sub": "alice", "type": "mcp_config_ui", "mcp_server_id": 999},
            priv_pem,
            algorithm="RS256",
            headers={"kid": "k1"},
        )
        r = client.post("/mcp", headers={"Authorization": f"Bearer {token}"})
        assert r.status_code == 403
        assert r.json() == {"error": "mcp_server_id 不匹配"}
    finally:
        srv.shutdown()


def test_middleware_rejects_invalid_signature():
    _, pub_jwk = _rsa_keypair()
    srv, url = _start_jwks_server({"keys": [pub_jwk]})
    try:
        v = JWKSVerifier(url)
        app = _make_app(v)
        client = TestClient(app)
        other_priv, _ = _rsa_keypair()
        token = pyjwt.encode({"sub": "bob"}, other_priv, algorithm="RS256", headers={"kid": "k1"})
        r = client.post("/mcp", headers={"Authorization": f"Bearer {token}"})
        assert r.status_code == 401
    finally:
        srv.shutdown()
