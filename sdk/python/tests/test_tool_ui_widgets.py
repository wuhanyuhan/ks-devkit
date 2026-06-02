"""5 个 MVP widget data schema 的 validate/to_dict 单元测试（widgets-protocol-v1）。

校验规则与 ks-types Go 端 Validate() 一一对齐。
"""
import pytest

from ks_app.tool_ui_widgets import (
    WidgetActionDescriptor,
    WidgetBadge,
    WidgetCard,
    WidgetCardGridV1,
    WidgetDiffAnnotation,
    WidgetDiffReviewV1,
    WidgetDiffSegment,
    WidgetEmptyState,
    WidgetImageItem,
    WidgetImageVariantsV1,
    WidgetListActionsV1,
    WidgetListItem,
    WidgetTimelineNode,
    WidgetTimelineV1,
)


# ---------------------------------------------------------------------------
# list-actions@v1
# ---------------------------------------------------------------------------


def test_list_actions_happy():
    w = WidgetListActionsV1(
        title="收件箱",
        items=[
            WidgetListItem(
                id="i1",
                title="主题 A",
                subtitle="发件人",
                badges=[WidgetBadge(label="未读", variant="primary")],
            ),
            WidgetListItem(id="i2", title="主题 B"),
        ],
        actions=[WidgetActionDescriptor(id="refresh", label="刷新")],
        empty=WidgetEmptyState(title="空", message="无邮件"),
    )
    w.validate()
    d = w.to_dict()
    assert d["title"] == "收件箱"
    assert len(d["items"]) == 2
    assert d["items"][0]["badges"] == [{"label": "未读", "variant": "primary"}]
    assert d["empty"] == {"title": "空", "message": "无邮件"}


def test_list_actions_missing_item_id():
    w = WidgetListActionsV1(items=[WidgetListItem(id="", title="t")])
    with pytest.raises(ValueError, match=r"items\[0\].id is required"):
        w.validate()


def test_list_actions_missing_item_title():
    w = WidgetListActionsV1(items=[WidgetListItem(id="i1", title="")])
    with pytest.raises(ValueError, match=r"items\[0\].title is required"):
        w.validate()


def test_list_actions_empty_items_is_legal():
    """items 可以是空 list（empty 占位场景）。"""
    w = WidgetListActionsV1(items=[], empty=WidgetEmptyState(title="空"))
    w.validate()
    d = w.to_dict()
    assert d["items"] == []


def test_list_actions_omits_empty_optional_fields():
    """空 list / None 字段按 omitempty 省略。"""
    w = WidgetListActionsV1(items=[WidgetListItem(id="i1", title="t")])
    d = w.to_dict()
    assert "title" not in d
    assert "actions" not in d
    assert "empty" not in d
    item = d["items"][0]
    assert "subtitle" not in item
    assert "icon" not in item
    assert "badges" not in item
    assert "metadata" not in item
    assert "row_actions" not in item


# ---------------------------------------------------------------------------
# diff-review@v1
# ---------------------------------------------------------------------------


def test_diff_review_happy():
    w = WidgetDiffReviewV1(
        title="审稿",
        subtitle="提案 v1",
        diff=[
            WidgetDiffSegment(type="context", text="abc"),
            WidgetDiffSegment(type="insert", text="def"),
            WidgetDiffSegment(type="delete", text="ghi"),
        ],
        actions=[WidgetActionDescriptor(id="approve", label="批准", variant="primary")],
        annotations=[
            WidgetDiffAnnotation(anchor_index=1, severity="warning", message="注意")
        ],
    )
    w.validate()
    d = w.to_dict()
    assert d["title"] == "审稿"
    assert len(d["diff"]) == 3
    assert d["annotations"][0] == {
        "anchor_index": 1,
        "severity": "warning",
        "message": "注意",
    }


def test_diff_review_empty_diff_rejected():
    w = WidgetDiffReviewV1(title="x", diff=[], actions=[])
    with pytest.raises(ValueError, match="diff requires at least 1 segment"):
        w.validate()


def test_diff_review_invalid_segment_type():
    w = WidgetDiffReviewV1(
        title="x",
        diff=[WidgetDiffSegment(type="weird", text="x")],
        actions=[],
    )
    with pytest.raises(ValueError, match=r"diff\[0\]: invalid segment type"):
        w.validate()


def test_diff_review_annotation_anchor_out_of_range():
    w = WidgetDiffReviewV1(
        title="x",
        diff=[WidgetDiffSegment(type="context", text="a")],
        actions=[],
        annotations=[
            WidgetDiffAnnotation(anchor_index=5, severity="info", message="oops")
        ],
    )
    with pytest.raises(ValueError, match="anchor_index 5 out of range"):
        w.validate()


def test_diff_review_annotation_negative_anchor():
    w = WidgetDiffReviewV1(
        title="x",
        diff=[WidgetDiffSegment(type="context", text="a")],
        actions=[],
        annotations=[
            WidgetDiffAnnotation(anchor_index=-1, severity="info", message="oops")
        ],
    )
    with pytest.raises(ValueError, match="out of range"):
        w.validate()


def test_diff_review_invalid_severity():
    w = WidgetDiffReviewV1(
        title="x",
        diff=[WidgetDiffSegment(type="context", text="a")],
        actions=[],
        annotations=[
            WidgetDiffAnnotation(anchor_index=0, severity="critical", message="x")
        ],
    )
    with pytest.raises(ValueError, match="invalid severity"):
        w.validate()


# ---------------------------------------------------------------------------
# timeline@v1
# ---------------------------------------------------------------------------


def test_timeline_happy():
    w = WidgetTimelineV1(
        title="发布流水线",
        events=[
            WidgetTimelineNode(
                id="n1",
                time="2026-05-04T10:00:00Z",
                title="构建",
                status="success",
            ),
            WidgetTimelineNode(
                id="n2",
                time="2026-05-04T10:05:00Z",
                title="部署",
                status="running",
                subtitle="prod",
                detail="mid",
            ),
        ],
    )
    w.validate()
    d = w.to_dict()
    assert d["title"] == "发布流水线"
    assert len(d["events"]) == 2
    assert d["events"][0]["status"] == "success"


def test_timeline_missing_event_id():
    w = WidgetTimelineV1(
        events=[
            WidgetTimelineNode(
                id="", time="2026-05-04T10:00:00Z", title="x", status="pending"
            )
        ]
    )
    with pytest.raises(ValueError, match=r"events\[0\].id is required"):
        w.validate()


def test_timeline_missing_title():
    w = WidgetTimelineV1(
        events=[
            WidgetTimelineNode(
                id="n1", time="2026-05-04T10:00:00Z", title="", status="pending"
            )
        ]
    )
    with pytest.raises(ValueError, match=r"events\[0\].title is required"):
        w.validate()


def test_timeline_invalid_time_format():
    w = WidgetTimelineV1(
        events=[
            WidgetTimelineNode(
                id="n1", time="not-a-time", title="x", status="pending"
            )
        ]
    )
    with pytest.raises(ValueError, match="invalid time"):
        w.validate()


def test_timeline_invalid_status():
    w = WidgetTimelineV1(
        events=[
            WidgetTimelineNode(
                id="n1", time="2026-05-04T10:00:00Z", title="x", status="bogus"
            )
        ]
    )
    with pytest.raises(ValueError, match="invalid status"):
        w.validate()


def test_timeline_all_statuses_valid():
    """5 个合法 status 全通过校验。"""
    for status in ("pending", "running", "success", "failed", "skipped"):
        w = WidgetTimelineV1(
            events=[
                WidgetTimelineNode(
                    id=f"n-{status}",
                    time="2026-05-04T10:00:00Z",
                    title="x",
                    status=status,
                )
            ]
        )
        w.validate()


def test_timeline_naive_datetime_rejected():
    """naive datetime（无 Z / 无 offset）必须被拒，与 Go time.RFC3339 对齐。"""
    w = WidgetTimelineV1(
        events=[
            WidgetTimelineNode(
                id="n1",
                time="2026-05-04T10:00:00",
                title="x",
                status="pending",
            )
        ]
    )
    with pytest.raises(ValueError, match="invalid time"):
        w.validate()


def test_timeline_space_separator_rejected():
    """空格分隔（非 T）必须被拒，Python fromisoformat 3.11+ 接受空格但 Go 不接受。"""
    w = WidgetTimelineV1(
        events=[
            WidgetTimelineNode(
                id="n1",
                time="2026-05-04 10:00:00Z",
                title="x",
                status="pending",
            )
        ]
    )
    with pytest.raises(ValueError, match="invalid time"):
        w.validate()


def test_timeline_with_offset_accepted():
    """带 ±HH:MM 时区 offset 的 RFC3339 应通过校验。"""
    w = WidgetTimelineV1(
        events=[
            WidgetTimelineNode(
                id="n1",
                time="2026-05-04T10:00:00+08:00",
                title="x",
                status="pending",
            )
        ]
    )
    w.validate()


def test_timeline_microseconds_accepted():
    """带小数秒的 RFC3339 应通过校验。"""
    w = WidgetTimelineV1(
        events=[
            WidgetTimelineNode(
                id="n1",
                time="2026-05-04T10:00:00.123Z",
                title="x",
                status="pending",
            )
        ]
    )
    w.validate()


def test_timeline_empty_time_rejected():
    """空字符串 time 必须被拒。"""
    w = WidgetTimelineV1(
        events=[
            WidgetTimelineNode(
                id="n1", time="", title="x", status="pending"
            )
        ]
    )
    with pytest.raises(ValueError, match="invalid time"):
        w.validate()


def test_timeline_invalid_calendar_date_rejected():
    """regex 形似 RFC3339 但日历非法（如 13 月 / 45 日）必须被拒。"""
    w = WidgetTimelineV1(
        events=[
            WidgetTimelineNode(
                id="n1",
                time="2026-13-45T10:00:00Z",
                title="x",
                status="pending",
            )
        ]
    )
    with pytest.raises(ValueError, match="invalid time"):
        w.validate()


# ---------------------------------------------------------------------------
# card-grid@v1
# ---------------------------------------------------------------------------


def test_card_grid_happy():
    w = WidgetCardGridV1(
        title="搜索结果",
        columns=3,
        cards=[
            WidgetCard(
                id="c1",
                title="结果 A",
                excerpt="摘要",
                image_url="https://cdn.example.com/a.png",
                source_url="https://example.com/a",
                source_label="example.com",
                score=0.85,
            ),
            WidgetCard(id="c2", title="结果 B"),
        ],
    )
    w.validate()
    d = w.to_dict()
    assert d["columns"] == 3
    assert d["cards"][0]["score"] == 0.85
    assert "score" not in d["cards"][1]


def test_card_grid_columns_out_of_range():
    """columns ∉ [1,4] 拒绝（0 视为未设置——等价 default 2）。"""
    for bad in (5, 10):
        w = WidgetCardGridV1(columns=bad, cards=[WidgetCard(id="c1", title="t")])
        with pytest.raises(ValueError, match=r"columns must be in \[1,4\]"):
            w.validate()


def test_card_grid_columns_negative_rejected():
    """columns < 0 也被认为越界。"""
    w = WidgetCardGridV1(columns=-1, cards=[WidgetCard(id="c1", title="t")])
    with pytest.raises(ValueError, match=r"columns must be in \[1,4\]"):
        w.validate()


def test_card_grid_columns_zero_means_unset():
    """columns=0 视为未设置（前端默认 2 列）；不应触发越界校验。"""
    w = WidgetCardGridV1(columns=0, cards=[WidgetCard(id="c1", title="t")])
    w.validate()


def test_card_grid_card_missing_id():
    w = WidgetCardGridV1(cards=[WidgetCard(id="", title="t")])
    with pytest.raises(ValueError, match=r"cards\[0\].id is required"):
        w.validate()


def test_card_grid_card_missing_title():
    w = WidgetCardGridV1(cards=[WidgetCard(id="c1", title="")])
    with pytest.raises(ValueError, match=r"cards\[0\].title is required"):
        w.validate()


def test_card_grid_image_url_must_be_https():
    """C1 修复点：image_url 严格 https（inline 渲染防 XSS）。"""
    w = WidgetCardGridV1(
        cards=[WidgetCard(id="c1", title="t", image_url="http://evil/a.png")]
    )
    with pytest.raises(ValueError, match="must be https URL"):
        w.validate()


def test_card_grid_image_url_javascript_rejected():
    w = WidgetCardGridV1(
        cards=[
            WidgetCard(id="c1", title="t", image_url="javascript:alert(1)")
        ]
    )
    with pytest.raises(ValueError, match="must be https URL"):
        w.validate()


def test_card_grid_source_url_allows_http():
    """source_url 是用户点击跳转，允许 http/https。"""
    w = WidgetCardGridV1(
        cards=[
            WidgetCard(id="c1", title="t", source_url="http://example.com/a")
        ]
    )
    w.validate()


def test_card_grid_source_url_rejects_javascript():
    w = WidgetCardGridV1(
        cards=[WidgetCard(id="c1", title="t", source_url="javascript:alert(1)")]
    )
    with pytest.raises(ValueError, match="unsupported source_url scheme"):
        w.validate()


def test_card_grid_score_out_of_range():
    for bad in (-0.1, 1.5):
        w = WidgetCardGridV1(cards=[WidgetCard(id="c1", title="t", score=bad)])
        with pytest.raises(ValueError, match=r"score must be in \[0,1\]"):
            w.validate()


def test_card_grid_score_boundary_zero_one():
    """score=0/1 边界合法。"""
    for s in (0.0, 1.0):
        w = WidgetCardGridV1(cards=[WidgetCard(id="c1", title="t", score=s)])
        w.validate()


# ---------------------------------------------------------------------------
# image-variants@v1
# ---------------------------------------------------------------------------


def test_image_variants_happy():
    w = WidgetImageVariantsV1(
        title="生成结果",
        primary=WidgetImageItem(
            id="p1",
            url="https://cdn.example.com/p.png",
            alt_text="主图",
            width=1024,
            height=1024,
        ),
        variants=[
            WidgetImageItem(
                id="v1", url="https://cdn.example.com/v.png", alt_text="变体"
            )
        ],
    )
    w.validate()
    d = w.to_dict()
    assert d["primary"]["alt_text"] == "主图"
    assert d["variants"][0]["url"] == "https://cdn.example.com/v.png"


def test_image_variants_primary_url_required():
    w = WidgetImageVariantsV1(
        primary=WidgetImageItem(id="p1", url="", alt_text="x")
    )
    with pytest.raises(ValueError, match="primary.url is required"):
        w.validate()


def test_image_variants_primary_url_must_be_https():
    w = WidgetImageVariantsV1(
        primary=WidgetImageItem(id="p1", url="http://evil/a.png", alt_text="x")
    )
    with pytest.raises(ValueError, match="must be https URL"):
        w.validate()


def test_image_variants_alt_text_required():
    w = WidgetImageVariantsV1(
        primary=WidgetImageItem(
            id="p1", url="https://cdn.example.com/p.png", alt_text=""
        )
    )
    with pytest.raises(ValueError, match="alt_text required"):
        w.validate()


def test_image_variants_negative_width():
    w = WidgetImageVariantsV1(
        primary=WidgetImageItem(
            id="p1",
            url="https://cdn.example.com/p.png",
            alt_text="x",
            width=-1,
        )
    )
    with pytest.raises(ValueError, match="width must be > 0"):
        w.validate()


def test_image_variants_negative_height():
    w = WidgetImageVariantsV1(
        primary=WidgetImageItem(
            id="p1",
            url="https://cdn.example.com/p.png",
            alt_text="x",
            height=-1,
        )
    )
    with pytest.raises(ValueError, match="height must be > 0"):
        w.validate()


def test_image_variants_zero_width_height_legal():
    """width/height = 0（默认未设置）合法。"""
    w = WidgetImageVariantsV1(
        primary=WidgetImageItem(
            id="p1", url="https://cdn.example.com/p.png", alt_text="x"
        )
    )
    w.validate()


def test_image_variants_variant_url_validated():
    """variants 中的 URL 同样校验（非 https 拒绝）。"""
    w = WidgetImageVariantsV1(
        primary=WidgetImageItem(
            id="p1", url="https://cdn.example.com/p.png", alt_text="x"
        ),
        variants=[
            WidgetImageItem(id="v1", url="http://evil/v.png", alt_text="bad")
        ],
    )
    with pytest.raises(ValueError, match=r"variants\[0\].url"):
        w.validate()


# ---------------------------------------------------------------------------
# 通用辅助类型 to_dict 行为
# ---------------------------------------------------------------------------


def test_widget_action_descriptor_omits_defaults():
    a = WidgetActionDescriptor(id="x", label="X")
    d = a.to_dict()
    assert d == {"id": "x", "label": "X"}


def test_widget_action_descriptor_full():
    a = WidgetActionDescriptor(
        id="x",
        label="X",
        variant="destructive",
        icon="trash",
        disabled=True,
        tooltip="tip",
        confirm_prompt="确认?",
    )
    d = a.to_dict()
    assert d == {
        "id": "x",
        "label": "X",
        "variant": "destructive",
        "icon": "trash",
        "disabled": True,
        "tooltip": "tip",
        "confirm_prompt": "确认?",
    }


def test_widget_badge_omits_empty_variant():
    b = WidgetBadge(label="未读")
    assert b.to_dict() == {"label": "未读"}


def test_widget_badge_with_variant():
    b = WidgetBadge(label="错误", variant="destructive")
    assert b.to_dict() == {"label": "错误", "variant": "destructive"}


def test_widget_empty_state_omits_optional():
    e = WidgetEmptyState(title="空")
    assert e.to_dict() == {"title": "空"}


def test_widget_empty_state_full():
    e = WidgetEmptyState(icon="inbox", title="空", message="无内容")
    assert e.to_dict() == {"icon": "inbox", "title": "空", "message": "无内容"}
