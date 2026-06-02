"""ksconfig/reflect.py 反射测试（镜像 Go ksconfig/reflect_test.go）。"""
from __future__ import annotations

from typing import Literal, Optional

import pytest
from pydantic import BaseModel, Field

from ks_app.ksconfig import reflect_config_schema


class BasicConfig(BaseModel):
    """覆盖 string / int / bool 基础类型 + required / optional 混合。"""
    api_key: str = Field(..., title="API Key")
    region: str = Field("cn", title="区域")
    max_retries: int = Field(3, title="最大重试次数")
    enable_cache: bool = Field(True, title="启用缓存")


def test_reflect_basic():
    schema, ui_schema = reflect_config_schema(BasicConfig)
    assert schema["type"] == "object"
    props = schema["properties"]
    assert "api_key" in props and props["api_key"]["type"] == "string"
    assert "region" in props and props["region"]["type"] == "string"
    assert "max_retries" in props and props["max_retries"]["type"] == "integer"
    assert "enable_cache" in props and props["enable_cache"]["type"] == "boolean"
    # required 只含 api_key（无默认值）
    required = schema.get("required", [])
    assert required == ["api_key"]


class PasswordConfig(BaseModel):
    api_key: str = Field(
        ...,
        title="API Key",
        json_schema_extra={"ui:widget": "password", "ks:sensitive": True},
    )


def test_reflect_ui_widget_password():
    _, ui_schema = reflect_config_schema(PasswordConfig)
    assert ui_schema["api_key"]["ui:widget"] == "password"


class LabelConfig(BaseModel):
    name: str = Field(..., title="显示名")


def test_reflect_ui_label_from_title():
    schema, ui_schema = reflect_config_schema(LabelConfig)
    # title 保留在 schema.properties.name.title（方案 A）
    assert schema["properties"]["name"]["title"] == "显示名"
    # ui_schema 也镜像 ui:label（rjsf 可消费）
    assert ui_schema["name"]["ui:label"] == "显示名"


class HelpConfig(BaseModel):
    api_key: str = Field(
        ...,
        json_schema_extra={"ui:help": "从控制台获取"},
    )


def test_reflect_ui_help():
    _, ui_schema = reflect_config_schema(HelpConfig)
    assert ui_schema["api_key"]["ui:help"] == "从控制台获取"


class SensitiveConfig(BaseModel):
    secret: str = Field(..., json_schema_extra={"ks:sensitive": True})


def test_reflect_ks_sensitive():
    _, ui_schema = reflect_config_schema(SensitiveConfig)
    assert ui_schema["secret"]["ks:sensitive"] is True


class EnumConfig(BaseModel):
    region: Literal["cn", "us", "eu"] = Field("cn", title="区域")


def test_reflect_enum():
    schema, ui_schema = reflect_config_schema(EnumConfig)
    region = schema["properties"]["region"]
    # enum 出现，选项齐全
    assert "enum" in region
    assert set(region["enum"]) == {"cn", "us", "eu"}
    # UI 自动推断为 select
    assert ui_schema["region"]["ui:widget"] == "select"


class NumberRangeConfig(BaseModel):
    retries: int = Field(3, ge=1, le=10)


def test_reflect_number_range():
    schema, _ = reflect_config_schema(NumberRangeConfig)
    retries = schema["properties"]["retries"]
    assert retries["minimum"] == 1
    assert retries["maximum"] == 10


class StringLengthConfig(BaseModel):
    name: str = Field(..., min_length=1, max_length=100)


def test_reflect_string_length():
    schema, _ = reflect_config_schema(StringLengthConfig)
    name = schema["properties"]["name"]
    assert name["minLength"] == 1
    assert name["maxLength"] == 100


class RequiredConfig(BaseModel):
    api_key: str = Field(...)  # required
    region: str = Field("cn")  # optional (has default)


def test_reflect_required():
    schema, _ = reflect_config_schema(RequiredConfig)
    required = schema.get("required", [])
    assert "api_key" in required
    assert "region" not in required


def test_reflect_not_basemodel_raises():
    class NotAModel:
        pass
    with pytest.raises(TypeError):
        reflect_config_schema(NotAModel)


class PatternConfig(BaseModel):
    code: str = Field(..., pattern=r"^[A-Z]{3}$")


def test_reflect_pattern():
    schema, _ = reflect_config_schema(PatternConfig)
    code = schema["properties"]["code"]
    assert code["pattern"] == r"^[A-Z]{3}$"


class DefaultConfig(BaseModel):
    region: str = Field("cn")
    retries: int = Field(3)
    enabled: bool = Field(True)


def test_reflect_default_values():
    schema, _ = reflect_config_schema(DefaultConfig)
    props = schema["properties"]
    assert props["region"]["default"] == "cn"
    assert props["retries"]["default"] == 3
    assert props["enabled"]["default"] is True


# --- 嵌套 BaseModel 早暴露（MVP 不支持，明确报错优于静默丢失） ---


class _InnerModel(BaseModel):
    name: str = Field(...)


class NestedBaseModelConfig(BaseModel):
    """嵌套 BaseModel：pydantic 会生成 `{"$ref": "#/$defs/_InnerModel"}`。"""
    inner: _InnerModel = Field(...)


def test_reflect_nested_basemodel_raises():
    with pytest.raises(NotImplementedError, match="inner"):
        reflect_config_schema(NestedBaseModelConfig)


class ListOfBaseModelConfig(BaseModel):
    """list[BaseModel]：items 内含 $ref，也应被拒绝。"""
    inners: list[_InnerModel] = Field(default_factory=list)


def test_reflect_list_of_basemodel_raises():
    with pytest.raises(NotImplementedError, match="inners"):
        reflect_config_schema(ListOfBaseModelConfig)


# --- Optional[T] 早暴露（pydantic 生成 anyOf，会丢 type） ---


class OptionalFieldConfig(BaseModel):
    """Optional[str]：pydantic 生成 `{"anyOf": [{"type":"string"}, {"type":"null"}]}`。"""
    note: Optional[str] = None


def test_reflect_optional_raises():
    with pytest.raises(NotImplementedError, match="note"):
        reflect_config_schema(OptionalFieldConfig)


# --- list 约束 minItems / maxItems 纳入白名单 ---


class ListRangeConfig(BaseModel):
    tags: list[str] = Field(default_factory=list, min_length=1, max_length=10)


def test_reflect_list_min_max_items():
    schema, _ = reflect_config_schema(ListRangeConfig)
    tags = schema["properties"]["tags"]
    assert tags["type"] == "array"
    assert tags["minItems"] == 1
    assert tags["maxItems"] == 10
    # items 必须是原始类型（list[str] → {"type": "string"}），不会被 C1 拦截
    assert tags["items"]["type"] == "string"
