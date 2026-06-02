"""Capability Mesh SDK 错误层级。

所有 SDK 抛出的非 stdlib 异常都从 KeystoneError 派生，方便用户统一 except。
"""
from __future__ import annotations

from typing import Optional


class KeystoneError(Exception):
    """SDK 错误基类。所有 Capability Mesh 相关的 SDK 异常都从这里继承。"""


# ── 401 AuthError ─────────────────────────────────────────────────────────────

class AuthError(KeystoneError):
    """401 等价错误：token 不被接受。"""


class TokenInvalid(AuthError):
    """scoped JWT 签名错误 / 解码失败 / 字段缺失。"""


class TokenExpired(AuthError):
    """scoped JWT exp 已过。"""


class TokenAudienceMismatch(AuthError):
    """scoped JWT aud 与当前 capability canonical_name 不一致。"""


# ── 403 PermissionError ───────────────────────────────────────────────────────

class PermissionError(KeystoneError):  # noqa: A001 — 刻意遮蔽 builtins.PermissionError
    """403：caller 无权调用。注意：本类与 builtins.PermissionError 同名；
    用户若 `from ks_app.errors import PermissionError` 会替换 builtin。
    建议 import 时改名：`from ks_app.errors import PermissionError as CapPermErr`。
    """


class CapabilityForbidden(PermissionError):
    """caller 不在 allowed_callers 白名单内或 default_grant 未覆盖。"""


class ApprovalRequired(PermissionError):
    """capability requires_approval=true 但未拿到 approval。"""


class CapabilityDisabled(PermissionError):
    """capability.status=disabled。"""


# ── 404 NotFoundError ─────────────────────────────────────────────────────────

class NotFoundError(KeystoneError):
    """404：目标 capability / task 不存在。"""


class CapabilityNotFound(NotFoundError):
    def __init__(self, canonical_name: str, *, message: Optional[str] = None):
        self.canonical_name = canonical_name
        super().__init__(message or f"capability not found: {canonical_name}")


class TaskNotFound(NotFoundError):
    def __init__(self, task_id: str, *, message: Optional[str] = None):
        self.task_id = task_id
        super().__init__(message or f"task not found: {task_id}")


# ── 400 ValidationError ───────────────────────────────────────────────────────

class ValidationError(KeystoneError):
    """400：入参 / 注册校验失败。"""


class InvalidArgs(ValidationError):
    """capability 入参不符合 input_schema。"""


class ManifestMismatch(ValidationError):
    """SDK 启动期校验：@app.capability 注册的 canonical_name 不在 manifest.provides.capabilities 内。"""

    def __init__(self, registered: str, manifest_names: list[str]):
        self.registered = registered
        self.manifest_names = manifest_names
        super().__init__(
            f"capability {registered!r} 未在 manifest.provides.capabilities 内声明，"
            f"现有声明 = {manifest_names!r}"
        )


# ── DependencyError 依赖问题 ───────────────────────────────────────────────────

class DependencyError(KeystoneError):
    """依赖类错误（503 / 508 / 451）。"""


class CapabilityUnavailable(DependencyError):
    """503：backend 不健康 / circuit-break。"""

    def __init__(self, message: str, *, retry_after_ms: Optional[int] = None):
        self.retry_after_ms = retry_after_ms
        super().__init__(message)


class LoopDetected(DependencyError):
    """508：chain header 中检测到环。"""


class GuardrailBlocked(DependencyError):
    """451：guardrail 拒绝（内容 / 合规）。"""


# ── ExecutionError 运行时错误 ─────────────────────────────────────────────────

class ExecutionError(KeystoneError):
    """运行时类错误。"""


class BackendError(ExecutionError):
    """502：backend 抛业务错。"""


class Timeout(ExecutionError):
    """408：sync 调用超时 / async 任务超时。"""

    def __init__(self, *, deadline_ms: int, elapsed_ms: int):
        self.deadline_ms = deadline_ms
        self.elapsed_ms = elapsed_ms
        super().__init__(f"capability timeout: elapsed={elapsed_ms}ms deadline={deadline_ms}ms")


class Cancelled(ExecutionError):
    """async 任务被取消（terminal cancelled）。"""


class DispatcherRestarted(ExecutionError):
    """dispatcher 重启遗留的孤儿任务被标记 failed。"""


# ── 429 RateLimitError ────────────────────────────────────────────────────────

class RateLimitError(KeystoneError):
    """429：超限流。"""

    def __init__(self, message: str, *, retry_after_ms: Optional[int] = None):
        self.retry_after_ms = retry_after_ms
        super().__init__(message)


class CapabilityConcurrencyLimit(RateLimitError):
    """单 capability 全局并发上限。"""


class UserQuotaExceeded(RateLimitError):
    """单 user 调用配额超限。"""


class AppQuotaExceeded(RateLimitError):
    """单 app 调用配额超限。"""


__all__ = [
    "KeystoneError",
    "AuthError", "TokenInvalid", "TokenExpired", "TokenAudienceMismatch",
    "PermissionError", "CapabilityForbidden", "ApprovalRequired", "CapabilityDisabled",
    "NotFoundError", "CapabilityNotFound", "TaskNotFound",
    "ValidationError", "InvalidArgs", "ManifestMismatch",
    "DependencyError", "CapabilityUnavailable", "LoopDetected", "GuardrailBlocked",
    "ExecutionError", "BackendError", "Timeout", "Cancelled", "DispatcherRestarted",
    "RateLimitError", "CapabilityConcurrencyLimit", "UserQuotaExceeded", "AppQuotaExceeded",
]
