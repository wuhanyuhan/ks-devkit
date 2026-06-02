"""Capability Mesh 错误层级单测。"""
import pytest

from ks_app.errors import (
    KeystoneError,
    AuthError, TokenInvalid, TokenExpired, TokenAudienceMismatch,
    PermissionError, CapabilityForbidden, ApprovalRequired, CapabilityDisabled,
    NotFoundError, CapabilityNotFound, TaskNotFound,
    ValidationError, InvalidArgs, ManifestMismatch,
    DependencyError, CapabilityUnavailable, LoopDetected, GuardrailBlocked,
    ExecutionError, BackendError, Timeout, Cancelled, DispatcherRestarted,
    RateLimitError, CapabilityConcurrencyLimit, UserQuotaExceeded, AppQuotaExceeded,
)


def test_keystone_error_base():
    assert issubclass(AuthError, KeystoneError)
    assert issubclass(PermissionError, KeystoneError)
    assert issubclass(NotFoundError, KeystoneError)
    assert issubclass(ValidationError, KeystoneError)
    assert issubclass(DependencyError, KeystoneError)
    assert issubclass(ExecutionError, KeystoneError)
    assert issubclass(RateLimitError, KeystoneError)


def test_token_audience_mismatch_subclass():
    assert issubclass(TokenAudienceMismatch, AuthError)
    err = TokenAudienceMismatch("aud=foo, expected=bar")
    assert isinstance(err, KeystoneError)
    assert "foo" in str(err)


def test_capability_not_found_carries_canonical_name():
    err = CapabilityNotFound("ks-mcp-x.foo")
    assert err.canonical_name == "ks-mcp-x.foo"
    assert "ks-mcp-x.foo" in str(err)


def test_task_not_found_carries_task_id():
    err = TaskNotFound("task-abc")
    assert err.task_id == "task-abc"
    assert "task-abc" in str(err)


def test_manifest_mismatch_carries_detail():
    err = ManifestMismatch(
        registered="ks-mcp-w.create",
        manifest_names=["ks-mcp-w.list"],
    )
    assert err.registered == "ks-mcp-w.create"
    assert "ks-mcp-w.create" in str(err)
    assert "ks-mcp-w.list" in str(err)


def test_capability_unavailable_retry_after():
    err = CapabilityUnavailable("backend down", retry_after_ms=5000)
    assert err.retry_after_ms == 5000


def test_rate_limit_retry_after():
    err = CapabilityConcurrencyLimit("too many", retry_after_ms=1000)
    assert err.retry_after_ms == 1000
    assert issubclass(CapabilityConcurrencyLimit, RateLimitError)


def test_timeout_carries_deadline():
    err = Timeout(deadline_ms=30000, elapsed_ms=30100)
    assert err.deadline_ms == 30000
    assert err.elapsed_ms == 30100


def test_subtree_membership():
    """所有具体类必须挂在对应大类下。"""
    assert issubclass(TokenInvalid, AuthError)
    assert issubclass(TokenExpired, AuthError)
    assert issubclass(CapabilityForbidden, PermissionError)
    assert issubclass(ApprovalRequired, PermissionError)
    assert issubclass(CapabilityDisabled, PermissionError)
    assert issubclass(CapabilityNotFound, NotFoundError)
    assert issubclass(TaskNotFound, NotFoundError)
    assert issubclass(InvalidArgs, ValidationError)
    assert issubclass(ManifestMismatch, ValidationError)
    assert issubclass(CapabilityUnavailable, DependencyError)
    assert issubclass(LoopDetected, DependencyError)
    assert issubclass(GuardrailBlocked, DependencyError)
    assert issubclass(BackendError, ExecutionError)
    assert issubclass(Timeout, ExecutionError)
    assert issubclass(Cancelled, ExecutionError)
    assert issubclass(DispatcherRestarted, ExecutionError)
    assert issubclass(CapabilityConcurrencyLimit, RateLimitError)
    assert issubclass(UserQuotaExceeded, RateLimitError)
    assert issubclass(AppQuotaExceeded, RateLimitError)
