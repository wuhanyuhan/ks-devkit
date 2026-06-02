"""ToolUIBinding / ToolResult 单元测试（widgets-protocol-v1）。"""
import json

import pytest

from ks_app.tool_ui import ToolResult, ToolUIBinding
from ks_app.tool_ui_widgets import (
    WidgetActionDescriptor,
    WidgetDiffReviewV1,
    WidgetDiffSegment,
)


# ---------------------------------------------------------------------------
# ToolResult
# ---------------------------------------------------------------------------


def test_tool_result_text_only():
    r = ToolResult().with_text("done")
    payload = json.loads(r.to_json())
    assert payload["content"] == [{"type": "text", "text": "done"}]
    assert "_meta" not in payload


def test_tool_result_empty_no_text_no_meta():
    """空 ToolResult.to_json() 不输出 content/_meta。"""
    r = ToolResult()
    payload = json.loads(r.to_json())
    assert payload == {}


def test_tool_result_with_ui_data():
    r = (
        ToolResult()
        .with_text("已审阅")
        .with_ui_data(
            WidgetDiffReviewV1(
                title="x",
                diff=[WidgetDiffSegment(type="context", text="a")],
                actions=[WidgetActionDescriptor(id="approve", label="批准")],
            )
        )
    )
    payload = json.loads(r.to_json())
    assert payload["content"] == [{"type": "text", "text": "已审阅"}]
    assert payload["_meta"]["keystone"]["ui"]["data"]["title"] == "x"
    assert payload["_meta"]["keystone"]["ui"]["data"]["diff"] == [
        {"type": "context", "text": "a"}
    ]


def test_tool_result_with_ui_override():
    r = (
        ToolResult()
        .with_text("详情")
        .with_ui_override("ks://widgets/list-actions@v1")
    )
    payload = json.loads(r.to_json())
    assert payload["_meta"]["ui"] == {"widget": "ks://widgets/list-actions@v1"}


def test_tool_result_with_ui_override_with_sandbox_hints():
    r = ToolResult().with_ui_override(
        "ui://x/y", sandbox_hints=["allow-downloads"]
    )
    payload = json.loads(r.to_json())
    assert payload["_meta"]["ui"] == {
        "widget": "ui://x/y",
        "sandbox_hints": ["allow-downloads"],
    }


def test_tool_result_validates():
    """ui_data.validate() 失败时 to_json 应抛 ValueError。"""
    with pytest.raises(ValueError, match="diff requires at least 1 segment"):
        ToolResult().with_ui_data(
            WidgetDiffReviewV1(title="x", diff=[], actions=[])
        ).to_json()


def test_tool_result_chain_returns_self():
    """链式 with_* 返回 self，支持流式调用。"""
    r = ToolResult()
    assert r.with_text("a") is r
    assert r.with_ui_override("ui://a/b") is r
    assert r.with_ui_data(
        WidgetDiffReviewV1(
            title="x",
            diff=[WidgetDiffSegment(type="context", text="a")],
            actions=[WidgetActionDescriptor(id="x", label="X")],
        )
    ) is r


def test_tool_result_with_ui_data_dict_passthrough():
    """没有 to_dict 的对象（裸 dict）作为 ui_data 直传。"""
    r = ToolResult().with_ui_data({"foo": "bar"})
    payload = json.loads(r.to_json())
    assert payload["_meta"]["keystone"]["ui"]["data"] == {"foo": "bar"}


def test_tool_result_text_and_override_no_data():
    r = ToolResult().with_text("hi").with_ui_override("ks://widgets/timeline@v1")
    payload = json.loads(r.to_json())
    assert payload["content"] == [{"type": "text", "text": "hi"}]
    assert payload["_meta"]["ui"]["widget"] == "ks://widgets/timeline@v1"
    assert "keystone" not in payload["_meta"]


def test_tool_result_to_dict_returns_same_structure_as_to_json():
    """to_dict 返回 dict 与 to_json 序列化后再 loads 等价（风格统一验证）。"""
    r = (
        ToolResult()
        .with_text("已审阅")
        .with_ui_override("ks://widgets/diff-review@v1", sandbox_hints=["allow-downloads"])
        .with_ui_data(
            WidgetDiffReviewV1(
                title="x",
                diff=[WidgetDiffSegment(type="context", text="a")],
                actions=[WidgetActionDescriptor(id="approve", label="批准")],
            )
        )
    )
    assert r.to_dict() == json.loads(r.to_json())


# ---------------------------------------------------------------------------
# ToolUIBinding
# ---------------------------------------------------------------------------


def test_tool_ui_binding_serializes():
    b = ToolUIBinding(widget="ks://widgets/diff-review@v1")
    assert b.to_dict() == {"widget": "ks://widgets/diff-review@v1"}


def test_tool_ui_binding_with_sandbox_hints():
    b = ToolUIBinding(widget="ui://x/y", sandbox_hints=["allow-downloads"])
    assert b.to_dict() == {
        "widget": "ui://x/y",
        "sandbox_hints": ["allow-downloads"],
    }


def test_tool_ui_binding_empty_sandbox_hints_omitted():
    """空 sandbox_hints 列表不应输出（对齐 Go json omitempty）。"""
    b = ToolUIBinding(widget="ks://widgets/timeline@v1", sandbox_hints=[])
    assert b.to_dict() == {"widget": "ks://widgets/timeline@v1"}


def test_tool_ui_binding_sandbox_hints_is_copied():
    """to_dict 不应共享原 list 引用，避免下游 mutate 污染 binding。"""
    hints = ["allow-downloads"]
    b = ToolUIBinding(widget="ui://x/y", sandbox_hints=hints)
    out = b.to_dict()
    out["sandbox_hints"].append("allow-popups")
    assert b.sandbox_hints == ["allow-downloads"]


# ---------------------------------------------------------------------------
# __init__.py 公共 API
# ---------------------------------------------------------------------------


def test_public_api_reexports():
    """ToolUIBinding / ToolResult / 5 widget data + 子类型可从 ks_app 顶层 import。"""
    from ks_app import (
        ToolResult as TopToolResult,
        ToolUIBinding as TopToolUIBinding,
        WidgetActionDescriptor as TopWidgetActionDescriptor,
        WidgetBadge as TopWidgetBadge,
        WidgetCard as TopWidgetCard,
        WidgetCardGridV1 as TopWidgetCardGridV1,
        WidgetDiffAnnotation as TopWidgetDiffAnnotation,
        WidgetDiffReviewV1 as TopWidgetDiffReviewV1,
        WidgetDiffSegment as TopWidgetDiffSegment,
        WidgetEmptyState as TopWidgetEmptyState,
        WidgetImageItem as TopWidgetImageItem,
        WidgetImageVariantsV1 as TopWidgetImageVariantsV1,
        WidgetListActionsV1 as TopWidgetListActionsV1,
        WidgetListItem as TopWidgetListItem,
        WidgetTimelineNode as TopWidgetTimelineNode,
        WidgetTimelineV1 as TopWidgetTimelineV1,
    )

    # 顶层 import 与子模块 import 是同一对象（避免 alias 不一致）
    from ks_app.tool_ui import ToolResult, ToolUIBinding
    from ks_app.tool_ui_widgets import (
        WidgetActionDescriptor,
        WidgetDiffReviewV1,
    )

    assert TopToolResult is ToolResult
    assert TopToolUIBinding is ToolUIBinding
    assert TopWidgetActionDescriptor is WidgetActionDescriptor
    assert TopWidgetDiffReviewV1 is WidgetDiffReviewV1
    # 触达其它 import 别名以避免 unused warning
    assert TopWidgetBadge.__name__ == "WidgetBadge"
    assert TopWidgetCard.__name__ == "WidgetCard"
    assert TopWidgetCardGridV1.__name__ == "WidgetCardGridV1"
    assert TopWidgetDiffAnnotation.__name__ == "WidgetDiffAnnotation"
    assert TopWidgetDiffSegment.__name__ == "WidgetDiffSegment"
    assert TopWidgetEmptyState.__name__ == "WidgetEmptyState"
    assert TopWidgetImageItem.__name__ == "WidgetImageItem"
    assert TopWidgetImageVariantsV1.__name__ == "WidgetImageVariantsV1"
    assert TopWidgetListActionsV1.__name__ == "WidgetListActionsV1"
    assert TopWidgetListItem.__name__ == "WidgetListItem"
    assert TopWidgetTimelineNode.__name__ == "WidgetTimelineNode"
    assert TopWidgetTimelineV1.__name__ == "WidgetTimelineV1"
