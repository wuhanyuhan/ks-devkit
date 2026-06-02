"""JSON-RPC 2.0 工具函数和常量。

提供 MCP 协议层所需的 JSON-RPC 错误码和响应构造函数。
"""

from typing import Any

from starlette.responses import JSONResponse


# JSON-RPC 2.0 标准错误码
ERR_PARSE_ERROR = -32700
ERR_INVALID_REQUEST = -32600
ERR_METHOD_NOT_FOUND = -32601
ERR_INVALID_PARAMS = -32602
ERR_INTERNAL = -32603


def jsonrpc_result(req_id: Any, result: Any) -> JSONResponse:
    """构造 JSON-RPC 2.0 成功响应。"""
    return JSONResponse({"jsonrpc": "2.0", "id": req_id, "result": result})


def jsonrpc_error(req_id: Any, code: int, message: str) -> JSONResponse:
    """构造 JSON-RPC 2.0 错误响应。"""
    return JSONResponse({
        "jsonrpc": "2.0",
        "id": req_id,
        "error": {"code": code, "message": message},
    })
