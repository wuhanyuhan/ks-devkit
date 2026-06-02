"""Keystone 运行时上下文注入。

通过 contextvars 在 MCP tools/call 调用期间为 handler 注入运行时元信息
（resource_scope / execution_id 等），开发者通过 get_context() 非侵入式
获取，handler 函数签名无需变更。

设计要点：
- ContextVar 默认值统一为空字符串 ""，避免开发者写 None 检查
- _set_meta 将 _meta 中的非 string 值强制转成 string（MCP 协议允许任意 JSON 值，
  但 SDK 内部统一按 string 暴露）。转换规则：
    - str：原样保留
    - dict / list：json.dumps(v, ensure_ascii=False)，保证业务侧可 json.loads 还原
    - 其它（int / bool / None 等）：str(v)
  这与 Go SDK 的行为 **不同**：Go 用 type assertion `v.(string)` 对非 string 值
  静默 drop；Python 侧刻意宽松 —— 工具 handler 有时合法地使用 int/bool（数值型
  租户 ID）或 dict（resource_scope 等结构化字段），coerce 后的 string 形式仍可用。
  特别地：Keystone 把 resource_scope 作为 JSON 对象注入 _meta，Python 业务需要
  `json.loads(get_context().resource_scope)` 还原 dict，故 dict/list 必须用
  json.dumps 而非 str()（str(dict) 是 Python repr，不是合法 JSON）。
- _reset_meta 必须在 tools/call 的 finally 中调用，防止上下文泄漏到下一个请求
"""

import json
from contextvars import ContextVar


# 模块级 ContextVar，每个字段独立存放，未注入时返回空字符串。
_ks_resource_scope: ContextVar[str] = ContextVar("ks_resource_scope", default="")
_ks_execution_id: ContextVar[str] = ContextVar("ks_execution_id", default="")
_ks_task_id: ContextVar[str] = ContextVar("ks_task_id", default="")
_ks_task_name: ContextVar[str] = ContextVar("ks_task_name", default="")
_ks_trigger_type: ContextVar[str] = ContextVar("ks_trigger_type", default="")
# v0.4.0 新增审计字段：keystone 调 MCP 工具应用时通过 _meta 透传，
# 用于工具应用写审计日志 / 链路追踪。
_ks_agent_id: ContextVar[str] = ContextVar("ks_agent_id", default="")
_ks_user_id: ContextVar[str] = ContextVar("ks_user_id", default="")
_ks_request_id: ContextVar[str] = ContextVar("ks_request_id", default="")
# v0.6.0 Capability Mesh：dispatcher 调 capability backend.kind=mcp_tool 时通过
# _meta 透传 caller 身份 + chain trace；capability handler 经由 CapabilityContext 读。
_ks_caller_id: ContextVar[str] = ContextVar("ks_caller_id", default="")
_ks_caller_kind: ContextVar[str] = ContextVar("ks_caller_kind", default="")
_ks_chain_id: ContextVar[str] = ContextVar("ks_chain_id", default="")
# capability mesh 调用链快照（mcp_tool 复用降级路径经 ToolContext 透传，
# 注入到被复用 tool wrap 出的下游调用 _meta.ks_chain_snapshot）。
_ks_chain_snapshot: ContextVar[str] = ContextVar("ks_chain_snapshot", default="")
# keystone 会话 ID：caller context，下游据此把决策门 / 交付物等回流到正确的 keystone 会话。
_ks_conversation_id: ContextVar[str] = ContextVar("ks_conversation_id", default="")


class ToolContext:
    """tools/call 调用期间可获取的 Keystone 运行时上下文。

    所有字段均为只读 string；当 MCP 客户端未注入对应 _meta 字段时，对应属性
    返回空字符串而非 None，以简化 handler 端的判空逻辑。
    """

    @property
    def resource_scope(self) -> str:
        """资源作用域。多租户隔离的关键字段，不同实例调用同一工具时通过它区分数据边界。"""
        return _ks_resource_scope.get()

    @property
    def execution_id(self) -> str:
        """当前执行 ID。"""
        return _ks_execution_id.get()

    @property
    def task_id(self) -> str:
        """当前任务 ID。"""
        return _ks_task_id.get()

    @property
    def task_name(self) -> str:
        """当前任务名称。"""
        return _ks_task_name.get()

    @property
    def trigger_type(self) -> str:
        """触发类型（manual / cron / webhook / event 等）。"""
        return _ks_trigger_type.get()

    @property
    def agent_id(self) -> str:
        """当前 keystone agent ID（v0.4.0 起 MCP 调用透传，用于审计）。"""
        return _ks_agent_id.get()

    @property
    def user_id(self) -> str:
        """当前 keystone user ID（v0.4.0 起 MCP 调用透传，用于审计）。"""
        return _ks_user_id.get()

    @property
    def request_id(self) -> str:
        """当前 keystone 请求 ID（v0.4.0 起 MCP 调用透传，用于审计 / 链路追踪）。"""
        return _ks_request_id.get()

    @property
    def caller_id(self) -> str:
        """capability mesh 调用方 ID（承载 wire ks_caller_id）。

        复用降级：复用普通 @app.tool 作为 mcp_tool capability
        backend 时，handler 拿不到 CapabilityContext，经此暴露 caller 上下文。
        """
        return _ks_caller_id.get()

    @property
    def caller_kind(self) -> str:
        """capability mesh 调用方类型（app / user 等，承载 wire ks_caller_kind）。"""
        return _ks_caller_kind.get()

    @property
    def chain_id(self) -> str:
        """capability mesh 调用链 ID（承载 wire ks_chain_id）。"""
        return _ks_chain_id.get()

    @property
    def conversation_id(self) -> str:
        """keystone 会话 ID（承载 wire ks_conversation_id）。

        下游据此把决策门 / 交付物等回流到正确的 keystone 会话。
        """
        return _ks_conversation_id.get()


def get_context() -> ToolContext:
    """获取当前 tools/call 调用的 Keystone 运行时上下文。

    在 handler 函数体内调用，返回的 ToolContext 反映 MCP 请求 _meta 字段
    中注入的值。在非 tools/call 路径下调用，所有字段均为空字符串。
    """
    return ToolContext()


def _coerce(value) -> str:
    """把任意 JSON 值 coerce 成 string。

    - str：原样
    - dict / list：json.dumps(ensure_ascii=False)，便于业务 json.loads 还原结构化数据
    - 其它（int / bool / None 等）：str(value)
    """
    if isinstance(value, str):
        return value
    if isinstance(value, (dict, list)):
        return json.dumps(value, ensure_ascii=False)
    return str(value)


def _set_meta(meta: dict | None) -> None:
    """将 MCP _meta 字段写入 ContextVars。

    None 或空 dict 时为 no-op；对每个字段通过 _coerce() 转成 string，
    使 int/bool/None/dict/list 等非 string 值也能以 string 形式暴露给 handler。

    与 Go SDK 的差异：Go `withMeta` 用 type assertion `v.(string)` 对非 string
    值静默 drop（完全忽略该字段），Python 则 coerce。这是刻意的分歧 —— Python 侧
    更宽松，保留 int/bool 租户 ID、dict resource_scope 等合法场景的可用性。
    """
    if not meta:
        return
    if "ks_resource_scope" in meta:
        _ks_resource_scope.set(_coerce(meta["ks_resource_scope"]))
    if "ks_execution_id" in meta:
        _ks_execution_id.set(_coerce(meta["ks_execution_id"]))
    if "ks_task_id" in meta:
        _ks_task_id.set(_coerce(meta["ks_task_id"]))
    if "ks_task_name" in meta:
        _ks_task_name.set(_coerce(meta["ks_task_name"]))
    if "ks_trigger_type" in meta:
        _ks_trigger_type.set(_coerce(meta["ks_trigger_type"]))
    if "ks_agent_id" in meta:
        _ks_agent_id.set(_coerce(meta["ks_agent_id"]))
    if "ks_user_id" in meta:
        _ks_user_id.set(_coerce(meta["ks_user_id"]))
    if "ks_request_id" in meta:
        _ks_request_id.set(_coerce(meta["ks_request_id"]))
    if "ks_caller_id" in meta:
        _ks_caller_id.set(_coerce(meta["ks_caller_id"]))
    if "ks_caller_kind" in meta:
        _ks_caller_kind.set(_coerce(meta["ks_caller_kind"]))
    if "ks_chain_id" in meta:
        _ks_chain_id.set(_coerce(meta["ks_chain_id"]))
    if "ks_chain_snapshot" in meta:
        _ks_chain_snapshot.set(_coerce(meta["ks_chain_snapshot"]))
    if "ks_conversation_id" in meta:
        _ks_conversation_id.set(_coerce(meta["ks_conversation_id"]))


def _reset_meta() -> None:
    """清空所有 ContextVar，必须在 tools/call 的 finally 中调用。

    避免 handler 异常或正常返回后遗留的上下文被下一个请求读取（在共享
    event loop 的 ASGI 服务下，ContextVar 不会自动重置）。
    """
    _ks_resource_scope.set("")
    _ks_execution_id.set("")
    _ks_task_id.set("")
    _ks_task_name.set("")
    _ks_trigger_type.set("")
    _ks_agent_id.set("")
    _ks_user_id.set("")
    _ks_request_id.set("")
    _ks_caller_id.set("")
    _ks_caller_kind.set("")
    _ks_chain_id.set("")
    _ks_chain_snapshot.set("")
    _ks_conversation_id.set("")
