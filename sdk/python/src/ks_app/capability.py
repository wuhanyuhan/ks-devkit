"""@app.capability decorator 注册 capability handler。

Capability 与 @app.tool 是并列两个维度：tool 是 MCP 协议层面的工具，capability 是
Capability Mesh 协议层面的能力。底层在 mcp_tool backend 路径下两者会共用 MCP route
（capability 自动注册成同名 MCP tool），但 ctx 类型不同：
  - tool handler 收 (params: dict) + get_context() 取 ToolContext
  - capability handler 收 (ctx: CapabilityContext, args: dict) 显式注入
"""
from __future__ import annotations

import inspect
from dataclasses import dataclass
from typing import Awaitable, Callable


CapabilityHandler = Callable[..., Awaitable[dict]]


@dataclass
class CapabilityEntry:
    """单个 capability 注册项。运行时 wiring 用。"""
    canonical_name: str
    handler: CapabilityHandler
    name: str = ""
    backend_kind: str = ""
    backend_tool_name: str = ""
    backend_path: str = ""
    backend_method: str = ""
    execution_mode: str = ""
    timeout_ms: int = 0
    input_schema: dict | None = None


def make_capability_decorator(registry: dict[str, CapabilityEntry], app_id: str):
    """工厂函数：返回绑定到 registry + app_id 的 @capability decorator。

    去前缀：作者写裸名 name；canonical_name 由 canonical(app_id, name) 派生。
    """
    from .canonical import canonical as _canonical

    def decorator(name: str):
        def inner(handler: CapabilityHandler) -> CapabilityHandler:
            if not inspect.iscoroutinefunction(handler):
                raise TypeError(
                    f"capability {name!r} 的 handler 必须是 async 函数，"
                    f"收到同步函数 {handler.__qualname__}"
                )
            cn = _canonical(app_id, name)
            if cn in registry:
                raise ValueError(
                    f"capability {cn!r} 已经注册过了，禁止重复注册"
                )
            registry[cn] = CapabilityEntry(
                canonical_name=cn,
                name=name,
                handler=handler,
            )
            return handler
        return inner
    return decorator
