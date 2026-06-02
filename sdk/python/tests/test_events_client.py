"""EventsClient (WS + polling) 单测。"""
import asyncio

import httpx
import pytest
import respx

from ks_app.events import EventsClient, TaskEventStream


@pytest.mark.asyncio
async def test_register_creates_queue_for_task_id():
    client = EventsClient(gateway_url="http://k", app_token="t", event_mode="polling")
    stream = client.register("task-1")
    assert isinstance(stream, TaskEventStream)
    assert "task-1" in client._streams


@pytest.mark.asyncio
async def test_dispatch_event_routes_by_task_id():
    client = EventsClient(gateway_url="http://k", app_token="t", event_mode="polling")
    stream = client.register("task-1")

    await client._dispatch({
        "type": "capability.task.lifecycle",
        "task_id": "task-1",
        "status": "running",
        "percent": 30,
    })

    ev = await asyncio.wait_for(stream._queue.get(), timeout=1.0)
    assert ev["status"] == "running"
    assert ev["percent"] == 30


@pytest.mark.asyncio
async def test_dispatch_event_to_unknown_task_dropped():
    """未注册的 task_id 事件被丢弃（不会泄漏到其他 stream）。"""
    client = EventsClient(gateway_url="http://k", app_token="t", event_mode="polling")
    stream = client.register("task-1")

    await client._dispatch({
        "type": "capability.task.lifecycle",
        "task_id": "task-OTHER",
        "status": "running",
    })

    assert stream._queue.empty()


@pytest.mark.asyncio
async def test_stream_terminal_event_marks_done():
    client = EventsClient(gateway_url="http://k", app_token="t", event_mode="polling")
    stream = client.register("task-1")

    await client._dispatch({
        "type": "capability.task.lifecycle",
        "task_id": "task-1",
        "status": "done",
    })

    received = []
    async for ev in stream:
        received.append(ev)
        if ev["status"] in ("done", "failed", "cancelled"):
            break
    assert received == [{
        "type": "capability.task.lifecycle",
        "task_id": "task-1",
        "status": "done",
    }]


@pytest.mark.asyncio
async def test_polling_fetch_returns_events_and_advances_cursor():
    """polling 模式：GET /v1/apps/self/events?since=cursor 拿到事件列表。"""
    with respx.mock(assert_all_called=False) as router:
        router.get("http://k/v1/apps/self/events").mock(
            return_value=httpx.Response(200, json={
                "code": 0,
                "data": {
                    "events": [
                        {"type": "capability.task.lifecycle",
                         "task_id": "t1", "status": "running"},
                    ],
                    "next_cursor": "100",
                    "heartbeat_ts": 999,
                },
            })
        )
        client = EventsClient(gateway_url="http://k", app_token="t", event_mode="polling")
        stream = client.register("t1")
        await client._poll_once()
        assert client._polling_cursor == "100"
        ev = await asyncio.wait_for(stream._queue.get(), timeout=1.0)
        assert ev["status"] == "running"


@pytest.mark.asyncio
async def test_unregister_removes_queue():
    client = EventsClient(gateway_url="http://k", app_token="t", event_mode="polling")
    client.register("task-1")
    client.unregister("task-1")
    assert "task-1" not in client._streams
