"""widgets-protocol-v1 五个 MVP widget 的数据 schema（dataclass + validate + to_dict）。

字段名 / 类型 / 必填规则 / 校验逻辑与 ks-types Go 端 widgets_data.go 一一对齐。
JSON 序列化遵循 snake_case，空 list / None / 默认值字段按 omitempty 省略。

每个 widget 类提供：
- dataclass 字段（与 Go struct 字段同名 snake_case）
- validate(self) -> None：校验失败时抛 ValueError，消息与 Go 端一致
- to_dict(self) -> dict：序列化为 dict，可直接 json.dumps
"""
from __future__ import annotations

import re
from dataclasses import dataclass, field
from datetime import datetime
from typing import Any
from urllib.parse import urlparse


# ---------------------------------------------------------------------------
# 通用辅助
# ---------------------------------------------------------------------------


def _validate_https_url(prefix: str, url_str: str) -> None:
    """校验 URL 是 https；空字符串视为合法（caller 决定是否必填）。

    严格限定 scheme 为 https，拒绝 http / ftp / javascript / data 等。
    适用于直接进入前端 inline 渲染的 URL（如 <img src>），防 XSS。
    与 ks-types Go validateHTTPSURL 完全等价。
    """
    if url_str == "":
        return
    parsed = urlparse(url_str)
    if parsed.scheme != "https":
        raise ValueError(f'{prefix}: must be https URL, got "{url_str}"')


def _omit_if_empty(d: dict[str, Any], key: str, value: Any) -> None:
    """把 key:value 写入 dict，但 None / 空字符串 / 空 list / 空 dict 跳过。

    适用类型：None / str / list / dict。

    **不适用 bool 或 int 字段**：caller 自行决定 inline omit 逻辑——
    例如 disabled (bool) 仅 True 时输出（参考 WidgetActionDescriptor.to_dict），
    width / height (int) 非负但 0 视为未设置（参考 WidgetImageItem.to_dict）。
    """
    if value is None:
        return
    if isinstance(value, (str, list, dict)) and len(value) == 0:
        return
    d[key] = value


# ---------------------------------------------------------------------------
# WidgetActionDescriptor / WidgetBadge / WidgetEmptyState
# ---------------------------------------------------------------------------


@dataclass
class WidgetActionDescriptor:
    """widget 上的可点击 action（按钮）。

    Variant 枚举：primary / default / destructive / ghost / link
    confirm_prompt 非空时点击需二次确认。
    """

    id: str
    label: str
    variant: str = ""
    icon: str = ""
    disabled: bool = False
    tooltip: str = ""
    confirm_prompt: str = ""

    def to_dict(self) -> dict[str, Any]:
        out: dict[str, Any] = {"id": self.id, "label": self.label}
        _omit_if_empty(out, "variant", self.variant)
        _omit_if_empty(out, "icon", self.icon)
        if self.disabled:
            out["disabled"] = True
        _omit_if_empty(out, "tooltip", self.tooltip)
        _omit_if_empty(out, "confirm_prompt", self.confirm_prompt)
        return out


@dataclass
class WidgetBadge:
    """widget 上的角标标签。"""

    label: str
    variant: str = ""

    def to_dict(self) -> dict[str, Any]:
        out: dict[str, Any] = {"label": self.label}
        _omit_if_empty(out, "variant", self.variant)
        return out


@dataclass
class WidgetEmptyState:
    """widget 列表为空时的占位提示。"""

    title: str
    icon: str = ""
    message: str = ""

    def to_dict(self) -> dict[str, Any]:
        out: dict[str, Any] = {"title": self.title}
        _omit_if_empty(out, "icon", self.icon)
        _omit_if_empty(out, "message", self.message)
        return out


def _actions_to_list(actions: list[WidgetActionDescriptor]) -> list[dict[str, Any]]:
    return [a.to_dict() for a in actions]


def _badges_to_list(badges: list[WidgetBadge]) -> list[dict[str, Any]]:
    return [b.to_dict() for b in badges]


# ---------------------------------------------------------------------------
# ks://widgets/list-actions@v1
# ---------------------------------------------------------------------------


@dataclass
class WidgetListItem:
    """list-actions widget 中的单行条目。"""

    id: str
    title: str
    subtitle: str = ""
    icon: str = ""
    badges: list[WidgetBadge] = field(default_factory=list)
    metadata: dict[str, Any] = field(default_factory=dict)
    row_actions: list[WidgetActionDescriptor] = field(default_factory=list)

    def to_dict(self) -> dict[str, Any]:
        out: dict[str, Any] = {"id": self.id, "title": self.title}
        _omit_if_empty(out, "subtitle", self.subtitle)
        _omit_if_empty(out, "icon", self.icon)
        if self.badges:
            out["badges"] = _badges_to_list(self.badges)
        if self.metadata:
            out["metadata"] = dict(self.metadata)
        if self.row_actions:
            out["row_actions"] = _actions_to_list(self.row_actions)
        return out


@dataclass
class WidgetListActionsV1:
    """ks://widgets/list-actions@v1 数据 schema。"""

    items: list[WidgetListItem]
    title: str = ""
    actions: list[WidgetActionDescriptor] = field(default_factory=list)
    empty: WidgetEmptyState | None = None

    def validate(self) -> None:
        for i, it in enumerate(self.items):
            if it.id == "":
                raise ValueError(f"items[{i}].id is required")
            if it.title == "":
                raise ValueError(f"items[{i}].title is required")

    def to_dict(self) -> dict[str, Any]:
        out: dict[str, Any] = {"items": [it.to_dict() for it in self.items]}
        _omit_if_empty(out, "title", self.title)
        if self.actions:
            out["actions"] = _actions_to_list(self.actions)
        if self.empty is not None:
            out["empty"] = self.empty.to_dict()
        return out


# ---------------------------------------------------------------------------
# ks://widgets/diff-review@v1
# ---------------------------------------------------------------------------


_DIFF_SEGMENT_TYPES = frozenset({"context", "insert", "delete"})
_DIFF_ANNOTATION_SEVERITIES = frozenset({"info", "warning", "error"})


@dataclass
class WidgetDiffSegment:
    """diff-review widget 中的一段 diff 文本。"""

    type: str  # "context" | "insert" | "delete"
    text: str

    def to_dict(self) -> dict[str, Any]:
        return {"type": self.type, "text": self.text}


@dataclass
class WidgetDiffAnnotation:
    """挂在某段 diff 上的批注（可选）。"""

    anchor_index: int
    severity: str  # "info" | "warning" | "error"
    message: str

    def to_dict(self) -> dict[str, Any]:
        return {
            "anchor_index": self.anchor_index,
            "severity": self.severity,
            "message": self.message,
        }


@dataclass
class WidgetDiffReviewV1:
    """ks://widgets/diff-review@v1 数据 schema。"""

    title: str
    diff: list[WidgetDiffSegment]
    actions: list[WidgetActionDescriptor]
    subtitle: str = ""
    annotations: list[WidgetDiffAnnotation] = field(default_factory=list)

    def validate(self) -> None:
        if len(self.diff) < 1:
            raise ValueError("diff requires at least 1 segment")
        for i, seg in enumerate(self.diff):
            if seg.type not in _DIFF_SEGMENT_TYPES:
                raise ValueError(
                    f'diff[{i}]: invalid segment type "{seg.type}"'
                )
        for i, ann in enumerate(self.annotations):
            if ann.anchor_index < 0 or ann.anchor_index >= len(self.diff):
                raise ValueError(
                    f"annotations[{i}].anchor_index {ann.anchor_index} "
                    f"out of range [0,{len(self.diff)})"
                )
            if ann.severity not in _DIFF_ANNOTATION_SEVERITIES:
                raise ValueError(
                    f'annotations[{i}]: invalid severity "{ann.severity}"'
                )

    def to_dict(self) -> dict[str, Any]:
        out: dict[str, Any] = {
            "title": self.title,
            "diff": [seg.to_dict() for seg in self.diff],
            "actions": _actions_to_list(self.actions),
        }
        _omit_if_empty(out, "subtitle", self.subtitle)
        if self.annotations:
            out["annotations"] = [a.to_dict() for a in self.annotations]
        return out


# ---------------------------------------------------------------------------
# ks://widgets/timeline@v1
# ---------------------------------------------------------------------------


_TIMELINE_NODE_STATUSES = frozenset(
    {"pending", "running", "success", "failed", "skipped"}
)


# 严格 RFC3339：date "T" time(.frac)? (Z|±HH:MM|±HHMM|±HH)
# 与 Go time.Parse(time.RFC3339, ...) 行为对齐：
#   - 必须 'T' 分隔（拒绝空格，避免 fromisoformat 3.11+ 的宽松接受）
#   - 必须以 Z 或 ±时区 offset 结尾（拒绝 naive datetime）
#   - 允许小数秒
_RFC3339_RE = re.compile(
    r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:?\d{2}|[+-]\d{2})$"
)


def _validate_rfc3339(value: str) -> bool:
    """校验 value 是否合法 RFC3339（与 Go time.Parse(time.RFC3339, ...) 完全等价）。

    严格规则（与 Go RFC3339 对齐）：
    - 必须以 'Z' 或时区 offset（±HH:MM / ±HHMM / ±HH）结尾
    - 拒绝 naive datetime（无 timezone 信息）
    - 拒绝空格分隔（Python 3.11+ fromisoformat 接受空格，但 Go 不接受）
    - 允许小数秒
    """
    if value == "":
        return False
    if not _RFC3339_RE.match(value):
        return False
    # 进一步校验是合法日期时间（拒绝 2026-13-45T... 之类的非法值）
    try:
        datetime.fromisoformat(value.replace("Z", "+00:00"))
        return True
    except ValueError:
        return False


@dataclass
class WidgetTimelineNode:
    """timeline widget 上的单个节点。Time 必须为 RFC3339（ISO8601 UTC）。"""

    id: str
    time: str
    title: str
    status: str  # pending|running|success|failed|skipped
    subtitle: str = ""
    detail: str = ""
    actions: list[WidgetActionDescriptor] = field(default_factory=list)

    def to_dict(self) -> dict[str, Any]:
        out: dict[str, Any] = {
            "id": self.id,
            "time": self.time,
            "title": self.title,
            "status": self.status,
        }
        _omit_if_empty(out, "subtitle", self.subtitle)
        _omit_if_empty(out, "detail", self.detail)
        if self.actions:
            out["actions"] = _actions_to_list(self.actions)
        return out


@dataclass
class WidgetTimelineV1:
    """ks://widgets/timeline@v1 数据 schema。"""

    events: list[WidgetTimelineNode]
    title: str = ""

    def validate(self) -> None:
        for i, n in enumerate(self.events):
            if n.id == "":
                raise ValueError(f"events[{i}].id is required")
            if n.title == "":
                raise ValueError(f"events[{i}].title is required")
            if not _validate_rfc3339(n.time):
                raise ValueError(
                    f'events[{i}]: invalid time "{n.time}" (expect RFC3339)'
                )
            if n.status not in _TIMELINE_NODE_STATUSES:
                raise ValueError(f'events[{i}]: invalid status "{n.status}"')

    def to_dict(self) -> dict[str, Any]:
        out: dict[str, Any] = {"events": [e.to_dict() for e in self.events]}
        _omit_if_empty(out, "title", self.title)
        return out


# ---------------------------------------------------------------------------
# ks://widgets/card-grid@v1
# ---------------------------------------------------------------------------


@dataclass
class WidgetCard:
    """card-grid widget 中的单张卡片。

    image_url 必须 https（inline 渲染防 XSS）；source_url 允许 http/https
    （用户点击跳转）；score 若设置必须在 [0,1] 区间。
    """

    id: str
    title: str
    excerpt: str = ""
    image_url: str = ""
    source_label: str = ""
    source_url: str = ""
    score: float | None = None
    badges: list[WidgetBadge] = field(default_factory=list)
    actions: list[WidgetActionDescriptor] = field(default_factory=list)

    def to_dict(self) -> dict[str, Any]:
        out: dict[str, Any] = {"id": self.id, "title": self.title}
        _omit_if_empty(out, "excerpt", self.excerpt)
        _omit_if_empty(out, "image_url", self.image_url)
        _omit_if_empty(out, "source_label", self.source_label)
        _omit_if_empty(out, "source_url", self.source_url)
        if self.score is not None:
            out["score"] = self.score
        if self.badges:
            out["badges"] = _badges_to_list(self.badges)
        if self.actions:
            out["actions"] = _actions_to_list(self.actions)
        return out


@dataclass
class WidgetCardGridV1:
    """ks://widgets/card-grid@v1 数据 schema。

    columns 取值范围 [1,4]；0 视为未设置（前端默认 2 列）。
    """

    cards: list[WidgetCard] = field(default_factory=list)
    title: str = ""
    columns: int = 0

    def validate(self) -> None:
        if self.columns != 0 and (self.columns < 1 or self.columns > 4):
            raise ValueError(f"columns must be in [1,4], got {self.columns}")
        for i, c in enumerate(self.cards):
            if c.id == "":
                raise ValueError(f"cards[{i}].id is required")
            if c.title == "":
                raise ValueError(f"cards[{i}].title is required")
            _validate_https_url(f"cards[{i}].image_url", c.image_url)
            if c.source_url != "":
                parsed = urlparse(c.source_url)
                if parsed.scheme not in ("http", "https"):
                    raise ValueError(
                        f'cards[{i}]: unsupported source_url scheme '
                        f'in "{c.source_url}"'
                    )
            if c.score is not None and (c.score < 0 or c.score > 1):
                raise ValueError(
                    f"cards[{i}]: score must be in [0,1], got {c.score}"
                )

    def to_dict(self) -> dict[str, Any]:
        out: dict[str, Any] = {"cards": [c.to_dict() for c in self.cards]}
        _omit_if_empty(out, "title", self.title)
        if self.columns != 0:
            out["columns"] = self.columns
        return out


# ---------------------------------------------------------------------------
# ks://widgets/image-variants@v1
# ---------------------------------------------------------------------------


@dataclass
class WidgetImageItem:
    """image-variants widget 中的单张图片条目。

    URL 必须 https；alt_text 必填；width/height 若设置必须 ≥ 0。
    """

    id: str
    url: str
    alt_text: str
    width: int = 0
    height: int = 0
    caption: str = ""
    actions: list[WidgetActionDescriptor] = field(default_factory=list)

    def to_dict(self) -> dict[str, Any]:
        out: dict[str, Any] = {
            "id": self.id,
            "url": self.url,
            "alt_text": self.alt_text,
        }
        if self.width > 0:
            out["width"] = self.width
        if self.height > 0:
            out["height"] = self.height
        _omit_if_empty(out, "caption", self.caption)
        if self.actions:
            out["actions"] = _actions_to_list(self.actions)
        return out


def _validate_image_item(prefix: str, it: WidgetImageItem) -> None:
    if it.url == "":
        raise ValueError(f"{prefix}.url is required")
    _validate_https_url(f"{prefix}.url", it.url)
    if it.alt_text == "":
        raise ValueError(f"{prefix}.alt_text required")
    if it.width < 0:
        raise ValueError(f"{prefix}.width must be > 0, got {it.width}")
    if it.height < 0:
        raise ValueError(f"{prefix}.height must be > 0, got {it.height}")


@dataclass
class WidgetImageVariantsV1:
    """ks://widgets/image-variants@v1 数据 schema。"""

    primary: WidgetImageItem
    title: str = ""
    variants: list[WidgetImageItem] = field(default_factory=list)
    actions: list[WidgetActionDescriptor] = field(default_factory=list)

    def validate(self) -> None:
        _validate_image_item("primary", self.primary)
        for i, v in enumerate(self.variants):
            _validate_image_item(f"variants[{i}]", v)

    def to_dict(self) -> dict[str, Any]:
        out: dict[str, Any] = {"primary": self.primary.to_dict()}
        _omit_if_empty(out, "title", self.title)
        if self.variants:
            out["variants"] = [v.to_dict() for v in self.variants]
        if self.actions:
            out["actions"] = _actions_to_list(self.actions)
        return out
