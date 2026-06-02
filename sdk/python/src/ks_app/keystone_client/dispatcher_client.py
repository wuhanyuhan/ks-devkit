"""DispatcherClient：与 keystone capability dispatcher 通讯的 HTTP 客户端。

覆盖端点：
  - POST /v1/apps/self/invoke         统一调用入口（sync / async）
  - POST /v1/user-tasks/:id/progress  capability handler 上报进度
  - GET  /v1/user-tasks/:id           查任务快照
  - POST /v1/user-tasks/:id/cancel    取消任务

所有端点 apptoken 鉴权（Bearer KS_APP_TOKEN）。HTTP 错误映射成
SDK 错误类型。
"""
from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Optional

import httpx

from ..errors import (
    BackendError,
    CapabilityDisabled,
    CapabilityForbidden,
    CapabilityNotFound,
    CapabilityUnavailable,
    GuardrailBlocked,
    InvalidArgs,
    KeystoneError,
    LoopDetected,
    RateLimitError,
    TaskNotFound,
    Timeout,
    TokenAudienceMismatch,
    TokenExpired,
    TokenInvalid,
)

# capability mesh 调用链穿透 header（与 Go dispatcher_client.go / scoped JWT 路径一致）。
HEADER_CALL_CHAIN = "X-Keystone-Call-Chain"
HEADER_CHAIN_ID = "X-Keystone-Chain-Id"


@dataclass
class InvokeSyncResult:
    result: dict
    duration_ms: int
    billed: Optional[dict] = None


@dataclass
class InvokeAsyncResult:
    task_id: str
    status: str
    submitted_at: str
    timeout_at: str


@dataclass
class TaskSnapshot:
    task_id: str
    status: str
    canonical_name: str = ""
    percent: int = 0
    stage_message: str = ""
    result: Optional[dict] = None
    error_code: str = ""
    error_message: str = ""


def _retry_after_ms(headers: httpx.Headers) -> Optional[int]:
    ra = headers.get("retry-after") or headers.get("Retry-After")
    if not ra:
        return None
    try:
        return int(float(ra) * 1000)
    except ValueError:
        return None


def _map_error(status: int, body: dict, headers: httpx.Headers) -> KeystoneError:
    """HTTP status → SDK 错误。"""
    msg = ""
    code = 0
    if isinstance(body, dict):
        msg = str(body.get("message", "") or "")
        code = int(body.get("code", 0) or 0)
    if status == 401:
        if code == 40103:
            return TokenAudienceMismatch(msg or "aud mismatch")
        if code == 40102:
            return TokenExpired(msg or "token expired")
        return TokenInvalid(msg or "token invalid")
    if status == 403:
        if code == 40301:
            return CapabilityDisabled(msg or "capability disabled")
        return CapabilityForbidden(msg or "capability forbidden")
    if status == 404:
        # 不在此处区分 task / capability：caller 根据路径上下文已知，
        # 上层 get_task / cancel_task / invoke 自行 catch 并重映射。
        return CapabilityNotFound(canonical_name="", message=msg)
    if status == 408:
        return Timeout(deadline_ms=0, elapsed_ms=0)
    if status == 429:
        return RateLimitError(msg or "rate limit", retry_after_ms=_retry_after_ms(headers))
    if status == 451:
        return GuardrailBlocked(msg or "guardrail blocked")
    if status == 502:
        return BackendError(msg or "backend error")
    if status == 503:
        return CapabilityUnavailable(msg or "capability unavailable",
                                     retry_after_ms=_retry_after_ms(headers))
    if status == 508:
        return LoopDetected(msg or "loop detected")
    if status == 400:
        return InvalidArgs(msg or "invalid args")
    return BackendError(f"unexpected http status={status} body={body}")


class DispatcherClient:
    """与 keystone capability dispatcher 通讯的 HTTP 客户端。

    instance lifecycle = SDK 进程级（持有 httpx.AsyncClient 连接池，关闭交给 GC
    或调 close()）。
    """

    def __init__(
        self,
        *,
        gateway_url: str,
        app_token: str,
        timeout: float = 30.0,
    ):
        self.gateway_url = gateway_url.rstrip("/")
        self.app_token = app_token
        self.timeout = timeout
        self._client: Optional[httpx.AsyncClient] = None

    def _get_client(self) -> httpx.AsyncClient:
        if self._client is None:
            self._client = httpx.AsyncClient(
                timeout=self.timeout,
                headers={"Authorization": f"Bearer {self.app_token}"},
            )
        return self._client

    async def close(self) -> None:
        if self._client is not None:
            await self._client.aclose()
            self._client = None

    async def _post(self, path: str, json: dict, headers: Optional[dict] = None) -> dict:
        c = self._get_client()
        try:
            resp = await c.post(f"{self.gateway_url}{path}", json=json, headers=headers or None)
        except httpx.RequestError as e:
            raise CapabilityUnavailable(f"网络错误: {e}") from e
        body = self._parse_body(resp)
        if resp.status_code >= 400:
            raise _map_error(resp.status_code, body, resp.headers)
        if body.get("code", 0) != 0:
            raise BackendError(
                f"业务错误: code={body.get('code')} message={body.get('message')}"
            )
        return body.get("data") or {}

    async def _get(self, path: str) -> dict:
        c = self._get_client()
        try:
            resp = await c.get(f"{self.gateway_url}{path}")
        except httpx.RequestError as e:
            raise CapabilityUnavailable(f"网络错误: {e}") from e
        body = self._parse_body(resp)
        if resp.status_code >= 400:
            raise _map_error(resp.status_code, body, resp.headers)
        if body.get("code", 0) != 0:
            raise BackendError(f"业务错误: code={body.get('code')}")
        return body.get("data") or {}

    @staticmethod
    def _parse_body(resp: httpx.Response) -> dict:
        try:
            data = resp.json()
            if isinstance(data, dict):
                return data
            return {"data": data}
        except Exception:
            return {}

    async def invoke(
        self,
        *,
        capability: str,
        args: dict,
        mode: str = "auto",
        idempotency_key: Optional[str] = None,
        timeout_ms_override: Optional[int] = None,
        on_behalf_of_user_id: Optional[int] = None,
        chain_id: Optional[str] = None,
        chain_header: Optional[str] = None,
    ):
        payload: dict[str, Any] = {
            "capability": capability,
            "args": args,
            "mode": mode,
        }
        if idempotency_key:
            payload["idempotency_key"] = idempotency_key
        if timeout_ms_override is not None:
            payload["timeout_ms_override"] = timeout_ms_override
        if on_behalf_of_user_id is not None and on_behalf_of_user_id > 0:
            # 仅 >0 时发送，对齐 Go dispatcher_client.go 的 OnBehalfOfUserID>0 守卫
            payload["on_behalf_of_user_id"] = on_behalf_of_user_id
        # capability mesh 多跳调用链穿透：chain_id/chain_header 走 HTTP header，
        # 非 payload；对齐 Go WithChainContext 的 wire 形态。空值不发送。
        chain_headers: dict = {}
        if chain_header:
            chain_headers[HEADER_CALL_CHAIN] = chain_header
        if chain_id:
            chain_headers[HEADER_CHAIN_ID] = chain_id
        try:
            data = await self._post("/v1/apps/self/invoke", payload, headers=chain_headers or None)
        except CapabilityNotFound as e:
            e.canonical_name = capability
            raise
        if "task_id" in data:
            return InvokeAsyncResult(
                task_id=str(data["task_id"]),
                status=str(data.get("status", "")),
                submitted_at=str(data.get("submitted_at", "")),
                timeout_at=str(data.get("timeout_at", "")),
            )
        return InvokeSyncResult(
            result=data.get("result") or {},
            duration_ms=int(data.get("duration_ms", 0) or 0),
            billed=data.get("billed"),
        )

    async def report_progress(
        self, *, task_id: str, stage: str, percent: Optional[int],
    ) -> None:
        payload: dict[str, Any] = {"stage_message": stage}
        if percent is not None:
            payload["percent"] = int(percent)
        await self._post(f"/v1/user-tasks/{task_id}/progress", payload)

    async def get_task(self, task_id: str) -> TaskSnapshot:
        try:
            data = await self._get(f"/v1/user-tasks/{task_id}")
        except CapabilityNotFound as e:
            raise TaskNotFound(task_id, message=str(e)) from e
        return TaskSnapshot(
            task_id=str(data.get("task_id", task_id)),
            status=str(data.get("status", "")),
            canonical_name=str(data.get("canonical_name", "")),
            percent=int(data.get("percent", 0) or 0),
            stage_message=str(data.get("stage_message", "")),
            result=data.get("result"),
            error_code=str(data.get("error_code", "")),
            error_message=str(data.get("error_message", "")),
        )

    async def cancel_task(self, task_id: str) -> None:
        try:
            await self._post(f"/v1/user-tasks/{task_id}/cancel", {})
        except CapabilityNotFound as e:
            raise TaskNotFound(task_id, message=str(e)) from e
