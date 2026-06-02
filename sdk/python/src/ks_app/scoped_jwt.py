"""scoped JWT 验签 + aud 校验。

dispatcher 在每次 invoke 时为下游 backend 签短期 token：
  iss=keystone, aud=<canonical_name>, sub=<user_id>, exp=iat+60s
  kx_caller_id / kx_caller_kind / kx_chain_id / kx_request_id 等 mesh 扩展 claims

http_endpoint backend 路径下，SDK 用本 verifier 校验 + 解码出 ScopedClaims；
mcp_tool backend 路径下，dispatcher 不签 scoped JWT（高信任链路），本 verifier 不参与。
"""
from __future__ import annotations

from dataclasses import dataclass
from typing import Optional

import jwt as pyjwt
from jwt import PyJWKClient

from .errors import TokenAudienceMismatch, TokenExpired, TokenInvalid


@dataclass(frozen=True)
class ScopedClaims:
    """scoped JWT 解码后的 claims 子集。"""
    user_id: str
    canonical_name: str
    caller_id: str
    caller_kind: str
    chain_id: str
    request_id: str
    issued_at: int
    expires_at: int


class ScopedJWTVerifier:
    """scoped JWT 验签器（http_endpoint backend 用）。

    keys 来自 keystone JWKS endpoint（与现有 JWKSVerifier 同源），通过 kid 选 key。
    测试时可直接注入 _static_keys（kid → PEM）跳过网络。
    """

    def __init__(self, jwks_url: str, *, timeout: float = 5.0):
        self.jwks_url = jwks_url
        self.timeout = timeout
        self._jwk_client: Optional[PyJWKClient] = None
        self._static_keys: dict[str, str] = {}

    def _resolve_key(self, token: str) -> str:
        """从 token header 取 kid → 返回 PEM 公钥。"""
        if self._static_keys:
            try:
                header = pyjwt.get_unverified_header(token)
            except Exception as e:
                raise TokenInvalid(f"token header 解析失败: {e}") from e
            kid = header.get("kid", "")
            if kid not in self._static_keys:
                raise TokenInvalid(f"未知 kid: {kid}")
            return self._static_keys[kid]
        if not self.jwks_url:
            raise TokenInvalid("ScopedJWTVerifier: jwks_url 未配置且无静态 key")
        if self._jwk_client is None:
            self._jwk_client = PyJWKClient(self.jwks_url, timeout=self.timeout)
        try:
            signing_key = self._jwk_client.get_signing_key_from_jwt(token)
            return signing_key.key
        except Exception as e:
            raise TokenInvalid(f"JWKS 取 key 失败: {e}") from e

    def verify(self, token: str, *, expected_aud: str) -> ScopedClaims:
        """验签 + aud 校验，返回 ScopedClaims。

        异常映射：
          - 解码失败 / 签名错 / 字段缺失 → TokenInvalid
          - exp 过期 → TokenExpired
          - aud 不匹配 → TokenAudienceMismatch
        """
        key = self._resolve_key(token)
        try:
            payload = pyjwt.decode(
                token, key, algorithms=["RS256", "EdDSA"],
                audience=expected_aud,
                options={"require": ["exp", "iat", "aud", "sub"]},
            )
        except pyjwt.ExpiredSignatureError as e:
            raise TokenExpired(str(e)) from e
        except pyjwt.InvalidAudienceError as e:
            raise TokenAudienceMismatch(
                f"aud 不匹配 expected={expected_aud}: {e}"
            ) from e
        except pyjwt.PyJWTError as e:
            raise TokenInvalid(str(e)) from e

        return ScopedClaims(
            user_id=str(payload.get("sub", "")),
            canonical_name=str(payload.get("aud", "")),
            caller_id=str(payload.get("kx_caller_id", "")),
            caller_kind=str(payload.get("kx_caller_kind", "")),
            chain_id=str(payload.get("kx_chain_id", "")),
            request_id=str(payload.get("kx_request_id", "")),
            issued_at=int(payload.get("iat", 0)),
            expires_at=int(payload.get("exp", 0)),
        )
