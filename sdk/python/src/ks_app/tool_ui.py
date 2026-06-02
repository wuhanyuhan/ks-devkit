"""widgets-protocol-v1 tool UI binding 与 ToolResult 类型。

对齐 ks-types/widgets.go 中的 ToolUIBinding / MetaUIDecl / MetaKeystoneUIDecl。
本模块提供：

- ToolUIBinding：squad 在 /meta.tools[]._meta.ui 里声明的 widget 绑定
- ToolResult：widgets-protocol-v1 推荐的 tool 返回值（链式 with_text /
  with_ui_override / with_ui_data / to_json）

序列化字段名遵循 snake_case，与 Go json tag 对齐；空 list / None 字段按
omitempty 语义省略。
"""
from __future__ import annotations

import json
from dataclasses import dataclass, field
from typing import Any


@dataclass
class ToolUIBinding:
    """squad 声明的 widget 绑定（写入 /meta.tools[]._meta.ui）。

    Attributes:
        widget: widget URI（例如 "ks://widgets/diff-review@v1" 或自定义
                "ui://{squad}/{path}"）。
        sandbox_hints: iframe sandbox flag 白名单（可选）。空列表按 omitempty
                       省略；非空时通过 to_dict 复制一份避免下游 mutate 污染。
    """

    widget: str
    sandbox_hints: list[str] = field(default_factory=list)

    def to_dict(self) -> dict[str, Any]:
        out: dict[str, Any] = {"widget": self.widget}
        if self.sandbox_hints:
            out["sandbox_hints"] = list(self.sandbox_hints)
        return out


class ToolResult:
    """widgets-protocol-v1 推荐的 tool 返回值。

    用法::

        return (
            ToolResult()
            .with_text("已审阅")
            .with_ui_data(WidgetDiffReviewV1(...))
            .to_json()
        )

    序列化结构对齐 MCP CallToolResult + _meta 扩展：

        {
          "content": [{"type": "text", "text": "..."}],   # with_text 时
          "_meta": {
            "ui": {"widget": "...", "sandbox_hints": [...]},  # with_ui_override 时
            "keystone": {"ui": {"data": {...}}}                # with_ui_data 时
          }
        }
    """

    def __init__(self) -> None:
        self._text: str = ""
        self._ui_override: dict[str, Any] | None = None
        self._ui_data: Any = None
        self._ui_data_set: bool = False

    def with_text(self, text: str) -> "ToolResult":
        """填充文本内容（CallToolResult.content[0]）。"""
        self._text = text
        return self

    def with_ui_override(
        self, widget: str, sandbox_hints: list[str] | None = None
    ) -> "ToolResult":
        """显式覆盖渲染 widget（写入 _meta.ui，优先于 squad 声明的 binding）。"""
        decl: dict[str, Any] = {"widget": widget}
        if sandbox_hints:
            decl["sandbox_hints"] = list(sandbox_hints)
        self._ui_override = decl
        return self

    def with_ui_data(self, data: Any) -> "ToolResult":
        """填充 widget 数据（写入 _meta.keystone.ui.data）。

        data 可以是任意带 ``to_dict()`` 方法的对象（例如 5 个 widget data
        类）或裸 dict。若对象暴露 ``validate()``，``to_json`` 会在序列化前
        调用它做规则校验，校验失败抛 ValueError。
        """
        self._ui_data = data
        self._ui_data_set = True
        return self

    def to_dict(self) -> dict[str, Any]:
        """构造 CallToolResult dict（含 content + _meta）。

        与 ToolUIBinding.to_dict / 5 widget data to_dict 风格一致，便于在测试
        与 caller 中直接断言结构，无需 json.loads(to_json())。

        Raises:
            ValueError: ui_data.validate() 校验失败时（消息直接透传）。
        """
        out: dict[str, Any] = {}
        if self._text:
            out["content"] = [{"type": "text", "text": self._text}]

        meta: dict[str, Any] = {}
        if self._ui_override is not None:
            meta["ui"] = self._ui_override
        if self._ui_data_set:
            if hasattr(self._ui_data, "validate"):
                self._ui_data.validate()
            data_dict = (
                self._ui_data.to_dict()
                if hasattr(self._ui_data, "to_dict")
                else self._ui_data
            )
            meta["keystone"] = {"ui": {"data": data_dict}}
        if meta:
            out["_meta"] = meta

        return out

    def to_json(self) -> str:
        """序列化为 JSON 字符串。

        Raises:
            ValueError: ui_data.validate() 校验失败时（消息直接透传）。
        """
        return json.dumps(self.to_dict(), ensure_ascii=False)
