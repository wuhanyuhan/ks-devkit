"""Cross-language wire round-trip：Python dump → Go parse → Go dump → Python parse；
对 5 widget schema 各跑一次，确保顶层字段集合在两端一致。

这是 wire 兼容的看门测试：
任何 Python ↔ Go 字段集合差异都会让本测试失败。

策略：
1. Python 用 dataclass 构造最小合法 widget data
2. ToolResult().with_ui_data(d).to_json() 序列化
3. 提取 _meta.keystone.ui.data 顶层 dict
4. 调 Go helper（subprocess `go run .`）传 {widget, data}，接收
   ksapp.NewToolResult().WithUIData(...).MarshalJSON() 输出
5. 提取 Go 端 _meta.keystone.ui.data 顶层 dict
6. 断言顶层字段集合一致（不要求 byte 等价：JSON map 顺序、嵌套字段
   集合差异等留给后续完整 round-trip；本测试聚焦 omit empty 行为差异）。

如发现失败，**不要修协议层**——应排查两端字段集合为何漂移。
"""

from __future__ import annotations

import json
import shutil
import subprocess
from pathlib import Path

import pytest

from ks_app.tool_ui import ToolResult
from ks_app.tool_ui_widgets import (
    WidgetActionDescriptor,
    WidgetCard,
    WidgetCardGridV1,
    WidgetDiffReviewV1,
    WidgetDiffSegment,
    WidgetImageItem,
    WidgetImageVariantsV1,
    WidgetListActionsV1,
    WidgetListItem,
    WidgetTimelineNode,
    WidgetTimelineV1,
)

GO_HELPER = Path(__file__).parent / "wire_compat" / "go_marshal"


def _go_marshal(widget: str, data_dict: dict) -> dict:
    """调 Go helper：传 {widget, data} → 接收 ksapp.ToolResult.MarshalJSON 输出。"""
    payload = json.dumps({"widget": widget, "data": data_dict}).encode()
    result = subprocess.run(
        ["go", "run", "."],
        cwd=str(GO_HELPER),
        input=payload,
        capture_output=True,
        check=True,
        timeout=60,
    )
    return json.loads(result.stdout)


@pytest.fixture(scope="module", autouse=True)
def _check_go_available() -> None:
    """前置：go 工具链 + helper go.mod 必须存在；缺一就 skip 整个 module。"""
    if shutil.which("go") is None:
        pytest.skip("go toolchain not in PATH; cross-language wire-compat skipped")
    if not (GO_HELPER / "go.mod").exists():
        pytest.skip(f"go helper go.mod not found at {GO_HELPER}; skipping")


@pytest.mark.parametrize(
    "widget,data_factory",
    [
        (
            "ks://widgets/diff-review@v1",
            lambda: WidgetDiffReviewV1(
                title="x",
                diff=[WidgetDiffSegment(type="context", text="a")],
                actions=[WidgetActionDescriptor(id="approve", label="批准")],
            ),
        ),
        (
            "ks://widgets/list-actions@v1",
            lambda: WidgetListActionsV1(
                items=[WidgetListItem(id="1", title="x")],
            ),
        ),
        (
            "ks://widgets/timeline@v1",
            lambda: WidgetTimelineV1(
                events=[
                    WidgetTimelineNode(
                        id="1",
                        time="2026-05-04T10:00:00Z",
                        title="x",
                        status="success",
                    )
                ],
            ),
        ),
        (
            "ks://widgets/card-grid@v1",
            lambda: WidgetCardGridV1(
                cards=[WidgetCard(id="1", title="x")],
            ),
        ),
        (
            "ks://widgets/image-variants@v1",
            lambda: WidgetImageVariantsV1(
                primary=WidgetImageItem(
                    id="p", url="https://x/y.png", alt_text="x"
                ),
            ),
        ),
    ],
    ids=[
        "diff-review-v1",
        "list-actions-v1",
        "timeline-v1",
        "card-grid-v1",
        "image-variants-v1",
    ],
)
def test_python_dump_go_parse_keys_match(widget: str, data_factory) -> None:
    """Python 序列化 → Go 反序列化 + 重新序列化 → 字段集合一致。

    不要求 byte 等价：Go map iteration 顺序可能不同，但顶层字段集合必须相同。
    Python omit empty（_omit_if_empty）与 Go json `,omitempty` 的覆盖语义在
    最小合法实例上必须等价；任何不一致都说明协议两端 drift。
    """
    data = data_factory()
    py_payload = json.loads(ToolResult().with_ui_data(data).to_json())
    py_data = py_payload["_meta"]["keystone"]["ui"]["data"]

    go_payload = _go_marshal(widget, py_data)
    go_data = go_payload["_meta"]["keystone"]["ui"]["data"]

    py_keys = set(py_data.keys())
    go_keys = set(go_data.keys())
    assert py_keys == go_keys, (
        f"field-set drift for {widget}: "
        f"py-only={py_keys - go_keys} go-only={go_keys - py_keys} "
        f"py={py_keys} go={go_keys}"
    )
