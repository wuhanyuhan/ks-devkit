"""caller-side：CapabilityCall + Task 对象。

CapabilityCall 是 ``app.call_capability(name)`` 返的构造器，
  ``.invoke(**args)``    阻塞调用、返结果
  ``.submit(**args)``    异步提交、返 Task

Task 是 async 任务的句柄，
  ``await task.result(timeout=None)``       等终态
  ``async for ev in task.events()``          订阅 lifecycle event（WS 驱动）
  ``await task.cancel()``                    发取消
  ``await task.refresh()``                   主动拉一次最新态
"""
from __future__ import annotations

import asyncio
from dataclasses import dataclass
from typing import Any, AsyncIterator, Optional

from .errors import BackendError, Cancelled, DispatcherRestarted, Timeout
from .keystone_client.dispatcher_client import (
    DispatcherClient,
    InvokeAsyncResult,
    InvokeSyncResult,
)

_TERMINAL_STATUSES = frozenset({"done", "failed", "cancelled"})


@dataclass
class Task:
    """async 任务句柄。"""
    task_id: str
    status: str
    canonical_name: str
    dispatcher_client: DispatcherClient
    events_client_getter: Optional[Any] = None
    percent: int = 0
    stage_message: str = ""
    result_payload: Optional[dict] = None
    error_code: str = ""
    error_message: str = ""

    async def refresh(self) -> None:
        snap = await self.dispatcher_client.get_task(self.task_id)
        self.status = snap.status
        self.percent = snap.percent
        self.stage_message = snap.stage_message
        if snap.result is not None:
            self.result_payload = snap.result
        if snap.error_code:
            self.error_code = snap.error_code
        if snap.error_message:
            self.error_message = snap.error_message

    async def cancel(self) -> None:
        await self.dispatcher_client.cancel_task(self.task_id)

    async def result(
        self, *, timeout: Optional[float] = None, poll_interval_s: float = 2.0,
    ) -> dict:
        """等终态。done 返 result_payload；failed / cancelled / orphan 抛错。

        有 inbound WS 时优先用 event 驱动；polling 兜底。
        """
        deadline = (
            None if timeout is None else asyncio.get_event_loop().time() + timeout
        )
        while True:
            await self.refresh()
            if self.status in _TERMINAL_STATUSES:
                break
            if deadline is not None and asyncio.get_event_loop().time() >= deadline:
                raise Timeout(
                    deadline_ms=int(timeout * 1000) if timeout else 0,
                    elapsed_ms=int(timeout * 1000) if timeout else 0,
                )
            if poll_interval_s > 0:
                await asyncio.sleep(poll_interval_s)
            elif deadline is None:
                break

        if self.status == "done":
            return self.result_payload or {}
        if self.status == "failed":
            if self.error_code in ("50000", "DispatcherRestarted"):
                raise DispatcherRestarted(self.error_message or "dispatcher restarted")
            raise BackendError(self.error_message or f"task {self.task_id} failed")
        if self.status == "cancelled":
            raise Cancelled(self.error_message or f"task {self.task_id} cancelled")
        raise BackendError(f"task {self.task_id} unexpected status={self.status}")

    async def events(self) -> AsyncIterator[dict]:
        """订阅 lifecycle event 流。

        实现：经 ``events_client_getter`` 拿 EventsClient → register(task_id)
        → 异步迭代 stream。若 client 无（env 未配 / 老路径），降级到周期 refresh。
        """
        getter = self.events_client_getter
        client = getter() if getter else None
        if client is None:
            while True:
                await self.refresh()
                yield {
                    "type": "snapshot",
                    "task_id": self.task_id,
                    "status": self.status,
                    "percent": self.percent,
                    "stage_message": self.stage_message,
                }
                if self.status in _TERMINAL_STATUSES:
                    return
                await asyncio.sleep(2.0)
        else:
            await client.start()
            stream = client.register(self.task_id)
            try:
                async for ev in stream:
                    yield ev
                    if ev.get("status") in _TERMINAL_STATUSES:
                        return
            finally:
                client.unregister(self.task_id)


@dataclass
class CapabilityCall:
    """app.call_capability(name) 返的构造器。"""
    canonical_name: str
    dispatcher_client: DispatcherClient
    events_client_getter: Optional[Any] = None

    async def invoke(
        self,
        *,
        idempotency_key: Optional[str] = None,
        timeout_ms_override: Optional[int] = None,
        on_behalf_of_user_id: Optional[int] = None,
        chain_id: Optional[str] = None,
        chain_header: Optional[str] = None,
        **args: Any,
    ) -> dict:
        """sync 调用 dispatcher，返结果 dict。"""
        result = await self.dispatcher_client.invoke(
            capability=self.canonical_name,
            args=args,
            mode="sync",
            idempotency_key=idempotency_key,
            timeout_ms_override=timeout_ms_override,
            on_behalf_of_user_id=on_behalf_of_user_id,
            chain_id=chain_id,
            chain_header=chain_header,
        )
        if isinstance(result, InvokeSyncResult):
            return result.result
        raise BackendError(
            f"capability {self.canonical_name} 期望 sync 但服务端返 "
            f"task_id={result.task_id}"
        )

    async def submit(
        self,
        *,
        idempotency_key: Optional[str] = None,
        timeout_ms_override: Optional[int] = None,
        on_behalf_of_user_id: Optional[int] = None,
        chain_id: Optional[str] = None,
        chain_header: Optional[str] = None,
        **args: Any,
    ) -> Task:
        """异步提交，返 Task。"""
        result = await self.dispatcher_client.invoke(
            capability=self.canonical_name,
            args=args,
            mode="async",
            idempotency_key=idempotency_key,
            timeout_ms_override=timeout_ms_override,
            on_behalf_of_user_id=on_behalf_of_user_id,
            chain_id=chain_id,
            chain_header=chain_header,
        )
        if isinstance(result, InvokeAsyncResult):
            return Task(
                task_id=result.task_id,
                status=result.status,
                canonical_name=self.canonical_name,
                dispatcher_client=self.dispatcher_client,
                events_client_getter=self.events_client_getter,
            )
        if isinstance(result, InvokeSyncResult):
            return Task(
                task_id="",
                status="done",
                canonical_name=self.canonical_name,
                dispatcher_client=self.dispatcher_client,
                events_client_getter=self.events_client_getter,
                result_payload=result.result,
            )
        raise BackendError(f"unexpected invoke result type: {type(result).__name__}")
