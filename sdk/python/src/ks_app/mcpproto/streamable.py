"""MCP Streamable HTTP 协议路由工厂。

实现 POST /mcp 端点，使用 JSON-RPC 2.0 信封。支持 initialize / tools/list /
tools/call 三个方法。

设计要点：
- 行为与 Go SDK 的 mcpproto/streamable.go 完全对齐（协议版本、错误码、错误消息格式）
- 通知（无 id 字段）严格按 JSON-RPC 2.0 S4.1 处理：返回 202 No Body，
  避免 MCP 客户端发送 notifications/initialized 时握手失败
- handler 异常通过 logging 记录完整堆栈到服务端，客户端只收到固定的
  "工具执行失败" 提示，防止泄露内部路径 / DB URI / SQL 片段
- ContextVar 在 try/finally 中清理，handler 抛错也能保证下个请求干净

与旧 mcp_handler.py 的区别：
- tools/call 通过 call_tool 回调执行，不再直接访问 tools["handler"]
- legacy 端点（/mcp/tools/call、/mcp/tools/list）拆到 legacy.py
"""

import json
import logging
from typing import Any, Callable, Awaitable

from starlette.requests import Request
from starlette.responses import JSONResponse, Response
from starlette.routing import Route

from ..context import _reset_meta, _set_meta
from ..schema import schema_from_func
from .jsonrpc import (
    ERR_INTERNAL,
    ERR_INVALID_PARAMS,
    ERR_INVALID_REQUEST,
    ERR_METHOD_NOT_FOUND,
    ERR_PARSE_ERROR,
    jsonrpc_error,
    jsonrpc_result,
)

# MCP Streamable HTTP 协议版本。
# Spec: https://spec.modelcontextprotocol.io/specification/2025-03-26/
MCP_PROTOCOL_VERSION = "2025-03-26"

_logger = logging.getLogger("ks_app.mcp")

# CallToolFunc 类型：接受 tool name 和 kwargs，返回结果
CallToolFunc = Callable[..., Awaitable[Any]]


def mcp_route(
    app_id: str,
    app_version: str,
    tools: dict[str, dict],
    call_tool: Callable[[str, dict], Awaitable[Any]] | None = None,
) -> Route:
    """构造 POST /mcp 路由。

    tools 形参的 dict 形态与 App._tools 完全一致：
        {tool_name: {"handler": async callable, "description": str}}

    call_tool 回调由上层提供，负责执行具体工具逻辑。
    如果未提供 call_tool，则直接调用 tools[name]["handler"]（向后兼容）。
    """

    async def handle(request: Request) -> Response:
        # ---- 1. 解析请求体 ----
        try:
            body = await request.json()
        except Exception:
            return jsonrpc_error(None, ERR_PARSE_ERROR, "JSON 解析失败")

        # ---- 2. 通知检测（JSON-RPC 2.0 S4.1）----
        if not isinstance(body, dict) or "id" not in body:
            return Response(status_code=202)

        req_id = body.get("id")

        # ---- 3. JSON-RPC 版本校验 ----
        if body.get("jsonrpc") != "2.0":
            return jsonrpc_error(req_id, ERR_INVALID_REQUEST, "仅支持 JSON-RPC 2.0")

        method = body.get("method", "")

        # ---- 4. 方法分派 ----
        if method == "initialize":
            return _handle_initialize(req_id, app_id, app_version)
        if method == "tools/list":
            return _handle_tools_list(req_id, tools)
        if method == "tools/call":
            return await _handle_tools_call(req_id, body, tools, call_tool)

        return jsonrpc_error(req_id, ERR_METHOD_NOT_FOUND, f"未知方法: {method}")

    return Route("/mcp", handle, methods=["POST"])


def _handle_initialize(req_id: Any, app_id: str, app_version: str) -> JSONResponse:
    return jsonrpc_result(req_id, {
        "protocolVersion": MCP_PROTOCOL_VERSION,
        "capabilities": {"tools": {}},
        "serverInfo": {
            "name": app_id,
            "version": app_version,
        },
    })


def _handle_tools_list(req_id: Any, tools: dict[str, dict]) -> JSONResponse:
    tool_defs = []
    for name, info in tools.items():
        # 优先用 App.tool(input_schema=...) 显式传入的 schema（包含 description /
        # enum / array items 等丰富信息，自动推导无法表达）；省略时回退到从
        # handler 函数签名推导，保持向后兼容。
        explicit_schema = info.get("input_schema")
        input_schema = explicit_schema if explicit_schema is not None else schema_from_func(info["handler"])
        tool_defs.append({
            "name": name,
            "description": info["description"],
            "inputSchema": input_schema,
        })
    return jsonrpc_result(req_id, {"tools": tool_defs})


async def _handle_tools_call(
    req_id: Any,
    body: dict,
    tools: dict[str, dict],
    call_tool: Callable[[str, dict], Awaitable[Any]] | None,
) -> JSONResponse:
    # 缺 params 字段单独报错
    params = body.get("params")
    if params is None:
        return jsonrpc_error(req_id, ERR_INVALID_PARAMS, "tools/call 缺少 params 字段")

    tool_name = params.get("name", "") if isinstance(params, dict) else ""
    if not tool_name:
        return jsonrpc_error(req_id, ERR_INVALID_PARAMS, "tools/call 缺少 name 参数")

    arguments, meta = _extract_arguments_and_meta(params)

    tool = tools.get(tool_name)
    if tool is None:
        return jsonrpc_error(req_id, ERR_INVALID_PARAMS, f"工具不存在: {tool_name}")

    _set_meta(meta)
    try:
        if call_tool is not None:
            result = await call_tool(tool_name, arguments)
        else:
            # 向后兼容：直接调用 handler
            result = await tool["handler"](**arguments)
    except Exception:
        _logger.error(
            "tool handler 执行失败",
            extra={"tool": tool_name, "request_id": req_id},
            exc_info=True,
        )
        return jsonrpc_error(req_id, ERR_INTERNAL, "工具执行失败")
    finally:
        _reset_meta()

    content = _result_to_content(result)
    return jsonrpc_result(req_id, {"content": content})


def _extract_arguments_and_meta(params: dict) -> tuple[dict, dict | None]:
    """提取 tools/call arguments 与 _meta。

    标准 MCP 把 _meta 放在 params._meta；Keystone Capability Mesh 经过
    debug / app invoke 路径时可能把 _meta 放进 params.arguments._meta。
    SDK 需要同时支持两种位置，并保证 _meta 不作为业务参数传给 handler。
    """
    arguments = params.get("arguments") or {}
    if not isinstance(arguments, dict):
        arguments = {}

    meta: dict = {}
    nested_meta = arguments.get("_meta")
    if isinstance(nested_meta, dict):
        meta.update(nested_meta)
        arguments = {key: value for key, value in arguments.items() if key != "_meta"}

    top_level_meta = params.get("_meta")
    if isinstance(top_level_meta, dict):
        meta.update(top_level_meta)

    return arguments, meta or None


def _result_to_content(result: Any) -> list[dict[str, str]]:
    """将 handler 返回值转成 MCP content 数组。"""
    if isinstance(result, str):
        return [{"type": "text", "text": result}]
    return [{"type": "text", "text": json.dumps(result, ensure_ascii=False)}]
