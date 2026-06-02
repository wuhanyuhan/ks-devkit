"""pydantic BaseModel → JSON Schema + UI Schema 反射。

镜像 Go ksapp/ksconfig/reflect.go。

与 Go 的偏差：
  - Go 用 struct tag `ksconfig:"required,type:password,label:xxx"` 表达约束 / UI hint；
    Python 用 pydantic `Field(..., title=..., ge=..., max_length=..., json_schema_extra={...})`
    表达，覆盖 Go tag 全部能力（见 task 说明映射表）。
  - 底层通过 pydantic v2 `Model.model_json_schema(mode='serialization')` 拿 JSON Schema，
    再手动遍历 `model_fields` 提取 `json_schema_extra` 合成 UI Schema。
  - Schema 只保留协议约定的子集字段：`type` / `properties` / `required` 顶层；
    字段内保留 `type` / `title` / `minimum` / `maximum` / `minLength` / `maxLength`
    / `pattern` / `enum` / `default`。`$defs` / `$schema` 等冗余字段过滤。
"""
from __future__ import annotations

from typing import Any, get_args, get_origin

from pydantic import BaseModel

# 字段级允许的 JSON Schema keys（协议约定子集）；pydantic 输出里其它字段会被过滤。
_ALLOWED_FIELD_KEYS = {
    "type",
    "title",
    "description",
    "minimum",
    "maximum",
    "exclusiveMinimum",
    "exclusiveMaximum",
    "minLength",
    "maxLength",
    "pattern",
    "enum",
    "default",
    "items",
    "properties",
    "required",
    "format",
    # list / 数值约束补充
    "minItems",
    "maxItems",
    "uniqueItems",
    "multipleOf",
    "additionalProperties",
}

# MVP 不支持的复合 schema 结构：顶层出现即早暴露为 NotImplementedError。
# 嵌套 BaseModel / Union / Optional 等结构的支持范围与 Go 实现收敛一致。
_UNSUPPORTED_FIELD_STRUCT_KEYS = ("$ref", "anyOf", "oneOf", "allOf")


def reflect_config_schema(
    model_cls: type[BaseModel],
) -> tuple[dict[str, Any], dict[str, Any]]:
    """反射 pydantic BaseModel 子类，生成 (JSON Schema, UI Schema)。

    Args:
        model_cls: pydantic BaseModel 子类

    Returns:
        (schema, ui_schema) — schema 是协议约定的极简 JSON Schema；
        ui_schema 的 key 是字段名，value 是 `{"ui:widget": ..., "ui:label": ..., ...}`。

    Raises:
        TypeError: `model_cls` 不是 BaseModel 子类
    """
    if not (isinstance(model_cls, type) and issubclass(model_cls, BaseModel)):
        raise TypeError(
            f"reflect_config_schema: model_cls 必须是 pydantic BaseModel 子类，"
            f"收到 {model_cls!r}"
        )

    raw_schema = model_cls.model_json_schema(mode="serialization")
    props_raw = raw_schema.get("properties", {}) or {}
    required_list = list(raw_schema.get("required", []) or [])

    cleaned_props: dict[str, Any] = {}
    ui_schema: dict[str, Any] = {}

    for field_name, field_info in model_cls.model_fields.items():
        # 字段名（pydantic v2 默认用字段标识符；别名场景 MVP 暂不处理）
        prop_raw = props_raw.get(field_name, {})
        if not prop_raw:
            continue

        # MVP 拒绝嵌套 BaseModel / Union / Optional / allOf 等复合结构，
        # 在反射边界立即暴露为 NotImplementedError，避免静默 reflected 为 `{}`。
        _assert_field_struct_supported(field_name, prop_raw)

        cleaned, extra_ui = _clean_field_schema(prop_raw, field_info.json_schema_extra)
        cleaned_props[field_name] = cleaned

        # 单独提取 UI 相关 key
        field_ui = _build_field_ui(field_info, extra_ui, cleaned)
        if field_ui:
            ui_schema[field_name] = field_ui

    schema: dict[str, Any] = {
        "type": "object",
        "properties": cleaned_props,
    }
    if required_list:
        schema["required"] = required_list
    return schema, ui_schema


def _clean_field_schema(
    prop_raw: dict[str, Any],
    json_schema_extra: Any,
) -> tuple[dict[str, Any], dict[str, Any]]:
    """从 pydantic 输出的字段 schema 中过滤出协议允许的 keys。

    返回 (cleaned_schema, extra_ui) — extra_ui 是从 json_schema_extra 里摘出来的
    `ui:xxx` / `ks:xxx` 键值对（已剥离，用于 UI Schema 构造）。
    """
    cleaned: dict[str, Any] = {}
    for k, v in prop_raw.items():
        if k in _ALLOWED_FIELD_KEYS:
            cleaned[k] = v

    # pydantic 会把 json_schema_extra 原样 merge 进 property schema；
    # 其中 `ui:xxx` / `ks:xxx` 键不是合法 JSON Schema，要摘出来给 UI Schema，
    # 同时从 cleaned 中剔除。
    extra_ui: dict[str, Any] = {}
    if isinstance(json_schema_extra, dict):
        for k, v in json_schema_extra.items():
            if k.startswith("ui:") or k.startswith("ks:"):
                extra_ui[k] = v
                cleaned.pop(k, None)

    # pydantic 对 Literal["a", "b"] 会生成 `{"enum": [...], "type": "string"}`；
    # 但对于简单 `enum` + 无 items / 复合项，确保保留即可。
    return cleaned, extra_ui


def _build_field_ui(
    field_info: Any,
    extra_ui: dict[str, Any],
    cleaned_schema: dict[str, Any],
) -> dict[str, Any]:
    """从 Field(title=...) + json_schema_extra 构造 UI Schema 单字段条目。"""
    ui: dict[str, Any] = {}

    # 1. `title` → `ui:label`（与 Go `label:xxx` → `ui:label` 对齐）
    title = getattr(field_info, "title", None)
    if title:
        ui["ui:label"] = title

    # 2. `description` → `ui:help`（当没显式 ui:help 时才补）
    #   Go 用独立 `hint:xxx` tag，这里 description 作备选。
    description = getattr(field_info, "description", None)
    if description and "ui:help" not in extra_ui:
        ui["ui:help"] = description

    # 3. 从 json_schema_extra 摘出的 ui:xxx / ks:xxx 直接 merge
    for k, v in extra_ui.items():
        ui[k] = v

    # 4. enum 字段自动推断 `ui:widget=select`（Go 同样行为），
    #   若用户已显式设 ui:widget 则不覆盖。
    if "enum" in cleaned_schema and "ui:widget" not in ui:
        ui["ui:widget"] = "select"

    return ui


def _assert_field_struct_supported(field_name: str, prop_raw: dict[str, Any]) -> None:
    """检测字段 schema 里是否含 MVP 暂不支持的结构；命中则 NotImplementedError。

    拦截场景：
      - 嵌套 BaseModel → pydantic 生成 `{"$ref": "#/$defs/..."}`
      - Optional[T] / Union[...] → pydantic 生成 `{"anyOf": [...]}`
      - 其它 oneOf / allOf → 同上
      - list[BaseModel] / list[Union[...]] → items 内含 `$ref` / `anyOf`

    对齐 Go 实现的 MVP 范围：Python 端只支持顶层扁平字段
    （string / int / bool / float / Literal / list[str|int|bool|Literal[...]]）。
    """
    # 直接命中顶层结构 key
    for key in _UNSUPPORTED_FIELD_STRUCT_KEYS:
        if key in prop_raw:
            raise NotImplementedError(
                f"字段 {field_name!r} 使用了 MVP 暂不支持的 schema 结构（{key} — "
                f"常见于嵌套 BaseModel / Optional / Union）。Spec A MVP 仅支持顶层"
                f"字段（string/int/bool/float/Literal/list[str|int|bool]）。"
            )
    # list 场景：允许 list[原始类型]；items 内含 $ref / anyOf / oneOf / allOf 则拒绝
    items = prop_raw.get("items")
    if isinstance(items, dict):
        for key in _UNSUPPORTED_FIELD_STRUCT_KEYS:
            if key in items:
                raise NotImplementedError(
                    f"字段 {field_name!r} 的 list items 使用了 MVP 暂不支持的结构"
                    f"（items.{key} — 常见于 list[BaseModel] / list[Union]）。"
                    f"Spec A MVP 仅支持 list[str|int|bool|float|Literal[...]]。"
                )
