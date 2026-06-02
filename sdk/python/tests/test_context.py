"""测试 ks_app.context 模块。

覆盖：
- ContextVar 的设置 / 读取 / 重置
- None / 空 dict 等边界
- 全字段写入
- 非 string 值的强制 str 转换（与 Go SDK 行为对齐）
"""

from ks_app.context import (
    _ks_caller_id, _ks_caller_kind, _ks_chain_id,
    _reset_meta, _set_meta, get_context,
)


def test_set_and_get_meta():
    _set_meta({
        "ks_resource_scope": "instance_123",
        "ks_execution_id": "exec_456",
    })
    ctx = get_context()
    assert ctx.resource_scope == "instance_123"
    assert ctx.execution_id == "exec_456"
    _reset_meta()


def test_empty_meta():
    """空 dict 不应改变现有 ContextVar 值（且默认应为空字符串）。"""
    _reset_meta()
    _set_meta({})
    assert get_context().resource_scope == ""


def test_none_meta():
    """None 应被安全忽略，不抛错且不改变现有值。"""
    _reset_meta()
    _set_meta(None)
    assert get_context().resource_scope == ""


def test_all_fields():
    _reset_meta()
    _set_meta({
        "ks_resource_scope": "s",
        "ks_execution_id": "e",
        "ks_task_id": "t",
        "ks_task_name": "日报",
        "ks_trigger_type": "cron",
    })
    ctx = get_context()
    assert ctx.resource_scope == "s"
    assert ctx.execution_id == "e"
    assert ctx.task_id == "t"
    assert ctx.task_name == "日报"
    assert ctx.trigger_type == "cron"
    _reset_meta()


def test_non_string_coerced():
    """_meta 中非 string 值应被强制转成 str（镜像 Go SDK 的行为，防止 int/bool 穿透）。"""
    _reset_meta()
    _set_meta({"ks_resource_scope": 42})
    assert get_context().resource_scope == "42"
    _reset_meta()


def test_non_string_bool_coerced():
    """bool 值也走 str() 转换，结果是 'True' / 'False'。"""
    _reset_meta()
    _set_meta({"ks_trigger_type": True})
    assert get_context().trigger_type == "True"
    _reset_meta()


def test_dict_resource_scope_is_json_dumps():
    """dict 类型 resource_scope（Keystone 实际下发格式）必须通过 json.dumps 序列化，
    保证业务侧 json.loads 可还原为 dict。"""
    import json

    _reset_meta()
    _set_meta({"ks_resource_scope": {"template_ids": ["a", "b"], "allowed_local_roots": ["/tmp"]}})
    raw = get_context().resource_scope
    assert json.loads(raw) == {"template_ids": ["a", "b"], "allowed_local_roots": ["/tmp"]}
    _reset_meta()


def test_list_coerced_to_json():
    """list 类型也走 json.dumps（而非 Python repr）。"""
    import json

    _reset_meta()
    _set_meta({"ks_task_name": ["a", "b"]})
    raw = get_context().task_name
    assert json.loads(raw) == ["a", "b"]
    _reset_meta()


def test_non_ascii_in_dict_not_escaped():
    """中文字符不应被 json escape（ensure_ascii=False）。"""
    _reset_meta()
    _set_meta({"ks_resource_scope": {"name": "日报"}})
    raw = get_context().resource_scope
    assert "日报" in raw
    _reset_meta()


def test_reset_clears_all_fields():
    """_reset_meta 必须清空所有字段，避免泄漏到下一个请求。"""
    _set_meta({
        "ks_resource_scope": "s1",
        "ks_execution_id": "e1",
        "ks_task_id": "t1",
        "ks_task_name": "n1",
        "ks_trigger_type": "tt1",
    })
    _reset_meta()
    ctx = get_context()
    assert ctx.resource_scope == ""
    assert ctx.execution_id == ""
    assert ctx.task_id == ""
    assert ctx.task_name == ""
    assert ctx.trigger_type == ""


def test_default_empty_strings():
    """在未注入任何 _meta 的状态下，所有字段返回空字符串而非 None。"""
    _reset_meta()
    ctx = get_context()
    assert ctx.resource_scope == ""
    assert ctx.execution_id == ""
    assert ctx.task_id == ""
    assert ctx.task_name == ""
    assert ctx.trigger_type == ""
    # v0.4.0 新增审计字段
    assert ctx.agent_id == ""
    assert ctx.user_id == ""
    assert ctx.request_id == ""


def test_set_meta_reads_audit_fields():
    """v0.4.0：keystone 调 MCP 工具应用时通过 _meta 透传
    ks_agent_id / ks_user_id / ks_request_id 三个审计字段，SDK 必须读出。"""
    _reset_meta()
    _set_meta({
        "ks_agent_id": "agent_42",
        "ks_user_id": "user_7",
        "ks_request_id": "req_abc123",
    })
    ctx = get_context()
    assert ctx.agent_id == "agent_42"
    assert ctx.user_id == "user_7"
    assert ctx.request_id == "req_abc123"
    _reset_meta()


def test_audit_fields_coerce_non_string():
    """审计字段的非 string 值（如 int 型 user_id）应走 _coerce 转 str，
    与现有 5 个字段行为一致。"""
    _reset_meta()
    _set_meta({
        "ks_agent_id": 42,
        "ks_user_id": 7,
        "ks_request_id": True,
    })
    ctx = get_context()
    assert ctx.agent_id == "42"
    assert ctx.user_id == "7"
    assert ctx.request_id == "True"
    _reset_meta()


def test_reset_meta_clears_audit_fields():
    """_reset_meta 必须同时清空新增的 3 个审计字段，避免泄漏到下一个请求。"""
    _set_meta({
        "ks_agent_id": "a1",
        "ks_user_id": "u1",
        "ks_request_id": "r1",
    })
    _reset_meta()
    ctx = get_context()
    assert ctx.agent_id == ""
    assert ctx.user_id == ""
    assert ctx.request_id == ""


def test_tool_context_exposes_caller_fields():
    """复用降级：复用普通 @app.tool 时 handler 拿不到
    CapabilityContext，caller 上下文经 ToolContext 的 caller_id/caller_kind/chain_id
    暴露（读 _ks_caller_* ContextVar，由 dispatcher 通过 _meta.ks_* 透传）。"""
    _ks_caller_id.set("app-7")
    _ks_caller_kind.set("app")
    _ks_chain_id.set("chn_1")
    try:
        ctx = get_context()
        assert ctx.caller_id == "app-7"
        assert ctx.caller_kind == "app"
        assert ctx.chain_id == "chn_1"
    finally:
        _reset_meta()


def test_conversation_id_from_meta():
    from ks_app.context import _set_meta, _reset_meta, get_context
    _set_meta({"ks_conversation_id": 1183})  # 非 string，应被 coerce 成 "1183"
    try:
        assert get_context().conversation_id == "1183"
    finally:
        _reset_meta()
    assert get_context().conversation_id == ""
