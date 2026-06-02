"""show_when DSL parser + JSON Schema if/then/else 编译器测试。

镜像 Go `show_when_test.go` + `show_when_vectors_test.go`。

- 基础用例：10+ 条，覆盖 cmp / in / and / or / 3 种拒绝路径 / 字面量类型 / ui_hidden_when shape
- Golden vectors：加载 conformance/config-schema/testvectors.json 的 show_when 段
  用 json.dumps(sort_keys=True) 做字节级对比（对齐 Go marshal 对比策略）
"""
from __future__ import annotations

import json
import os
from typing import Any

import pytest

from ks_app.ksconfig import compile_show_when


# ============================================================================
# 基础用例（10+ 条）
# ============================================================================


def test_simple_equality():
    """backend == 'github' → if.properties.backend.const == 'github'。"""
    ifte, ui = compile_show_when("backend == 'github'", "github_token")
    assert ifte["if"] == {"properties": {"backend": {"const": "github"}}}
    assert ifte["then"] == {"required": ["github_token"]}
    assert ifte["else"] == {"properties": {"github_token": False}}
    # ui_hidden_when cmp shape = {field, op, value, negate: False}
    assert ui == {"field": "backend", "op": "==", "value": "github", "negate": False}


def test_inequality():
    """type != 'github' → if.properties.type.not.const == 'github'。"""
    ifte, _ = compile_show_when("type != 'github'", "base_url")
    assert ifte["if"] == {
        "properties": {"type": {"not": {"const": "github"}}}
    }
    assert ifte["then"] == {"required": ["base_url"]}
    assert ifte["else"] == {"properties": {"base_url": False}}


def test_in_list():
    """region in ['cn','us','eu'] → if.properties.region.enum == [...]"""
    ifte, ui = compile_show_when("region in ['cn','us','eu']", "icp_number")
    assert ifte["if"] == {
        "properties": {"region": {"enum": ["cn", "us", "eu"]}}
    }
    # ui in shape
    assert ui == {
        "field": "region",
        "op": "in",
        "values": ["cn", "us", "eu"],
        "negate": False,
    }


def test_and():
    """backend == 'github' && enabled == true → allOf 两子句。"""
    ifte, _ = compile_show_when(
        "backend == 'github' && enabled == true", "github_token"
    )
    if_node = ifte["if"]
    assert "allOf" in if_node
    all_of = if_node["allOf"]
    assert len(all_of) == 2
    assert all_of[0] == {"properties": {"backend": {"const": "github"}}}
    assert all_of[1] == {"properties": {"enabled": {"const": True}}}


def test_or():
    """region == 'cn' || region == 'us' → anyOf 两子句。"""
    ifte, _ = compile_show_when("region == 'cn' || region == 'us'", "locale")
    if_node = ifte["if"]
    assert "anyOf" in if_node
    any_of = if_node["anyOf"]
    assert len(any_of) == 2
    assert any_of[0] == {"properties": {"region": {"const": "cn"}}}
    assert any_of[1] == {"properties": {"region": {"const": "us"}}}


def test_and_or_precedence():
    """&& 优先级高于 ||：a == 'x' && b == 'y' || c == 'z' → (a&&b)||c。"""
    ifte, _ = compile_show_when(
        "a == 'x' && b == 'y' || c == 'z'", "target_field"
    )
    if_node = ifte["if"]
    assert "anyOf" in if_node
    any_of = if_node["anyOf"]
    assert len(any_of) == 2
    # 第一项是 allOf
    assert "allOf" in any_of[0]
    assert any_of[0]["allOf"] == [
        {"properties": {"a": {"const": "x"}}},
        {"properties": {"b": {"const": "y"}}},
    ]
    # 第二项是单 cmp
    assert any_of[1] == {"properties": {"c": {"const": "z"}}}


def test_reject_parenthesis():
    """(a || b) && c → SyntaxError（对齐 Go panic 的 programmer error 性质）。"""
    with pytest.raises(SyntaxError) as exc_info:
        compile_show_when("(a || b) && c", "field")
    assert "spec-v1 §3.3" in str(exc_info.value)


def test_reject_cross_level():
    """parent.field == 'x' → ValueError（跨 level）。"""
    with pytest.raises(ValueError) as exc_info:
        compile_show_when("parent.field == 'x'", "child")
    assert "跨 level" in str(exc_info.value)


def test_reject_arithmetic():
    """x + 1 == 2 → ValueError（算术运算）。"""
    with pytest.raises(ValueError) as exc_info:
        compile_show_when("x + 1 == 2", "field")
    assert "算术" in str(exc_info.value)


def test_reject_rhs_arithmetic():
    """a == 1 + 2 → ValueError（RHS 算术，从尾部字符检测）。"""
    with pytest.raises(ValueError) as exc_info:
        compile_show_when("a == 1 + 2", "field")
    assert "算术" in str(exc_info.value)


def test_number_literal():
    """max_retries == 3 → const: 3（Python int 原生）。"""
    ifte, _ = compile_show_when("max_retries == 3", "retry_delay_ms")
    if_node = ifte["if"]
    const_val = if_node["properties"]["max_retries"]["const"]
    assert const_val == 3
    assert isinstance(const_val, int) and not isinstance(const_val, bool)


def test_boolean_literal():
    """enabled == true → const: True。"""
    ifte, _ = compile_show_when("enabled == true", "api_key")
    assert ifte["if"]["properties"]["enabled"]["const"] is True


def test_null_literal():
    """proxy == null → const: None。"""
    ifte, _ = compile_show_when("proxy == null", "direct_url")
    prop = ifte["if"]["properties"]["proxy"]
    assert "const" in prop
    assert prop["const"] is None


def test_unterminated_string():
    """field == 'unclosed → ValueError（未闭合字符串）。"""
    with pytest.raises(ValueError) as exc_info:
        compile_show_when("field == 'unclosed", "target")
    assert "未闭合" in str(exc_info.value)


def test_empty_in_list():
    """region in [] → ValueError（in 列表不可为空）。"""
    with pytest.raises(ValueError) as exc_info:
        compile_show_when("region in []", "locale")
    assert "不可为空" in str(exc_info.value)


def test_in_keyword_word_boundary():
    """inbox 字段以 'in' 开头，不应被误识别为 in 操作符。"""
    # 这里会走到 cmp == 分支；只要不报错就算通过
    ifte, _ = compile_show_when("inbox == 'something'", "target")
    assert ifte["if"]["properties"]["inbox"]["const"] == "something"


def test_true_prefix_identifier_as_literal_rejected():
    """trueFlag 作为 literal 应报错（不是合法字面量）。"""
    with pytest.raises(ValueError):
        compile_show_when("enabled == trueFlag", "target")


def test_null_prefix_identifier_as_literal_rejected():
    """nullFoo 作为 literal 应报错。"""
    with pytest.raises(ValueError):
        compile_show_when("x == nullFoo", "target")


def test_ui_hidden_when_and_shape():
    """&& → ui.logical = {kind: 'and', left, right}，嵌套 cmp 也带 negate。"""
    _, ui = compile_show_when("a == 'x' && b == 'y'", "target")
    assert "logical" in ui
    assert ui["logical"]["kind"] == "and"
    left = ui["logical"]["left"]
    right = ui["logical"]["right"]
    assert left == {"field": "a", "op": "==", "value": "x", "negate": False}
    assert right == {"field": "b", "op": "==", "value": "y", "negate": False}


def test_ui_hidden_when_or_shape():
    """|| → ui.logical = {kind: 'or', ...}。"""
    _, ui = compile_show_when("a == 'x' || b == 'y'", "target")
    assert "logical" in ui
    assert ui["logical"]["kind"] == "or"


def test_negative_number():
    """max_retries == -1 → const: -1（前导负号支持）。"""
    ifte, _ = compile_show_when("max_retries == -1", "field")
    assert ifte["if"]["properties"]["max_retries"]["const"] == -1


# ============================================================================
# Golden vectors：加载 conformance/config-schema/testvectors.json show_when 段
# 对齐 Go show_when_vectors_test.go 的整 dict 字节级比对
# ============================================================================


def _load_show_when_vectors() -> list[tuple[str, dict[str, Any]]]:
    """从 testdata/testvectors.json 加载 show_when 段，返回 (name, vector) 列表。"""
    path = os.path.join(os.path.dirname(__file__), "testdata", "testvectors.json")
    if not os.path.exists(path):
        return []
    with open(path, encoding="utf-8") as f:
        data = json.load(f)
    return [(v["name"], v) for v in data.get("show_when", [])]


_VECTORS = _load_show_when_vectors()


@pytest.mark.skipif(not _VECTORS, reason="testvectors.json 缺失或 show_when 段为空")
@pytest.mark.parametrize(
    "vector_name,vector",
    _VECTORS,
    ids=[name for name, _ in _VECTORS],
)
def test_vectors_golden(vector_name: str, vector: dict[str, Any]) -> None:
    """每条 golden vector：跑 compile_show_when，与 expected_json_schema 整 dict 比对。"""
    # 拒绝路径：parenthesis 走 SyntaxError；cross_level / arithmetic 走 ValueError
    if vector.get("should_reject"):
        with pytest.raises((SyntaxError, ValueError)):
            compile_show_when(vector["dsl"], "field")
        return

    # array_context：由 reflect.py 做 array wrapping，show_when 本身不产出 {type:array,...}
    context = vector.get("context", {})
    if context.get("array_context"):
        pytest.skip("array_context 由 reflect.py 包装，show_when 不直接产出")
        return

    field_under_if = context["field_under_if"]
    got_ifte, _ = compile_show_when(vector["dsl"], field_under_if)
    expected = vector["expected_json_schema"]

    # 用 JSON 字节比（sort_keys=True 消除 dict 顺序差异；避免 int/float 类型差异）
    got_bytes = json.dumps(got_ifte, sort_keys=True)
    want_bytes = json.dumps(expected, sort_keys=True)
    assert got_bytes == want_bytes, (
        f"vector {vector_name} mismatch\n"
        f"dsl:  {vector['dsl']}\n"
        f"got:  {got_bytes}\n"
        f"want: {want_bytes}"
    )
