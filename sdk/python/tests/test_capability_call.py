"""caller-side: app.call_capability(name).invoke() / .submit() + Task 对象单测。"""
from unittest.mock import AsyncMock

import pytest

from ks_app import App
from ks_app.errors import BackendError, Cancelled
from ks_app.keystone_client.dispatcher_client import (
    InvokeAsyncResult,
    InvokeSyncResult,
    TaskSnapshot,
)
from ks_app.task import Task


@pytest.fixture
def app(tmp_path):
    """构造一个 App，注入 mock DispatcherClient。"""
    a = App("ks-mcp-caller", manifest_path=str(tmp_path / "missing.yaml"))
    a._dispatcher_client = AsyncMock()
    return a


@pytest.mark.asyncio
async def test_call_capability_invoke_returns_result(app):
    app._dispatcher_client.invoke.return_value = InvokeSyncResult(
        result={"article": "hi"}, duration_ms=100,
    )
    result = await app.call_capability("ks-mcp-writer.create_article").invoke(topic="AI")
    assert result == {"article": "hi"}
    app._dispatcher_client.invoke.assert_awaited_once_with(
        capability="ks-mcp-writer.create_article",
        args={"topic": "AI"},
        mode="sync",
        idempotency_key=None,
        timeout_ms_override=None,
        on_behalf_of_user_id=None,
        chain_id=None,
        chain_header=None,
    )


@pytest.mark.asyncio
async def test_call_capability_submit_returns_task(app):
    app._dispatcher_client.invoke.return_value = InvokeAsyncResult(
        task_id="task-1", status="pending", submitted_at="", timeout_at="",
    )
    task = await app.call_capability("ks-mcp-image-gen.generate").submit(prompt="x")
    assert isinstance(task, Task)
    assert task.task_id == "task-1"
    assert task.status == "pending"
    assert task.canonical_name == "ks-mcp-image-gen.generate"


@pytest.mark.asyncio
async def test_task_refresh_updates_status(app):
    app._dispatcher_client.get_task.return_value = TaskSnapshot(
        task_id="task-1", status="running",
        canonical_name="ks.x", percent=50, stage_message="halfway",
    )
    task = Task(
        task_id="task-1", status="pending",
        canonical_name="ks.x", dispatcher_client=app._dispatcher_client,
    )
    await task.refresh()
    assert task.status == "running"
    assert task.percent == 50
    assert task.stage_message == "halfway"


@pytest.mark.asyncio
async def test_task_cancel_calls_dispatcher(app):
    task = Task(
        task_id="task-1", status="pending",
        canonical_name="ks.x", dispatcher_client=app._dispatcher_client,
    )
    await task.cancel()
    app._dispatcher_client.cancel_task.assert_awaited_once_with("task-1")


@pytest.mark.asyncio
async def test_task_result_returns_when_done(app):
    """task 状态 done 时，result() 返 result payload。"""
    app._dispatcher_client.get_task.return_value = TaskSnapshot(
        task_id="task-1", status="done",
        result={"image_url": "https://..."},
    )
    task = Task(
        task_id="task-1", status="pending",
        canonical_name="ks.x", dispatcher_client=app._dispatcher_client,
    )
    result = await task.result(poll_interval_s=0)
    assert result == {"image_url": "https://..."}


@pytest.mark.asyncio
async def test_task_result_raises_backend_error_on_failed(app):
    app._dispatcher_client.get_task.return_value = TaskSnapshot(
        task_id="task-1", status="failed",
        error_code="50200", error_message="image gen exploded",
    )
    task = Task(
        task_id="task-1", status="pending",
        canonical_name="ks.x", dispatcher_client=app._dispatcher_client,
    )
    with pytest.raises(BackendError, match="exploded"):
        await task.result(poll_interval_s=0)


@pytest.mark.asyncio
async def test_task_result_raises_cancelled_on_cancelled(app):
    app._dispatcher_client.get_task.return_value = TaskSnapshot(
        task_id="task-1", status="cancelled",
    )
    task = Task(
        task_id="task-1", status="pending",
        canonical_name="ks.x", dispatcher_client=app._dispatcher_client,
    )
    with pytest.raises(Cancelled):
        await task.result(poll_interval_s=0)


@pytest.mark.asyncio
async def test_invoke_without_dispatcher_client_raises(tmp_path, monkeypatch):
    """没配 KS_APP_TOKEN / KS_GATEWAY_URL 时 caller-side 不可用。"""
    monkeypatch.delenv("KS_APP_TOKEN", raising=False)
    monkeypatch.delenv("KS_GATEWAY_URL", raising=False)
    a = App("ks-mcp-caller", manifest_path=str(tmp_path / "missing.yaml"))
    with pytest.raises(RuntimeError, match="KS_APP_TOKEN"):
        await a.call_capability("foo.bar").invoke(x=1)


@pytest.mark.asyncio
async def test_call_capability_invoke_passes_on_behalf_of_user_id(app):
    app._dispatcher_client.invoke.return_value = InvokeSyncResult(result={"ok": True}, duration_ms=1)
    await app.call_capability("ks.test").invoke(x=1, on_behalf_of_user_id=42)
    app._dispatcher_client.invoke.assert_awaited_once_with(
        capability="ks.test", args={"x": 1}, mode="sync",
        idempotency_key=None, timeout_ms_override=None, on_behalf_of_user_id=42,
        chain_id=None, chain_header=None,
    )


@pytest.mark.asyncio
async def test_call_capability_submit_passes_on_behalf_of_user_id(app):
    app._dispatcher_client.invoke.return_value = InvokeAsyncResult(
        task_id="t1", status="pending", submitted_at="", timeout_at="",
    )
    await app.call_capability("ks.test").submit(x=1, on_behalf_of_user_id=7)
    app._dispatcher_client.invoke.assert_awaited_once_with(
        capability="ks.test", args={"x": 1}, mode="async",
        idempotency_key=None, timeout_ms_override=None, on_behalf_of_user_id=7,
        chain_id=None, chain_header=None,
    )


@pytest.mark.asyncio
async def test_call_capability_invoke_default_on_behalf_of_none(app):
    app._dispatcher_client.invoke.return_value = InvokeSyncResult(result={}, duration_ms=1)
    await app.call_capability("ks.test").invoke(x=1)
    app._dispatcher_client.invoke.assert_awaited_once_with(
        capability="ks.test", args={"x": 1}, mode="sync",
        idempotency_key=None, timeout_ms_override=None, on_behalf_of_user_id=None,
        chain_id=None, chain_header=None,
    )


def test_call_capability_keeps_full_name():
    """锁调用方不对称契约：caller 侧 call_capability 传全名，绝不被去前缀派生波及。"""
    app = App("ks-mcp-x")
    app._dispatcher_client = object()  # 占位，非 None 即可构造 CapabilityCall
    cc = app.call_capability("ks-mcp-other.generate")
    assert cc.canonical_name == "ks-mcp-other.generate"


@pytest.mark.asyncio
async def test_call_capability_invoke_passes_chain(app):
    """caller 侧 invoke 支持 chain_id/chain_header 透传到 dispatcher（多跳调用链穿透）。"""
    app._dispatcher_client.invoke.return_value = InvokeSyncResult(result={}, duration_ms=1)
    await app.call_capability("ks.test").invoke(x=1, chain_id="chn_1", chain_header="hdr")
    app._dispatcher_client.invoke.assert_awaited_once_with(
        capability="ks.test", args={"x": 1}, mode="sync",
        idempotency_key=None, timeout_ms_override=None, on_behalf_of_user_id=None,
        chain_id="chn_1", chain_header="hdr",
    )
