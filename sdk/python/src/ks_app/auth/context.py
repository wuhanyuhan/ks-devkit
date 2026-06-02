"""用 contextvars 传递 JWT claims。

JWKSAuthMiddleware 在验证通过后把 claims 存入 contextvar；
业务层在工具 handler 中通过 get_claims() 读取。

与 ks_app.context（MCP _meta 上下文注入）是独立机制：claims 装原始 JWT payload，
_meta 装 Keystone 自定义字段（resource_scope/execution_id 等）。两者并存。
"""
from __future__ import annotations

import contextvars
from typing import Optional

_current_claims: contextvars.ContextVar[Optional[dict]] = contextvars.ContextVar(
    "ks_app_jwt_claims", default=None
)


def set_claims(claims: dict) -> contextvars.Token:
    """Middleware 内部用：设置当前请求的 claims。返回 token 供 reset。"""
    return _current_claims.set(claims)


def reset_claims(token: contextvars.Token) -> None:
    """Middleware 内部用：清理 claims。"""
    _current_claims.reset(token)


def get_claims() -> Optional[dict]:
    """业务层用：取当前请求的 JWT claims，未设置返回 None。"""
    return _current_claims.get()
