"""CapabilityContext 字段访问与 progress 上报单测。"""
from unittest.mock import AsyncMock

import pytest

from ks_app.capability_context import CapabilityContext, build_context_from_meta


def test_build_context_from_meta_mcp_path():
    """mcp_tool backend 路径下，CapabilityContext 从 _meta.ks_* 字段构建。"""
    meta = {
        "ks_user_id": "1234",
        "ks_caller_kind": "app",
        "ks_caller_id": "ks-mcp-writer",
        "ks_chain_id": "chain-abc",
        "ks_task_id": "task-xyz",
        "ks_request_id": "req-111",
    }
    ctx = build_context_from_meta(
        meta=meta,
        canonical_name="ks-mcp-image-gen.generate",
        timeout_ms=300000,
    )
    assert ctx.user_id == "1234"
    assert ctx.caller_id == "ks-mcp-writer"
    assert ctx.caller_kind == "app"
    assert ctx.chain_id == "chain-abc"
    assert ctx.task_id == "task-xyz"
    assert ctx.request_id == "req-111"
    assert ctx.canonical_name == "ks-mcp-image-gen.generate"


def test_build_context_from_meta_missing_fields_defaults_empty():
    ctx = build_context_from_meta(
        meta={},
        canonical_name="foo.bar",
        timeout_ms=0,
    )
    assert ctx.user_id == ""
    assert ctx.caller_id == ""
    assert ctx.task_id == ""


@pytest.mark.asyncio
async def test_progress_calls_dispatcher_client():
    """ctx.progress() 应调 DispatcherClient.report_progress（仅 task_id 非空时）。"""
    fake_client = AsyncMock()
    ctx = CapabilityContext(
        user_id="u", caller_id="a", caller_kind="app",
        chain_id="c", task_id="task-1", request_id="r",
        canonical_name="foo.bar", timeout_ms=300000,
        dispatcher_client=fake_client,
    )
    await ctx.progress("正在搜索...", percent=10)
    fake_client.report_progress.assert_awaited_once_with(
        task_id="task-1", stage="正在搜索...", percent=10,
    )


@pytest.mark.asyncio
async def test_progress_noop_when_task_id_empty():
    """sync 调用没 task_id，progress 是 no-op（不抛错）。"""
    fake_client = AsyncMock()
    ctx = CapabilityContext(
        user_id="u", caller_id="a", caller_kind="app",
        chain_id="c", task_id="", request_id="r",
        canonical_name="foo.bar", timeout_ms=30000,
        dispatcher_client=fake_client,
    )
    await ctx.progress("ignored", percent=10)
    fake_client.report_progress.assert_not_called()


def test_deadline_computed_from_timeout_ms():
    import time
    ctx = CapabilityContext(
        user_id="u", caller_id="a", caller_kind="app",
        chain_id="c", task_id="t", request_id="r",
        canonical_name="foo.bar", timeout_ms=30000,
        dispatcher_client=None,
        started_at_ms=int(time.time() * 1000),
    )
    deadline = ctx.deadline()
    assert deadline - ctx.started_at_ms == 30000


def test_cancelled_default_false():
    ctx = CapabilityContext(
        user_id="u", caller_id="a", caller_kind="app",
        chain_id="c", task_id="t", request_id="r",
        canonical_name="foo.bar", timeout_ms=30000,
        dispatcher_client=None,
    )
    assert ctx.cancelled() is False
    ctx._set_cancelled()
    assert ctx.cancelled() is True


def test_deadline_zero_when_no_timeout():
    """timeout_ms=0 表示无超时，deadline() 返 0。"""
    ctx = CapabilityContext(
        user_id="u", caller_id="a", caller_kind="app",
        chain_id="c", task_id="t", request_id="r",
        canonical_name="foo.bar", timeout_ms=0,
        dispatcher_client=None,
    )
    assert ctx.deadline() == 0


@pytest.mark.asyncio
async def test_progress_swallows_dispatcher_error():
    """progress 上报失败 best-effort，不应 raise 出去打挂业务 handler。"""
    fake_client = AsyncMock()
    fake_client.report_progress.side_effect = RuntimeError("dispatcher down")
    ctx = CapabilityContext(
        user_id="u", caller_id="a", caller_kind="app",
        chain_id="c", task_id="task-1", request_id="r",
        canonical_name="foo.bar", timeout_ms=30000,
        dispatcher_client=fake_client,
    )
    # 不抛
    await ctx.progress("stage", percent=10)


def test_capability_context_caller_id_naming():
    """caller_id 正名：CapabilityContext 承载 wire ks_caller_id 的字段
    从 app_id 正名 caller_id；旧 app_id 字段不再存在。"""
    ctx = build_context_from_meta(
        meta={"ks_caller_id": "app-7"},
        canonical_name="ks-mcp-x.foo",
        timeout_ms=0,
    )
    assert ctx.caller_id == "app-7"
    assert not hasattr(ctx, "app_id")


def test_capability_context_chain_header_from_meta():
    """形态对齐：CapabilityContext 加 chain_header 字段，
    mcp_tool 路径从 _meta.ks_chain_snapshot 读取（对齐 Go ChainHeader()）。"""
    ctx = build_context_from_meta(
        meta={"ks_chain_id": "chn_1", "ks_chain_snapshot": "eyJjaGFpbiI6IDF9"},
        canonical_name="ks-mcp-x.foo",
        timeout_ms=0,
    )
    assert ctx.chain_id == "chn_1"
    assert ctx.chain_header == "eyJjaGFpbiI6IDF9"
