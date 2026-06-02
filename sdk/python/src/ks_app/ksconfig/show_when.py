"""show_when DSL 解析器（手写递归下降 parser）+ JSON Schema 编译器。

镜像 Go `ks-devkit/sdk/go/ksapp/ksconfig/show_when.go`。

DSL 语法（EBNF）:

    expr       = or_expr
    or_expr    = and_expr { "||" and_expr }
    and_expr   = cmp_expr { "&&" cmp_expr }
    cmp_expr   = field op literal | field "in" "[" literals "]"
    field      = identifier           # 不支持 `.` 跨 level
    op         = "==" | "!="
    literal    = string | number | bool | null

禁止规则:
  - 括号嵌套 ``(a || b) && c`` → SyntaxError（programmer error，对齐 Go panic）
  - 跨 level 字段引用 ``parent.field == 'x'`` → ValueError
  - 算术运算 ``x + 1 == 2`` → ValueError
  - 未闭合字符串 ``field == 'unclosed`` → ValueError

错误模型:
  - SyntaxError: 括号嵌套（programmer error，需要改代码而非改数据）
  - ValueError: 跨 level / 算术 / 未闭合字符串 / 非法字面量 / 空 in 列表 等可恢复错

返回值:
  - ``if_then_else``: ``{"if": ..., "then": {"required": [...]}, "else": {"properties": {...}}}``
    调用方（reflect.py）负责把它包进 ``allOf`` 加到 schema 上。
  - ``ui_hidden_when``: rjsf widget 消费的 AST 副本；cmp / in / logical 三种 shape。
"""
from __future__ import annotations

from dataclasses import dataclass, field as _dc_field
from typing import Any


@dataclass
class ShowWhenNode:
    """show_when 表达式的 AST 节点。

    一个节点只有一种形态（由 ``kind`` 决定使用哪些字段）：
      - ``'cmp'``: 使用 ``field`` / ``op`` / ``literal``
      - ``'in'``:  使用 ``field`` / ``literals``
      - ``'and'`` / ``'or'``: 使用 ``left`` / ``right``
    """

    kind: str  # 'cmp' | 'in' | 'and' | 'or'
    field: str = ""  # cmp / in 专用
    op: str = ""  # cmp 专用：'==' | '!='
    literal: Any = None  # cmp 专用
    literals: list[Any] = _dc_field(default_factory=list)  # in 专用
    left: "ShowWhenNode | None" = None  # and / or 专用
    right: "ShowWhenNode | None" = None  # and / or 专用


# ---------------------------------------------------------------------------
# Parser（递归下降）
# ---------------------------------------------------------------------------


class _Parser:
    """show_when DSL 的递归下降 parser 状态。

    每个 ``parse_*`` 方法从 ``self.pos`` 处消费输入，返回 AST 节点或字面量。
    """

    def __init__(self, src: str) -> None:
        self.src = src
        self.pos = 0

    # ------------------------------------------------------------------ utils

    def peek(self) -> str:
        """返回当前位置的字符；越界返回空串（对齐 Go 的 byte 0）。"""
        if self.pos >= len(self.src):
            return ""
        return self.src[self.pos]

    def skip_whitespace(self) -> None:
        """跳过空白（空格 / tab / 换行）。"""
        while self.pos < len(self.src) and self.src[self.pos].isspace():
            self.pos += 1

    def consume(self, s: str) -> bool:
        """若当前位置以 ``s`` 为前缀则消费并返回 True，否则 False。"""
        if self.src.startswith(s, self.pos):
            self.pos += len(s)
            return True
        return False

    def consume_word(self, s: str) -> bool:
        """消费关键字 ``s``，要求后续字符不是 identifier 续字符。

        用于 ``in`` / ``true`` / ``false`` / ``null``，避免把 ``inbox`` / ``trueFlag``
        / ``nullFoo`` 之类的 identifier 误识别为关键字。
        """
        self.skip_whitespace()
        if not self.src.startswith(s, self.pos):
            return False
        end = self.pos + len(s)
        if end < len(self.src):
            c = self.src[end]
            if c == "_" or c.isalpha() or c.isdigit():
                return False
        self.pos = end
        return True

    # ------------------------------------------------------------------ expr

    def parse_or(self) -> ShowWhenNode:
        """or_expr = and_expr { "||" and_expr }"""
        left = self.parse_and()
        while True:
            self.skip_whitespace()
            if not self.consume("||"):
                break
            self.skip_whitespace()
            right = self.parse_and()
            left = ShowWhenNode(kind="or", left=left, right=right)
        return left

    def parse_and(self) -> ShowWhenNode:
        """and_expr = cmp_expr { "&&" cmp_expr }"""
        left = self.parse_cmp()
        while True:
            self.skip_whitespace()
            if not self.consume("&&"):
                break
            self.skip_whitespace()
            right = self.parse_cmp()
            left = ShowWhenNode(kind="and", left=left, right=right)
        return left

    def parse_cmp(self) -> ShowWhenNode:
        """cmp_expr = field op literal | field "in" "[" literals "]"

        禁止：
          - 括号嵌套 → SyntaxError（programmer error，对齐 Go panic）
          - 跨 level 字段（identifier 含 '.'）→ ValueError
          - field 后出现算术运算符（+ - * /）→ ValueError
        """
        self.skip_whitespace()
        # 括号嵌套不支持（programmer error）
        if self.peek() == "(":
            raise SyntaxError(
                f"show_when: 括号嵌套不支持（spec-v1 §3.3），位置 {self.pos}"
            )

        field_name = self._parse_identifier()
        if "." in field_name:
            raise ValueError(
                f"show_when: 跨 level 字段引用不支持 ({field_name!r})"
            )

        self.skip_whitespace()
        # field 之后出现算术运算符，明确分类为 "算术运算不支持"
        c = self.peek()
        if c in ("+", "-", "*", "/"):
            raise ValueError(
                f"show_when: 算术运算不支持 ({c!r} 位置 {self.pos})"
            )

        # "in" 关键字（word-boundary，避免吃掉 inbox / interval 等以 "in" 开头的字段名）
        if self.consume_word("in"):
            self.skip_whitespace()
            if not self.consume("["):
                raise ValueError(
                    f"show_when: 'in' 后应为 '['，位置 {self.pos}"
                )
            literals = self._parse_literals()
            self.skip_whitespace()
            if not self.consume("]"):
                raise ValueError(
                    f"show_when: 'in' 列表未闭合，缺少 ']'，位置 {self.pos}"
                )
            return ShowWhenNode(kind="in", field=field_name, literals=literals)

        # "==" / "!="
        if self.consume("=="):
            op = "=="
        elif self.consume("!="):
            op = "!="
        else:
            remaining = self.src[self.pos :]
            raise ValueError(
                f"show_when: 期望 '==' / '!=' / 'in'，位置 {self.pos} "
                f"(remaining={remaining!r})"
            )

        self.skip_whitespace()
        literal = self._parse_literal()
        return ShowWhenNode(kind="cmp", field=field_name, op=op, literal=literal)

    # ------------------------------------------------------------- identifier

    def _parse_identifier(self) -> str:
        """读取一个 identifier（含可能的 ``.``；``.`` 后续由 parse_cmp 用 ValueError 拒绝）。

        合法字符：字母 / 数字 / 下划线 / 点；首字符必须为字母或下划线。
        """
        self.skip_whitespace()
        if self.pos >= len(self.src):
            raise ValueError("show_when: 期望标识符，输入已结束")
        c = self.src[self.pos]
        if not (c.isalpha() or c == "_"):
            raise ValueError(
                f"show_when: 标识符首字符非法 {c!r} 位置 {self.pos}"
            )
        start = self.pos
        while self.pos < len(self.src):
            c = self.src[self.pos]
            if c.isalpha() or c.isdigit() or c == "_" or c == ".":
                self.pos += 1
            else:
                break
        if self.pos == start:
            raise ValueError(f"show_when: 期望标识符，位置 {self.pos}")
        return self.src[start : self.pos]

    # ---------------------------------------------------------------- literal

    def _parse_literals(self) -> list[Any]:
        """读取 ``in [...]`` 中的多个 literal（逗号分隔，非空）。"""
        self.skip_whitespace()
        if self.peek() == "]":
            raise ValueError(
                f"show_when: 'in' 列表不可为空，位置 {self.pos}"
            )
        out: list[Any] = []
        while True:
            self.skip_whitespace()
            out.append(self._parse_literal())
            self.skip_whitespace()
            if not self.consume(","):
                break
        return out

    def _parse_literal(self) -> Any:
        """按首字符分派到具体 literal 解析器。"""
        self.skip_whitespace()
        if self.pos >= len(self.src):
            raise ValueError("show_when: 期望字面量，输入已结束")
        c = self.src[self.pos]
        if c == "'":
            return self._parse_string()
        if c in ("t", "f"):
            return self._parse_bool()
        if c == "n":
            return self._parse_null()
        if c.isdigit() or c == "-":
            return self._parse_number()
        raise ValueError(
            f"show_when: 无法识别的字面量起始字符 {c!r} 位置 {self.pos}"
        )

    def _parse_string(self) -> str:
        """读取单引号字符串（MVP：禁转义）。"""
        if self.peek() != "'":
            raise ValueError(
                f"show_when: 字符串应以 ' 开头，位置 {self.pos}"
            )
        self.pos += 1  # consume 开头 '
        start = self.pos
        while self.pos < len(self.src) and self.src[self.pos] != "'":
            self.pos += 1
        if self.pos >= len(self.src):
            raise ValueError(
                f"show_when: 未闭合字符串，起始位置 {start - 1}"
            )
        s = self.src[start : self.pos]
        self.pos += 1  # consume 结尾 '
        return s

    def _parse_bool(self) -> bool:
        """读取 true/false（word-boundary，避免 trueFlag / falseX 被吃掉）。"""
        if self.consume_word("true"):
            return True
        if self.consume_word("false"):
            return False
        raise ValueError(f"show_when: 期望 true/false，位置 {self.pos}")

    def _parse_null(self) -> None:
        """读取 null（word-boundary，避免 nullFoo 被吃掉）。"""
        if self.consume_word("null"):
            return None
        raise ValueError(f"show_when: 期望 null，位置 {self.pos}")

    def _parse_number(self) -> int:
        """读取整数字面量，返回 Python int（对齐 Go int64；JSON 序列化后一致）。"""
        start = self.pos
        if self.peek() == "-":
            self.pos += 1
        if self.pos >= len(self.src) or not self.src[self.pos].isdigit():
            raise ValueError(
                f"show_when: 数字字面量非法，位置 {start}"
            )
        while self.pos < len(self.src) and self.src[self.pos].isdigit():
            self.pos += 1
        raw = self.src[start : self.pos]
        try:
            return int(raw)
        except ValueError as e:
            raise ValueError(f"show_when: 数字解析失败 {raw!r}: {e}") from e


# ---------------------------------------------------------------------------
# Compiler（AST → JSON Schema）
# ---------------------------------------------------------------------------


def _compile_node(n: ShowWhenNode) -> dict[str, Any]:
    """把 AST 递归编译为 JSON Schema ``if`` 子树。"""
    if n.kind == "cmp":
        if n.op == "==":
            inner: dict[str, Any] = {"const": n.literal}
        else:
            # !=
            inner = {"not": {"const": n.literal}}
        return {"properties": {n.field: inner}}
    if n.kind == "in":
        # 复制列表，避免共享引用
        return {
            "properties": {
                n.field: {"enum": list(n.literals)},
            }
        }
    if n.kind == "and":
        assert n.left is not None and n.right is not None
        return {
            "allOf": [_compile_node(n.left), _compile_node(n.right)],
        }
    if n.kind == "or":
        assert n.left is not None and n.right is not None
        return {
            "anyOf": [_compile_node(n.left), _compile_node(n.right)],
        }
    # 防御性（不应到达）
    return {}


def _build_ui_hidden_when(n: ShowWhenNode) -> dict[str, Any]:
    """从 AST 根节点构造 rjsf ui:hidden_when 对象。

    形态（对齐 Go buildUIHiddenWhen，供 TS 前端镜像复用）:
      - cmp:    ``{field, op, value, negate: False}``
      - in:     ``{field, op: "in", values, negate: False}``
      - and/or: ``{logical: {kind: "and"/"or", left, right}}``
    """
    if n.kind == "cmp":
        return {
            "field": n.field,
            "op": n.op,
            "value": n.literal,
            "negate": False,
        }
    if n.kind == "in":
        return {
            "field": n.field,
            "op": "in",
            "values": list(n.literals),
            "negate": False,
        }
    if n.kind in ("and", "or"):
        assert n.left is not None and n.right is not None
        return {
            "logical": {
                "kind": n.kind,
                "left": _build_ui_hidden_when(n.left),
                "right": _build_ui_hidden_when(n.right),
            },
        }
    return {}


# ---------------------------------------------------------------------------
# 顶层 API
# ---------------------------------------------------------------------------


def compile_show_when(
    expr: str, field_name: str
) -> tuple[dict[str, Any], dict[str, Any]]:
    """把 show_when DSL 表达式编译为 JSON Schema if/then/else + UI Schema ui:hidden_when。

    Args:
        expr: DSL 表达式字符串，如 ``"backend == 'github'"``。
        field_name: 受条件控制的字段名（用于 ``then.required`` 和 ``else.properties``）。

    Returns:
        一个二元组：

        * ``if_then_else``: ``{"if": {...}, "then": {"required": [field_name]},
          "else": {"properties": {field_name: False}}}``
          — 调用方（reflect.py）负责把它包进 ``allOf`` 加到 schema 上。
        * ``ui_hidden_when``: rjsf 自定义 widget 消费的 AST 副本；cmp / in / logical
          三种 shape。

    Raises:
        SyntaxError: 括号嵌套（programmer error）。
        ValueError: 跨 level 字段引用 / 算术运算 / 未闭合字符串 / 空 in 列表 /
            非法字面量 等可恢复错。
    """
    p = _Parser(expr)
    p.skip_whitespace()
    node = p.parse_or()
    p.skip_whitespace()
    if p.pos < len(p.src):
        rem = p.src[p.pos :]
        # 尾部若以算术运算符起始，分类为 "算术运算不支持"，与 parse_cmp 的
        # field 后算术检测保持一致。
        trimmed = rem.lstrip(" \t")
        if trimmed and trimmed[0] in ("+", "-", "*", "/"):
            raise ValueError(
                f"show_when: 算术运算不支持 (spec-v1 §3.3)，"
                f"位置 {p.pos} 遇到 {trimmed[:1]!r}"
            )
        raise ValueError(
            f"show_when: 表达式尾部存在未消费字符 (位置 {p.pos}, 剩余 {rem!r})"
        )

    if_schema = _compile_node(node)
    if_then_else: dict[str, Any] = {
        "if": if_schema,
        "then": {"required": [field_name]},
        "else": {"properties": {field_name: False}},
    }
    ui_hidden_when = _build_ui_hidden_when(node)
    return if_then_else, ui_hidden_when


__all__ = ["compile_show_when", "ShowWhenNode"]
