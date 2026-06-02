"""JWKS 端点拉取 + RS256 JWT 验签。

参照 Go SDK auth/jwks_verifier.go：缓存 TTL 1 小时，未知 kid 强制重拉。
"""
from __future__ import annotations

import base64
import json
import threading
import time
import urllib.request
from dataclasses import dataclass, field

import jwt as pyjwt
from cryptography.hazmat.backends import default_backend
from cryptography.hazmat.primitives.asymmetric.rsa import RSAPublicNumbers


@dataclass
class JWKSVerifier:
    jwks_url: str
    cache_ttl_seconds: int = 3600
    _cache: dict = field(default_factory=dict)
    _cache_time: float = 0.0
    _lock: threading.Lock = field(default_factory=threading.Lock)

    def verify(self, token_str: str) -> dict:
        """验证 JWT 字符串，返回 claims 或抛异常。"""
        if not self.jwks_url:
            raise ValueError("JWKS URL 未配置，无法验证")
        unverified_header = pyjwt.get_unverified_header(token_str)
        kid = unverified_header.get("kid")
        if not kid:
            raise ValueError("JWT header 缺少 kid")
        alg = unverified_header.get("alg")
        if alg != "RS256":
            raise ValueError(f"不支持的签名算法: {alg}")
        pub_key = self._get_key(kid)
        claims = pyjwt.decode(
            token_str,
            pub_key,
            algorithms=["RS256"],
            options={"verify_aud": False},
        )
        return claims

    def _get_key(self, kid: str):
        with self._lock:
            cache_fresh = (time.time() - self._cache_time) < self.cache_ttl_seconds
            if cache_fresh and kid in self._cache:
                return self._cache[kid]
            self._fetch()
        if kid not in self._cache:
            raise ValueError(f"未找到 kid={kid} 对应的公钥")
        return self._cache[kid]

    def _fetch(self):
        req = urllib.request.Request(self.jwks_url, headers={"Accept": "application/json"})
        with urllib.request.urlopen(req, timeout=10) as resp:
            if resp.status != 200:
                raise ValueError(f"JWKS 端点返回非 200: {resp.status}")
            data = json.loads(resp.read().decode("utf-8"))
        new_cache = {}
        for key in data.get("keys", []):
            if key.get("kty", "").upper() != "RSA":
                continue
            try:
                pub = _parse_rsa_public_key(key["n"], key["e"])
            except Exception:
                continue
            new_cache[key.get("kid", "")] = pub
        self._cache = new_cache
        self._cache_time = time.time()


def _parse_rsa_public_key(n_b64url: str, e_b64url: str):
    def _b64_to_int(s: str) -> int:
        padding = 4 - (len(s) % 4)
        if padding < 4:
            s = s + ("=" * padding)
        return int.from_bytes(base64.urlsafe_b64decode(s), "big")

    n = _b64_to_int(n_b64url)
    e = _b64_to_int(e_b64url)
    numbers = RSAPublicNumbers(e=e, n=n)
    return numbers.public_key(default_backend())
