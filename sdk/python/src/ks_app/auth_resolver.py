"""三层优先级解析 effective auth mode。

对齐 Go SDK resolveAuth()：
  1. 代码 Option（keystone_auth=True → code_mode='keystone_jwks'）最高优先级
  2. manifest.yaml 的 mount.*.auth_mode 作 fallback
  3. 默认 'none'

KS_APP_AUTH_MODE=insecure 强制降级为 none（全局逃生）。

strict-by-default: effective=keystone_jwks 且 jwksURL='' 时抛 AuthResolveError。
"""
from __future__ import annotations

from typing import Optional, Tuple

from .manifest import load_manifest_auth_mode


class AuthResolveError(RuntimeError):
    """auth 解析失败（最常见：strict-by-default 触发）。"""


def resolve_auth(
    code_mode: Optional[str],
    manifest_path: str,
    env: dict,
) -> Tuple[str, str]:
    """返回 (effective_mode, jwks_url)。

    code_mode: 'keystone_jwks' / 'none' / None（None 表示未传 Option，走 fallback）
    env: 环境变量 dict；生产中传 os.environ
    """
    effective = code_mode or "none"

    if effective == "none":
        fallback = load_manifest_auth_mode(manifest_path)
        if fallback:
            effective = fallback

    jwks_url = env.get("KEYSTONE_JWKS_URL", "")

    if env.get("KS_APP_AUTH_MODE") == "insecure":
        return "none", ""

    if effective == "keystone_jwks" and not jwks_url:
        raise AuthResolveError(
            "ks_app: auth_mode=keystone_jwks 但 KEYSTONE_JWKS_URL 未配置；"
            "生产必须设置此 env，或本地开发用 KS_APP_AUTH_MODE=insecure 降级"
        )

    return effective, jwks_url
