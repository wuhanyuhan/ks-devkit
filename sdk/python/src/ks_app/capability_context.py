"""CapabilityContext：capability handler 收到的运行时上下文。

设计要点：
- 与现有 ToolContext 并列：ToolContext 暴露 MCP _meta（resource_scope / execution_id 等
  MCP 协议层面字段），CapabilityContext 暴露 capability mesh 层面字段
  （user_id / caller_id / chain_id / task_id 等 caller 身份字段）。
- mcp_tool backend 路径下，字段来自 _meta.ks_*；http_endpoint backend 路径下，
  字段来自 scoped JWT claims。
- progress() / deadline() / cancelled() 是 capability mesh 特有的能力，
  ToolContext 不提供。
"""
from __future__ import annotations

import time
from dataclasses import dataclass, field
from typing import Optional, Protocol


class _ProgressReporter(Protocol):
    """DispatcherClient 子集，避免循环 import。"""

    async def report_progress(
        self, *, task_id: str, stage: str, percent: Optional[int]
    ) -> None: ...


@dataclass
class CapabilityContext:
    """capability handler 收到的运行时上下文。

    handler 签名：``async def handler(ctx: CapabilityContext, args: dict) -> dict``
    """

    user_id: str
    caller_id: str
    caller_kind: str
    chain_id: str
    task_id: str
    request_id: str
    canonical_name: str
    timeout_ms: int
    # capability mesh 调用链快照（承载 wire X-Keystone-Call-Chain / _meta.ks_chain_snapshot，
    # 对齐 Go CapabilityContext.ChainHeader()）。带默认值，放非默认字段末尾以满足 dataclass 排序。
    chain_header: str = ""
    dispatcher_client: Optional[_ProgressReporter] = None
    started_at_ms: int = field(default_factory=lambda: int(time.time() * 1000))
    _cancelled: bool = False

    async def progress(self, stage: str, percent: Optional[int] = None) -> None:
        """上报进度（仅 long_running 任务有效；sync 调用时是 no-op）。

        调 dispatcher ``/v1/user-tasks/:task_id/progress`` 写后端任务事件。
        client 网络错误不 raise — 进度丢失不应导致业务 handler 失败。
        """
        if not self.task_id:
            return
        if self.dispatcher_client is None:
            return
        try:
            await self.dispatcher_client.report_progress(
                task_id=self.task_id, stage=stage, percent=percent,
            )
        except Exception:
            pass

    def deadline(self) -> int:
        """任务超时点（unix ms）。timeout_ms<=0 时返 0（无超时）。"""
        if self.timeout_ms <= 0:
            return 0
        return self.started_at_ms + self.timeout_ms

    def cancelled(self) -> bool:
        """cooperative cancellation check。long_running handler 应周期性 poll。"""
        return self._cancelled

    def _set_cancelled(self) -> None:
        """SDK 内部使用（事件流收到 task.cancelled 时调）。"""
        self._cancelled = True


def build_context_from_meta(
    *,
    meta: dict,
    canonical_name: str,
    timeout_ms: int,
    dispatcher_client: Optional[_ProgressReporter] = None,
) -> CapabilityContext:
    """从 MCP ``_meta.ks_*`` 字段构造 CapabilityContext（mcp_tool backend 路径）。

    与 ToolContext._set_meta 不同：CapabilityContext 是 per-call 不可变快照，
    不走 ContextVar；handler 拿到 ctx 直接读。
    """

    def _g(key: str) -> str:
        v = meta.get(key, "") if isinstance(meta, dict) else ""
        if v is None:
            return ""
        if isinstance(v, str):
            return v
        return str(v)

    return CapabilityContext(
        user_id=_g("ks_user_id"),
        caller_id=_g("ks_caller_id"),
        caller_kind=_g("ks_caller_kind"),
        chain_id=_g("ks_chain_id"),
        chain_header=_g("ks_chain_snapshot"),
        task_id=_g("ks_task_id"),
        request_id=_g("ks_request_id"),
        canonical_name=canonical_name,
        timeout_ms=timeout_ms,
        dispatcher_client=dispatcher_client,
    )
