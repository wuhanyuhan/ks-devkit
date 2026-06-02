"""Starlette ScopedJWTMiddleware：保护 http_endpoint backend 路径。

挂在 capability HTTP route 之前，处理 Bearer token → 校验 → 把 ScopedClaims
注入 ``request.state.scoped_claims``，handler 适配器从这里读出来构 CapabilityContext。
"""
from __future__ import annotations

from starlette.requests import Request
from starlette.responses import JSONResponse
from starlette.types import ASGIApp, Receive, Scope, Send

from ..errors import TokenAudienceMismatch, TokenExpired, TokenInvalid
from ..scoped_jwt import ScopedJWTVerifier


class ScopedJWTMiddleware:
    """Per-route middleware：从 Authorization Bearer 取 token，验签 + aud 校验。

    ``path_to_canonical_name`` 是 path → canonical_name 反查表；一个实例可保护
    多个 capability path（共用 verifier）。
    """

    def __init__(
        self,
        app: ASGIApp,
        *,
        verifier: ScopedJWTVerifier,
        path_to_canonical_name: dict[str, str],
    ):
        self.app = app
        self.verifier = verifier
        self.path_to_canonical_name = path_to_canonical_name

    async def __call__(self, scope: Scope, receive: Receive, send: Send) -> None:
        if scope["type"] != "http":
            await self.app(scope, receive, send)
            return
        path = scope.get("path", "")
        expected_aud = self.path_to_canonical_name.get(path)
        if expected_aud is None:
            await self.app(scope, receive, send)
            return

        request = Request(scope, receive)
        auth_header = request.headers.get("authorization", "")
        if not auth_header.lower().startswith("bearer "):
            await JSONResponse(
                {"error": "missing_bearer", "code": 401}, status_code=401,
            )(scope, receive, send)
            return
        token = auth_header[7:].strip()

        try:
            claims = self.verifier.verify(token, expected_aud=expected_aud)
        except TokenAudienceMismatch as e:
            await JSONResponse(
                {"error": "aud_mismatch", "message": str(e), "code": 401},
                status_code=401,
            )(scope, receive, send)
            return
        except TokenExpired as e:
            await JSONResponse(
                {"error": "token_expired", "message": str(e), "code": 401},
                status_code=401,
            )(scope, receive, send)
            return
        except TokenInvalid as e:
            await JSONResponse(
                {"error": "token_invalid", "message": str(e), "code": 401},
                status_code=401,
            )(scope, receive, send)
            return

        scope.setdefault("state", {})
        scope["state"]["scoped_claims"] = claims
        await self.app(scope, receive, send)
