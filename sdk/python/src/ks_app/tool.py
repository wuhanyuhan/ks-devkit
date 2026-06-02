from typing import Optional


class Param:
    """装饰器辅助类，描述单个参数。"""
    def __init__(self, description: str, *, default=None, required=False):
        self.description = description
        self.default = default
        self.required = required


def tool(name: str, description: str, input_schema: Optional[dict] = None):
    """独立装饰器（用于在 App 外部定义 tool，便于单元测试）。"""
    def decorator(func):
        func._ks_tool_name = name
        func._ks_tool_description = description
        func._ks_tool_input_schema = input_schema
        return func
    return decorator
