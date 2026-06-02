"""旧版 MCP 端点（过渡兼容）。

Deprecated: 这些端点使用自定义 JSON 协议，已被标准 MCP Streamable HTTP
端点替代。保留仅用于过渡兼容，将在 Keystone 客户端全部迁移完成后移除。
"""

import logging
from typing import Any, Callable, Awaitable

from starlette.requests import Request
from starlette.responses import JSONResponse
from starlette.routing import Route

from ..context import _reset_meta, _set_meta

_logger = logging.getLogger("ks_app.mcp")


def legacy_call_route(
    tools: dict[str, dict],
    call_tool: Callable[[str, dict], Awaitable[Any]] | None = None,
) -> Route:
    """构造 POST /mcp/tools/call 旧版路由。

    call_tool 回调由上层提供。如果未提供，直接调用 tools[name]["handler"]。
    """

    async def handle(request: Request) -> JSONResponse:
        try:
            body = await request.json()
        except Exception as e:
            return JSONResponse({"error": f"invalid json: {e}"}, status_code=400)

        name = body.get("name")
        params = body.get("params", {})
        tool = tools.get(name)
        if not tool:
            return JSONResponse({"error": f"tool {name} not found"}, status_code=404)

        # 提取 _meta 并注入上下文
        meta = params.pop("_meta", None) if isinstance(params, dict) else None
        _set_meta(meta)
        try:
            if call_tool is not None:
                result = await call_tool(name, params)
            else:
                result = await tool["handler"](**params)
            return JSONResponse({"result": result})
        except Exception:
            _logger.error(
                "tool handler 执行失败",
                extra={"tool": name},
                exc_info=True,
            )
            return JSONResponse({"error": "工具执行失败"}, status_code=500)
        finally:
            _reset_meta()

    return Route("/mcp/tools/call", handle, methods=["POST"])


def legacy_list_route(tools: dict[str, dict]) -> Route:
    """构造 GET /mcp/tools/list 旧版路由。"""

    async def handle(request: Request) -> JSONResponse:
        tool_list = [
            {"name": name, "description": info["description"]}
            for name, info in tools.items()
        ]
        return JSONResponse({"tools": tool_list})

    return Route("/mcp/tools/list", handle, methods=["GET"])
