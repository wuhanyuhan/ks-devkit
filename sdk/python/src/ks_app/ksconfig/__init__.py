"""ksconfig 子包：pydantic BaseModel → JSON Schema + UI Schema 反射 + show_when DSL 编译。

  - ``reflect_config_schema``: pydantic Schema 反射
  - ``compile_show_when``: show_when DSL → JSON Schema if/then/else
"""
from .reflect import reflect_config_schema
from .show_when import compile_show_when

__all__ = ["reflect_config_schema", "compile_show_when"]
