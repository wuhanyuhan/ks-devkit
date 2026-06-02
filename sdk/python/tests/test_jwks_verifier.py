"""JWKSVerifier 单元测试：启本地 HTTP JWKS 端点 → 签 JWT → 验证。"""
import base64
import json
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

import jwt as pyjwt
import pytest
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa

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
    return priv_pem, n, e


def _start_jwks_server(jwks: dict):
    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps(jwks).encode())

        def log_message(self, *a, **k):
            pass

    srv = HTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=srv.serve_forever, daemon=True)
    thread.start()
    return srv, f"http://127.0.0.1:{srv.server_port}"


def test_verify_valid_token():
    priv_pem, n, e = _rsa_keypair()
    jwks = {"keys": [{"kty": "RSA", "kid": "k1", "n": n, "e": e, "alg": "RS256"}]}
    srv, url = _start_jwks_server(jwks)
    try:
        v = JWKSVerifier(url)
        token = pyjwt.encode({"sub": "u1"}, priv_pem, algorithm="RS256", headers={"kid": "k1"})
        claims = v.verify(token)
        assert claims["sub"] == "u1"
    finally:
        srv.shutdown()


def test_verify_unknown_kid():
    _, n, e = _rsa_keypair()
    jwks = {"keys": [{"kty": "RSA", "kid": "k1", "n": n, "e": e, "alg": "RS256"}]}
    srv, url = _start_jwks_server(jwks)
    try:
        v = JWKSVerifier(url)
        priv2, _, _ = _rsa_keypair()
        token = pyjwt.encode({"sub": "u2"}, priv2, algorithm="RS256", headers={"kid": "unknown"})
        with pytest.raises(Exception):
            v.verify(token)
    finally:
        srv.shutdown()


def test_verify_empty_url_returns_error():
    v = JWKSVerifier("")
    with pytest.raises(Exception) as ei:
        v.verify("abc.def.ghi")
    assert "JWKS URL 未配置" in str(ei.value)
