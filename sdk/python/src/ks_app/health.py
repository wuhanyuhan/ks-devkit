"""/healthz、/readyz、/meta 端点。

/meta 响应对齐 ks-types MetaResponse（v0.5.0 同格式，Python SDK v0.4.0+）：
  {name, version, auth_mode, tools, config_ui?, nav?, permissions?,
   config_mode?, protocol_version?, config_status?}
"""
import logging
from typing import Optional

from starlette.responses import JSONResponse
from starlette.routing import Route

logger = logging.getLogger(__name__)


def health_routes(
    app_id: str,
    version: str,
    auth_mode: str,
    tools: dict,
    health_checks: list | None = None,
    config_ui: Optional[dict] = None,
    *,
    nav: Optional[dict] = None,
    permissions: Optional[list[dict]] = None,
    config_mode: Optional[str] = None,
    protocol_version: Optional[str] = None,
    config_status: Optional[str] = None,
) -> list:
    """返回 /healthz、/readyz、/meta 三个路由。

    health_checks: [(name, check_fn), ...] 列表。check_fn 无异常表示健康。
    config_ui: 若非空则反射到 /meta.config_ui。
    nav / permissions / config_mode / protocol_version / config_status:
        v0.4.0 新增，对齐 ks-types v0.5.0 MetaResponse；按 omitempty 语义反射到 /meta。
    """
    checks = health_checks or []

    async def healthz(request):
        if not checks:
            return JSONResponse({"status": "ok"})
        failures = {}
        for name, check_fn in checks:
            try:
                check_fn()
            except Exception as e:
                failures[name] = str(e)
        if failures:
            return JSONResponse(
                {"status": "unhealthy", "checks": failures}, status_code=503
            )
        return JSONResponse({"status": "ok"})

    async def readyz(request):
        return JSONResponse({"status": "ok"})

    async def meta(request):
        tool_infos = []
        has_ui_binding = False
        for name, info in tools.items():
            entry = {"name": name, "description": info["description"]}
            if info.get("input_schema"):
                entry["input_schema"] = info["input_schema"]
            # v0.6.0 widgets-protocol-v1：tool 级 ui_binding 注入 _meta.ui
            ui_binding = info.get("ui_binding")
            if ui_binding is not None:
                entry["_meta"] = {"ui": ui_binding.to_dict()}
                has_ui_binding = True
            tool_infos.append(entry)
        resp = {
            "name": app_id,
            "version": version,
            "auth_mode": auth_mode,
            "tools": tool_infos,
        }
        if config_ui:
            resp["config_ui"] = config_ui
        # v0.4.0 新增 5 字段（对齐 ks-types v0.5.0），按 omitempty 语义只在非空时写入
        if nav is not None:
            resp["nav"] = nav
        if permissions:
            resp["permissions"] = permissions
        if config_mode is not None:
            resp["config_mode"] = config_mode
        if protocol_version is not None:
            resp["protocol_version"] = protocol_version
        if config_status is not None:
            resp["config_status"] = config_status
        # v0.6.0 widgets-protocol-v1：任意 tool 有 binding 即声明 capabilities.ui
        if has_ui_binding:
            resp["capabilities"] = {"ui": {"enabled": True}}
        return JSONResponse(resp)

    return [
        Route("/healthz", healthz, methods=["GET"]),
        Route("/readyz", readyz, methods=["GET"]),
        Route("/meta", meta, methods=["GET"]),
    ]
