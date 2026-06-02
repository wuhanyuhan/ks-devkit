"""测试 ks_app.schema 模块的自动 schema 生成。

覆盖：
- 全部内置类型的映射
- required-only / default-only / mixed 三种参数组合
- 无 type hint 时不带 type
- keyword-only 参数
- *args / **kwargs 跳过
"""

from ks_app.schema import schema_from_func


def test_required_only():
    async def f(a: int, b: str):
        pass

    schema = schema_from_func(f)
    assert schema["type"] == "object"
    assert schema["properties"]["a"] == {"type": "integer"}
    assert schema["properties"]["b"] == {"type": "string"}
    assert sorted(schema["required"]) == ["a", "b"]


def test_default_only_no_required_key():
    """全部参数都有默认值时，required 键应被省略而非空列表。"""
    async def f(name: str = "world", count: int = 1):
        pass

    schema = schema_from_func(f)
    assert "required" not in schema
    assert schema["properties"]["name"] == {"type": "string", "default": "world"}
    assert schema["properties"]["count"] == {"type": "integer", "default": 1}


def test_mixed_required_and_default():
    async def f(name: str, age: int = 0):
        pass

    schema = schema_from_func(f)
    assert schema["required"] == ["name"]
    assert schema["properties"]["name"] == {"type": "string"}
    assert schema["properties"]["age"] == {"type": "integer", "default": 0}


def test_no_type_hint():
    """无类型注解的参数应出现在 properties 中但没有 type 字段。"""
    async def f(x):
        pass

    schema = schema_from_func(f)
    assert schema["properties"]["x"] == {}
    assert schema["required"] == ["x"]


def test_no_type_hint_with_default():
    async def f(x="hello"):
        pass

    schema = schema_from_func(f)
    assert schema["properties"]["x"] == {"default": "hello"}
    assert "required" not in schema


def test_full_type_map():
    """覆盖所有支持的内置类型。"""
    async def f(s: str, i: int, fl: float, b: bool, l: list, d: dict):
        pass

    schema = schema_from_func(f)
    props = schema["properties"]
    assert props["s"]["type"] == "string"
    assert props["i"]["type"] == "integer"
    assert props["fl"]["type"] == "number"
    assert props["b"]["type"] == "boolean"
    assert props["l"]["type"] == "array"
    assert props["d"]["type"] == "object"


def test_keyword_only_args():
    """位置 + keyword-only 混合应都被收集。"""
    async def f(a: str, *, b: int = 0):
        pass

    schema = schema_from_func(f)
    assert "a" in schema["properties"]
    assert "b" in schema["properties"]
    assert schema["properties"]["a"] == {"type": "string"}
    assert schema["properties"]["b"] == {"type": "integer", "default": 0}
    assert schema["required"] == ["a"]


def test_var_positional_and_keyword_skipped():
    """*args / **kwargs 不能映射到 JSON Schema 字段，应被跳过。"""
    async def f(a: str, *args, **kwargs):
        pass

    schema = schema_from_func(f)
    assert list(schema["properties"].keys()) == ["a"]


def test_no_params():
    """无参数函数返回空 properties + 无 required 键。"""
    async def f():
        pass

    schema = schema_from_func(f)
    assert schema == {"type": "object", "properties": {}}


def test_unknown_annotation_no_type():
    """非内置类型的注解（例如自定义类）不应填 type 字段，但参数本身仍要出现。"""
    class Custom:
        pass

    async def f(x: Custom):
        pass

    schema = schema_from_func(f)
    assert "x" in schema["properties"]
    assert "type" not in schema["properties"]["x"]
    assert schema["required"] == ["x"]


# ---------- 泛型 / Optional / Literal 支持 ----------

def test_list_of_str_emits_items():
    """list[str] 应输出 array + items.type=string，不能再退化成空对象。

    回归测试：在此修复前，schema_from_func 对 list[str] 输出 {} 不带任何
    type，导致下游 LLM 把 array 当字符串传，触发"按字符遍历 topics"的 bug。
    """
    async def f(topics: list[str]):
        pass

    schema = schema_from_func(f)
    assert schema["properties"]["topics"] == {
        "type": "array",
        "items": {"type": "string"},
    }
    assert schema["required"] == ["topics"]


def test_list_of_int_emits_items():
    async def f(ids: list[int]):
        pass

    schema = schema_from_func(f)
    assert schema["properties"]["ids"] == {
        "type": "array",
        "items": {"type": "integer"},
    }


def test_bare_list_no_items():
    """不带元素类型的 list 仍输出 array 但不带 items（保持向后兼容）。"""
    async def f(xs: list):
        pass

    schema = schema_from_func(f)
    assert schema["properties"]["xs"] == {"type": "array"}


def test_optional_int_strips_required():
    """Optional[int] 即使无默认值也不应进 required（None 是合法值）。"""
    from typing import Optional

    async def f(x: Optional[int]):
        pass

    schema = schema_from_func(f)
    assert schema["properties"]["x"] == {"type": "integer"}
    assert "required" not in schema


def test_pep604_union_with_none():
    """PEP 604 的 `int | None` 等价于 Optional[int]。"""
    async def f(x: int | None = None):
        pass

    schema = schema_from_func(f)
    assert schema["properties"]["x"] == {"type": "integer", "default": None}
    assert "required" not in schema


def test_optional_list_of_str():
    """Optional[list[str]] 应剥掉 None、保留 array+items 结构。"""
    from typing import Optional

    async def f(tags: Optional[list[str]] = None):
        pass

    schema = schema_from_func(f)
    assert schema["properties"]["tags"] == {
        "type": "array",
        "items": {"type": "string"},
        "default": None,
    }
    assert "required" not in schema


def test_literal_string_enum():
    """Literal["a","b"] 应输出 enum + 字符串 type。"""
    from typing import Literal

    async def f(platform: Literal["wechat", "zhihu", "toutiao"]):
        pass

    schema = schema_from_func(f)
    assert schema["properties"]["platform"]["type"] == "string"
    assert schema["properties"]["platform"]["enum"] == ["wechat", "zhihu", "toutiao"]


def test_literal_mixed_types_no_type_field():
    """字面量混合类型时只给 enum，不强行写 type（避免与值类型不一致）。"""
    from typing import Literal

    async def f(x: Literal[1, "two"]):
        pass

    schema = schema_from_func(f)
    prop = schema["properties"]["x"]
    assert prop["enum"] == [1, "two"]
    assert "type" not in prop


def test_union_multiple_non_none_falls_back_to_no_type():
    """非 Optional 的 Union（例如 int | str）目前 best-effort 输出无 type。

    需要丰富 Union schema 的服务应改用 App.tool(input_schema=...) 显式声明。
    """
    async def f(x: int | str):
        pass

    schema = schema_from_func(f)
    prop = schema["properties"]["x"]
    assert "type" not in prop
    assert "enum" not in prop


def test_dict_with_args_collapses_to_object():
    """dict[str, int] 不展开 value，输出 object 即可。"""
    async def f(payload: dict[str, int]):
        pass

    schema = schema_from_func(f)
    assert schema["properties"]["payload"] == {"type": "object"}
