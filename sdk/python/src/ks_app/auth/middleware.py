"""Starlette middleware 检查 /mcp 的 JWT。

仅对 protected_path 前缀做鉴权；其它路径裸放行。
对齐 Go SDK app.go "只给 /mcp handler 叠加 auth middleware" 的语义。
"""
from __future__ import annotations

import os

from starlette.middleware.base import BaseHTTPMiddleware
from starlette.responses import JSONResponse

from .context import reset_claims, set_claims
from .jwks_verifier import JWKSVerifier


class JWKSAuthMiddleware(BaseHTTPMiddleware):
    def __init__(self, app, verifier: JWKSVerifier, protected_path: str = "/mcp"):
        super().__init__(app)
        self.verifier = verifier
        self.protected_path = protected_path

    async def dispatch(self, request, call_next):
        if not request.url.path.startswith(self.protected_path):
            return await call_next(request)

        auth_hdr = request.headers.get("authorization", "")
        if not auth_hdr:
            return JSONResponse({"error": "缺少 Authorization 头"}, status_code=401)
        parts = auth_hdr.split(" ", 1)
        if len(parts) != 2 or parts[0].lower() != "bearer":
            return JSONResponse(
                {"error": "Authorization 格式错误，期望 'Bearer <jwt>'"},
                status_code=401,
            )
        try:
            claims = self.verifier.verify(parts[1])
        except Exception:
            return JSONResponse({"error": "令牌验证失败"}, status_code=401)
        config_ui_error = _validate_config_ui_server_id(claims)
        if config_ui_error is not None:
            status_code, message = config_ui_error
            return JSONResponse({"error": message}, status_code=status_code)

        token = set_claims(claims)
        try:
            return await call_next(request)
        finally:
            reset_claims(token)


def _validate_config_ui_server_id(claims: dict) -> tuple[int, str] | None:
    if claims.get("type") != "mcp_config_ui":
        return None

    expected_server_id = os.environ.get("KSAPP_SERVER_ID", "")
    if not expected_server_id:
        return 500, "KSAPP_SERVER_ID 环境变量未配置"

    raw_claim = claims.get("mcp_server_id")
    if isinstance(raw_claim, bool) or not isinstance(raw_claim, (int, float, str)):
        return 403, "mcp_server_id 类型不支持"
    if isinstance(raw_claim, float):
        claim_server_id = str(int(raw_claim))
    elif isinstance(raw_claim, int):
        claim_server_id = str(raw_claim)
    else:
        claim_server_id = raw_claim

    if claim_server_id != expected_server_id:
        return 403, "mcp_server_id 不匹配"
    return None
