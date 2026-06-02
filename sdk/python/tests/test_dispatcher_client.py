"""DispatcherClient：POST /v1/apps/self/invoke + progress 上报 + 任务查询单测。"""
import json

import httpx
import pytest
import respx

from ks_app.errors import (
    BackendError,
    CapabilityForbidden,
    CapabilityNotFound,
    CapabilityUnavailable,
    RateLimitError,
    TaskNotFound,
    Timeout,
)
from ks_app.keystone_client.dispatcher_client import (
    DispatcherClient,
    InvokeAsyncResult,
    InvokeSyncResult,
    TaskSnapshot,
)

GATEWAY = "https://ks.example.com"
TOKEN = "ks-app-token-xxx"


@pytest.fixture
def client():
    return DispatcherClient(gateway_url=GATEWAY, app_token=TOKEN, timeout=2.0)


@pytest.mark.asyncio
@respx.mock
async def test_invoke_sync_returns_result(client):
    respx.post(f"{GATEWAY}/v1/apps/self/invoke").mock(
        return_value=httpx.Response(200, json={
            "code": 0, "message": "",
            "data": {
                "result": {"article": "hello"},
                "duration_ms": 123,
            },
        })
    )
    result = await client.invoke(
        capability="ks-mcp-writer.create_article",
        args={"topic": "AI"},
        mode="sync",
    )
    assert isinstance(result, InvokeSyncResult)
    assert result.result == {"article": "hello"}
    assert result.duration_ms == 123


@pytest.mark.asyncio
@respx.mock
async def test_invoke_async_returns_task_snapshot(client):
    respx.post(f"{GATEWAY}/v1/apps/self/invoke").mock(
        return_value=httpx.Response(200, json={
            "code": 0, "message": "",
            "data": {
                "task_id": "task-abc",
                "status": "pending",
                "submitted_at": "2026-05-19T10:00:00Z",
                "timeout_at": "2026-05-19T10:05:00Z",
            },
        })
    )
    result = await client.invoke(
        capability="ks-mcp-image-gen.generate",
        args={"prompt": "x"},
        mode="async",
    )
    assert isinstance(result, InvokeAsyncResult)
    assert result.task_id == "task-abc"
    assert result.status == "pending"


@pytest.mark.asyncio
@respx.mock
async def test_invoke_404_raises_capability_not_found(client):
    respx.post(f"{GATEWAY}/v1/apps/self/invoke").mock(
        return_value=httpx.Response(404, json={
            "code": 40400, "message": "capability not found",
            "data": None,
        })
    )
    with pytest.raises(CapabilityNotFound) as exc_info:
        await client.invoke(capability="ks.x", args={}, mode="sync")
    assert exc_info.value.canonical_name == "ks.x"


@pytest.mark.asyncio
@respx.mock
async def test_invoke_403_raises_capability_forbidden(client):
    respx.post(f"{GATEWAY}/v1/apps/self/invoke").mock(
        return_value=httpx.Response(403, json={
            "code": 40300, "message": "forbidden", "data": None,
        })
    )
    with pytest.raises(CapabilityForbidden):
        await client.invoke(capability="ks.x", args={}, mode="sync")


@pytest.mark.asyncio
@respx.mock
async def test_invoke_429_raises_rate_limit(client):
    respx.post(f"{GATEWAY}/v1/apps/self/invoke").mock(
        return_value=httpx.Response(
            429, json={"code": 42900, "message": "rate limit", "data": None},
            headers={"Retry-After": "5"},
        )
    )
    with pytest.raises(RateLimitError) as exc_info:
        await client.invoke(capability="ks.x", args={}, mode="sync")
    assert exc_info.value.retry_after_ms == 5000


@pytest.mark.asyncio
@respx.mock
async def test_invoke_503_raises_capability_unavailable(client):
    respx.post(f"{GATEWAY}/v1/apps/self/invoke").mock(
        return_value=httpx.Response(
            503, json={"code": 50300, "message": "backend down", "data": None},
            headers={"Retry-After": "2"},
        )
    )
    with pytest.raises(CapabilityUnavailable) as exc_info:
        await client.invoke(capability="ks.x", args={}, mode="sync")
    assert exc_info.value.retry_after_ms == 2000


@pytest.mark.asyncio
@respx.mock
async def test_invoke_502_raises_backend_error(client):
    respx.post(f"{GATEWAY}/v1/apps/self/invoke").mock(
        return_value=httpx.Response(502, json={
            "code": 50200, "message": "backend exploded", "data": None,
        })
    )
    with pytest.raises(BackendError):
        await client.invoke(capability="ks.x", args={}, mode="sync")


@pytest.mark.asyncio
@respx.mock
async def test_invoke_408_raises_timeout(client):
    respx.post(f"{GATEWAY}/v1/apps/self/invoke").mock(
        return_value=httpx.Response(408, json={
            "code": 40800, "message": "timeout", "data": None,
        })
    )
    with pytest.raises(Timeout):
        await client.invoke(capability="ks.x", args={}, mode="sync")


@pytest.mark.asyncio
@respx.mock
async def test_report_progress_posts(client):
    route = respx.post(f"{GATEWAY}/v1/user-tasks/task-1/progress").mock(
        return_value=httpx.Response(200, json={"code": 0, "data": {}})
    )
    await client.report_progress(task_id="task-1", stage="正在搜索", percent=10)
    assert route.called


@pytest.mark.asyncio
@respx.mock
async def test_get_task_returns_snapshot(client):
    respx.get(f"{GATEWAY}/v1/user-tasks/task-1").mock(
        return_value=httpx.Response(200, json={
            "code": 0,
            "data": {
                "task_id": "task-1", "status": "running",
                "canonical_name": "ks.x", "percent": 30,
                "stage_message": "halfway",
            },
        })
    )
    snap = await client.get_task("task-1")
    assert isinstance(snap, TaskSnapshot)
    assert snap.task_id == "task-1"
    assert snap.status == "running"


@pytest.mark.asyncio
@respx.mock
async def test_get_task_404_raises(client):
    respx.get(f"{GATEWAY}/v1/user-tasks/missing").mock(
        return_value=httpx.Response(404, json={"code": 40400, "data": None})
    )
    with pytest.raises(TaskNotFound):
        await client.get_task("missing")


@pytest.mark.asyncio
@respx.mock
async def test_cancel_task_posts(client):
    route = respx.post(f"{GATEWAY}/v1/user-tasks/task-1/cancel").mock(
        return_value=httpx.Response(200, json={"code": 0, "data": {}})
    )
    await client.cancel_task("task-1")
    assert route.called


@pytest.mark.asyncio
@respx.mock
async def test_invoke_includes_on_behalf_of_user_id(client):
    """on_behalf_of_user_id > 0 时编码进 /v1/apps/self/invoke payload。"""
    route = respx.post(f"{GATEWAY}/v1/apps/self/invoke").mock(
        return_value=httpx.Response(200, json={
            "code": 0, "message": "",
            "data": {"result": {"ok": True}, "duration_ms": 1},
        })
    )
    await client.invoke(
        capability="test.echo", args={"message": "hi"}, mode="sync",
        on_behalf_of_user_id=42,
    )
    body = json.loads(route.calls.last.request.content)
    assert body["on_behalf_of_user_id"] == 42


@pytest.mark.asyncio
@respx.mock
async def test_invoke_omits_on_behalf_of_user_id_when_zero_or_none(client):
    """None / 0 时不出现在 payload（对齐 Go ">0" 守卫）。"""
    respx.post(f"{GATEWAY}/v1/apps/self/invoke").mock(
        return_value=httpx.Response(200, json={
            "code": 0, "message": "", "data": {"result": {}, "duration_ms": 1},
        })
    )
    await client.invoke(capability="test.echo", args={}, mode="sync")
    body_none = json.loads(respx.calls.last.request.content)
    assert "on_behalf_of_user_id" not in body_none

    await client.invoke(capability="test.echo", args={}, mode="sync", on_behalf_of_user_id=0)
    body_zero = json.loads(respx.calls.last.request.content)
    assert "on_behalf_of_user_id" not in body_zero


@pytest.mark.asyncio
@respx.mock
async def test_invoke_writes_chain_headers(client):
    route = respx.post(f"{GATEWAY}/v1/apps/self/invoke").mock(
        return_value=httpx.Response(200, json={"code": 0, "message": "", "data": {"result": {}, "duration_ms": 1}})
    )
    await client.invoke(
        capability="test.echo", args={}, mode="sync",
        chain_id="chn_1", chain_header="eyJjaGFpbiI6IDF9",
    )
    req = route.calls.last.request
    assert req.headers["X-Keystone-Chain-Id"] == "chn_1"
    assert req.headers["X-Keystone-Call-Chain"] == "eyJjaGFpbiI6IDF9"


@pytest.mark.asyncio
@respx.mock
async def test_invoke_omits_chain_headers_when_empty(client):
    route = respx.post(f"{GATEWAY}/v1/apps/self/invoke").mock(
        return_value=httpx.Response(200, json={"code": 0, "message": "", "data": {"result": {}, "duration_ms": 1}})
    )
    await client.invoke(capability="test.echo", args={}, mode="sync")
    req = route.calls.last.request
    assert "X-Keystone-Chain-Id" not in req.headers
    assert "X-Keystone-Call-Chain" not in req.headers
