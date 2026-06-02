"""Starlette middleware 保护 MCP 配置 UI 专用端点（/config-* 与代理端点）。

镜像 Go SDK auth/config_ui_middleware.go：
  - 校验 Bearer token 的 RS256 签名（JWKSVerifier）
  - 断言 claims.type == "mcp_config_ui"
  - 断言 claims.mcp_server_id == os.environ["KSAPP_SERVER_ID"]
  - 三分支错误映射：token 问题 401 / server_id 不匹配 403 / env 未配 500

claims 注入到 request.state.config_ui_claims（与 contextvars 的 get_claims 独立，
对齐 Go 侧独立 ctxKey 语义，避免和 /mcp 上的 JWKSAuthMiddleware 混淆）。
"""
from __future__ import annotations

import os

from starlette.middleware import Middleware
from starlette.middleware.base import BaseHTTPMiddleware
from starlette.responses import JSONResponse

from .jwks_verifier import JWKSVerifier


class ConfigUIJWTMiddleware(BaseHTTPMiddleware):
    """校验 mcp_config_ui JWT + server_id 匹配的 Starlette middleware。

    保护范围由 protected_prefixes 控制（默认 ["/config-", "/ks-config/"]）：
    只对路径前缀匹配的请求强制 JWT 校验；其它请求（/meta、/healthz、/mcp 等）
    直接放过。对齐 Go JWKSAuthMiddleware 的 protected_path 设计，避免全局
    拦截破坏非配置端点。

    create_app() 自动挂本 middleware 且不影响现有路由；
    protected_prefixes 既兼容 /config-ui/ 匹配默认前缀，又让
    /config-schema / /config-pubkey / /ks-config/* 被正确保护。
    """

    def __init__(
        self,
        app,
        verifier: JWKSVerifier,
        protected_prefixes: list[str] | None = None,
    ):
        super().__init__(app)
        self.verifier = verifier
        # 默认保护前缀：/config- 覆盖 /config-ui/ + /config-schema + /config-pubkey；
        # /ks-config/ 覆盖 /ks-config/save + /ks-config/validate。
        self.protected_prefixes = (
            protected_prefixes
            if protected_prefixes is not None
            else ["/config-", "/ks-config/"]
        )

    async def dispatch(self, request, call_next):
        # 不在保护前缀列表 → 直接放过（/meta、/healthz、/mcp 等）
        path = request.url.path
        if not any(path.startswith(p) for p in self.protected_prefixes):
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

        if claims.get("type") != "mcp_config_ui":
            return JSONResponse(
                {"error": "token 类型错误，期望 mcp_config_ui"},
                status_code=401,
            )

        expected_server_id = os.environ.get("KSAPP_SERVER_ID", "")
        if not expected_server_id:
            return JSONResponse(
                {"error": "KSAPP_SERVER_ID 环境变量未配置"},
                status_code=500,
            )

        # 兼容 claim 中 mcp_server_id 的 int / float / str（与 Go float64/string 对齐）
        # 注意：bool 在 Python 中是 int 的子类，必须显式排除
        raw_claim = claims.get("mcp_server_id")
        if isinstance(raw_claim, bool) or not isinstance(raw_claim, (int, float, str)):
            return JSONResponse(
                {"error": "mcp_server_id 类型不支持"},
                status_code=403,
            )
        if isinstance(raw_claim, float):
            # 对齐 Go strconv.FormatInt(int64(tid), 10)：截断到 int64
            claim_server_id = str(int(raw_claim))
        elif isinstance(raw_claim, int):
            claim_server_id = str(raw_claim)
        else:
            claim_server_id = raw_claim

        if claim_server_id != expected_server_id:
            return JSONResponse(
                {"error": "mcp_server_id 不匹配"},
                status_code=403,
            )

        # 注入 claims 到 request.state（与 /mcp 的 contextvars 独立命名，避免冲突）
        request.state.config_ui_claims = claims
        return await call_next(request)


def require_config_ui_jwt_middleware(verifier: JWKSVerifier) -> Middleware:
    """工厂函数：返回 Starlette Middleware 实例，用法参考 App.config_ui_middleware()。"""
    return Middleware(ConfigUIJWTMiddleware, verifier=verifier)
