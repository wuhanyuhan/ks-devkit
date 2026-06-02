"""EventsClient：caller-side 的 inbound WS + polling fallback。

启动时机：首次 ``App.call_capability(...).submit(...)`` 时 lazy 构造 + start。
生命周期：app 进程生命周期；shutdown 调 close。

两种模式：
  - ``event_mode='ws'`` (默认)：持长 WS 连接 /v1/apps/self/events
  - ``event_mode='polling'``：每 ``poll_interval`` 调 GET /v1/apps/self/events?since=<cursor>
"""
from __future__ import annotations

import asyncio
import json
import logging
from dataclasses import dataclass, field
from typing import Optional

import httpx

logger = logging.getLogger(__name__)


_TERMINAL_STATUSES = frozenset({"done", "failed", "cancelled"})


@dataclass
class TaskEventStream:
    """单个 task 的 event 队列。``Task.events()`` 内部消费。"""
    task_id: str
    _queue: asyncio.Queue = field(default_factory=asyncio.Queue)
    _closed: bool = False

    def __aiter__(self) -> "TaskEventStream":
        return self

    async def __anext__(self) -> dict:
        if self._closed and self._queue.empty():
            raise StopAsyncIteration
        ev = await self._queue.get()
        if isinstance(ev, dict) and ev.get("status") in _TERMINAL_STATUSES:
            self._closed = True
        return ev


class EventsClient:
    """Inbound 事件通道客户端。"""

    def __init__(
        self,
        *,
        gateway_url: str,
        app_token: str,
        event_mode: str = "ws",
        poll_interval_s: float = 2.0,
    ):
        self.gateway_url = gateway_url.rstrip("/")
        self.app_token = app_token
        self.event_mode = event_mode
        self.poll_interval_s = poll_interval_s
        self._streams: dict[str, TaskEventStream] = {}
        self._loop_task: Optional[asyncio.Task] = None
        self._stop = asyncio.Event()
        self._polling_cursor: str = "0"
        self._http_client: Optional[httpx.AsyncClient] = None

    def register(self, task_id: str) -> TaskEventStream:
        if task_id in self._streams:
            return self._streams[task_id]
        stream = TaskEventStream(task_id=task_id)
        self._streams[task_id] = stream
        return stream

    def unregister(self, task_id: str) -> None:
        stream = self._streams.pop(task_id, None)
        if stream is not None:
            stream._closed = True

    async def start(self) -> None:
        """启动后台 loop（幂等）。"""
        if self._loop_task is not None and not self._loop_task.done():
            return
        self._stop.clear()
        if self.event_mode == "polling":
            self._loop_task = asyncio.create_task(self._polling_loop())
        else:
            self._loop_task = asyncio.create_task(self._ws_loop())

    async def close(self) -> None:
        self._stop.set()
        if self._loop_task is not None:
            try:
                await asyncio.wait_for(self._loop_task, timeout=5.0)
            except asyncio.TimeoutError:
                self._loop_task.cancel()
        if self._http_client is not None:
            await self._http_client.aclose()
            self._http_client = None
        for s in self._streams.values():
            s._closed = True

    async def _dispatch(self, event: dict) -> None:
        """把一个事件按 task_id 路由到对应 stream。"""
        task_id = str(event.get("task_id", ""))
        if not task_id:
            return
        stream = self._streams.get(task_id)
        if stream is None:
            return
        await stream._queue.put(event)

    # ── polling 模式 ──────────────────────────────────────────────────────

    async def _polling_loop(self) -> None:
        async with httpx.AsyncClient(
            timeout=10.0,
            headers={"Authorization": f"Bearer {self.app_token}"},
        ) as c:
            self._http_client = c
            while not self._stop.is_set():
                try:
                    await self._poll_once()
                except Exception as e:
                    logger.warning("ks-app: events polling failed: %s", e)
                try:
                    await asyncio.wait_for(
                        self._stop.wait(), timeout=self.poll_interval_s,
                    )
                except asyncio.TimeoutError:
                    pass

    async def _poll_once(self) -> None:
        """单次 polling 拉事件 + 推进 cursor。"""
        c = self._http_client
        owns_client = False
        if c is None:
            c = httpx.AsyncClient(
                timeout=10.0,
                headers={"Authorization": f"Bearer {self.app_token}"},
            )
            self._http_client = c
            owns_client = True
        try:
            resp = await c.get(
                f"{self.gateway_url}/v1/apps/self/events",
                params={"since": self._polling_cursor},
            )
        finally:
            if owns_client:
                # 单测路径走这里：留 client 复用，close() 时统一关
                pass
        if resp.status_code != 200:
            return
        body = resp.json()
        data = body.get("data") or {}
        for ev in data.get("events") or []:
            if isinstance(ev, dict):
                await self._dispatch(ev)
        nc = data.get("next_cursor")
        if nc is not None:
            self._polling_cursor = str(nc)

    # ── WS 模式 ──────────────────────────────────────────────────────────

    async def _ws_loop(self) -> None:
        """WS 长连接 + 指数退避重连。"""
        import websockets

        backoff = 1.0
        ws_url = (
            self.gateway_url.replace("https://", "wss://", 1).replace(
                "http://", "ws://", 1,
            )
            + "/v1/apps/self/events"
        )

        while not self._stop.is_set():
            try:
                async with websockets.connect(
                    ws_url,
                    additional_headers={"Authorization": f"Bearer {self.app_token}"},
                ) as ws:
                    backoff = 1.0
                    async for raw in ws:
                        if self._stop.is_set():
                            break
                        try:
                            event = json.loads(raw)
                        except Exception:
                            continue
                        if not isinstance(event, dict):
                            continue
                        if event.get("type") == "heartbeat":
                            continue
                        await self._dispatch(event)
            except Exception as e:
                logger.warning(
                    "ks-app: events ws disconnected: %s; backoff=%ss", e, backoff,
                )
                try:
                    await asyncio.wait_for(self._stop.wait(), timeout=backoff)
                except asyncio.TimeoutError:
                    pass
                backoff = min(backoff * 2, 30.0)
