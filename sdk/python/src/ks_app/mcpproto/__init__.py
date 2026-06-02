"""MCP 协议层子包。

将 MCP Streamable HTTP 和旧版端点的实现从 ks_app.app 中解耦，
使协议层可独立测试和复用。
"""

from .legacy import legacy_call_route, legacy_list_route
from .streamable import MCP_PROTOCOL_VERSION, mcp_route

__all__ = [
    "mcp_route",
    "legacy_call_route",
    "legacy_list_route",
    "MCP_PROTOCOL_VERSION",
]
