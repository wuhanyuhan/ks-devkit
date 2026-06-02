"""从 Python 函数签名自动生成 JSON Schema。

用于 MCP tools/list 响应的 inputSchema 字段。开发者只需用标准 Python type
annotation 就能让客户端获得参数类型信息，无需手写 JSON Schema。

支持的类型映射：
    str   -> "string"
    int   -> "integer"
    float -> "number"
    bool  -> "boolean"
    list  -> "array"          (无元素类型时输出 {"type": "array"})
    list[T] / List[T]         -> {"type": "array", "items": <T 的 schema>}
    dict  -> "object"
    dict[str, V]              -> {"type": "object"}（不展开 V，避免无界递归）
    Optional[T] / T | None    -> 等价于 T 的 schema，但参数会从 required 移除
    Literal["a", "b", ...]    -> {"type": <推导自字面量类型>, "enum": [...]}

无 type hint 的参数会出现在 properties 中但不带 "type" 字段；
带默认值的参数自动放到 properties.<name>.default 并从 required 列表移除。

需要更复杂的 schema（如丰富的 description / 多层嵌套对象）请直接用
``App.tool(name, description, input_schema={...})`` 显式传入完整 JSON Schema，
此时 SDK 会原样透传，不再走自动推导。
"""

import inspect
import types
import typing
from typing import Any, Callable, get_type_hints


# Python 内置类型 -> JSON Schema type 字符串
_TYPE_MAP: dict[type, str] = {
    str: "string",
    int: "integer",
    float: "number",
    bool: "boolean",
    list: "array",
    dict: "object",
}

# 兼容 typing.Union（Python 3.9 用 typing.Union，3.10+ 还会出现 types.UnionType
# 即 PEP 604 的 `int | None`）。用元组在 isinstance 风格判断里同时覆盖两种。
_UNION_ORIGINS: tuple = (typing.Union,)
if hasattr(types, "UnionType"):  # Python 3.10+
    _UNION_ORIGINS = _UNION_ORIGINS + (types.UnionType,)


def _annotation_to_schema(annotation: Any) -> dict[str, Any]:
    """把单个 Python 注解转成 JSON Schema property 描述。

    返回的 dict 不含 "default"，由调用方按 inspect 的默认值填。
    无法识别的注解返回空 dict（保留 "无 type"）。
    """
    if annotation is inspect.Parameter.empty:
        return {}

    # 直接命中基础类型映射
    if annotation in _TYPE_MAP:
        return {"type": _TYPE_MAP[annotation]}

    origin = typing.get_origin(annotation)
    args = typing.get_args(annotation)

    # Optional[T] / Union[T, None] / T | None：剥掉 None 后递归
    if origin in _UNION_ORIGINS:
        non_none = tuple(a for a in args if a is not type(None))
        # 非 Optional 的 Union（如 int | str）不展开成 oneOf，按 best-effort
        # 兜底为无 type；调用方需要 Union 多分支 schema 时应改用显式 input_schema。
        if len(non_none) == 1:
            return _annotation_to_schema(non_none[0])
        return {}

    # Literal["a", "b"] -> enum
    if origin is typing.Literal:
        prop: dict[str, Any] = {"enum": list(args)}
        # 推导值类型：所有字面量同类型时填 type 字段，混合类型则只给 enum
        value_types = {type(v) for v in args}
        if len(value_types) == 1:
            single = next(iter(value_types))
            if single in _TYPE_MAP:
                prop["type"] = _TYPE_MAP[single]
        return prop

    # list[T] / List[T] -> array + items
    if origin is list:
        prop = {"type": "array"}
        if args:
            prop["items"] = _annotation_to_schema(args[0])
        return prop

    # dict / dict[K, V] -> object（不展开 value，避免任意嵌套递归）
    if origin is dict:
        return {"type": "object"}

    # 自定义类 / forward ref / typing 高阶构造：返回空 dict，等价于"出现但不带 type"
    return {}


def schema_from_func(func: Callable) -> dict[str, Any]:
    """根据函数签名生成 JSON Schema 对象。

    返回的 dict 形如：
        {
            "type": "object",
            "properties": {
                "name": {"type": "string", "default": "world"},
                "age":  {"type": "integer"},
            },
            "required": ["age"],
        }

    若函数无 required 字段，"required" 键会被整体省略，避免出现
    "required": [] 这种冗余形态。

    Optional[T] / T | None 注解的参数会按 T 推导 schema，**且无论是否带默认值
    都不会进入 required 列表**——因为 None 是合法值，要求该参数等价于"必传非
    None 值"，与注解语义矛盾。
    """
    sig = inspect.signature(func)
    try:
        hints = get_type_hints(func)
    except Exception:
        # 某些 forward reference / 字符串 annotation 在 get_type_hints 阶段
        # 可能抛错；此时退回到不含类型信息的 schema，保证 SDK 不会因为
        # 用户的特殊注解风格而崩溃。
        hints = {}

    properties: dict[str, dict[str, Any]] = {}
    required: list[str] = []

    for param_name, param in sig.parameters.items():
        # 防御性跳过 self / cls，理论上 tool handler 是顶层 async 函数不会有
        if param_name in ("self", "cls"):
            continue
        # *args / **kwargs 无法表达成 JSON Schema 字段，跳过
        if param.kind in (inspect.Parameter.VAR_POSITIONAL, inspect.Parameter.VAR_KEYWORD):
            continue

        annotation = hints.get(param_name, param.annotation)
        prop = _annotation_to_schema(annotation)

        # Optional[T] / T | None：注解层面已允许 None，不应进 required
        is_optional = False
        origin = typing.get_origin(annotation)
        if origin in _UNION_ORIGINS:
            args = typing.get_args(annotation)
            if any(a is type(None) for a in args):
                is_optional = True

        if param.default is not inspect.Parameter.empty:
            prop["default"] = param.default
        elif not is_optional:
            required.append(param_name)

        properties[param_name] = prop

    schema: dict[str, Any] = {
        "type": "object",
        "properties": properties,
    }
    if required:
        schema["required"] = required
    return schema
