"""manifest.yaml 解析。

读取 mount.service.auth_mode 或 mount.extension.auth_mode 作为 auth 模式 fallback；
读取 mount.*.config_ui 作为 /meta 的 config_ui 段来源。
读取 provides.capabilities[] / requires.capabilities[]（v0.19.0 capability mesh）。

manifest 缺失或格式损坏不是错误（返回 None / 空列表）。
"""
from __future__ import annotations

import os
from dataclasses import dataclass
from typing import Optional

import yaml


# ── v0.19.0 capability mesh schema ────────────────────────────────────────────

@dataclass(frozen=True)
class BackendSpec:
    """capability 后端路由声明，对齐 ks-types v0.29.0 BackendSpec。"""
    kind: str
    tool_name: str = ""
    path: str = ""
    method: str = ""


@dataclass(frozen=True)
class ProvidesCapability:
    """单个 provides.capabilities[i]，对齐 ks-types v0.29.0 CapabilitySpec 必读字段。

    去前缀：作者写裸名 name；canonical_name 由 SDK 内部派生
    <app_id>.<name>（见 canonical.py / app.py）。SDK 启动期消费
    name / execution_mode / timeout_ms / backend / input_schema。
    """
    name: str
    execution_mode: str
    backend: BackendSpec
    timeout_ms: int = 0
    input_schema: Optional[dict] = None
    concurrency_limit: int = 0
    resumable: bool = False


@dataclass(frozen=True)
class RequiresCapability:
    """单个 requires.capabilities[i]。"""
    canonical_name: str
    mode: str = "required"
    reason: str = ""


# ── 通用 manifest 读取 ────────────────────────────────────────────────────────

def _read_manifest(path: str) -> Optional[dict]:
    if not path or not os.path.exists(path):
        return None
    try:
        with open(path, "r", encoding="utf-8") as f:
            data = yaml.safe_load(f) or {}
    except Exception:
        return None
    if not isinstance(data, dict):
        return None
    return data


def load_manifest_auth_mode(path: str) -> Optional[str]:
    """从 manifest.yaml 读取 auth_mode。service 优先 extension。"""
    data = _read_manifest(path)
    if not data:
        return None
    mount = data.get("mount", {}) or {}
    service = mount.get("service")
    if isinstance(service, dict):
        am = service.get("auth_mode")
        if am:
            return am
    extension = mount.get("extension")
    if isinstance(extension, dict):
        am = extension.get("auth_mode")
        if am:
            return am
    return None


def load_manifest_config_ui(path: str) -> Optional[dict]:
    """从 manifest.yaml 读取 mount.*.config_ui。"""
    data = _read_manifest(path)
    if not data:
        return None
    mount = data.get("mount", {}) or {}
    for key in ("service", "extension"):
        section = mount.get(key)
        if isinstance(section, dict):
            cu = section.get("config_ui")
            if isinstance(cu, dict):
                return cu
    return None


def load_manifest_capabilities(path: str) -> list[ProvidesCapability]:
    """读取 provides.capabilities[]（v0.19.0 capability mesh）。"""
    data = _read_manifest(path)
    if not data:
        return []
    provides = data.get("provides", {}) or {}
    raw = provides.get("capabilities") or []
    if not isinstance(raw, list):
        return []
    out: list[ProvidesCapability] = []
    for item in raw:
        if not isinstance(item, dict):
            continue
        backend_raw = item.get("backend") or {}
        if not isinstance(backend_raw, dict):
            backend_raw = {}
        backend = BackendSpec(
            kind=str(backend_raw.get("kind", "")),
            tool_name=str(backend_raw.get("tool_name", "")),
            path=str(backend_raw.get("path", "")),
            method=str(backend_raw.get("method", "")),
        )
        out.append(ProvidesCapability(
            name=str(item.get("name", "")),
            execution_mode=str(item.get("execution_mode", "")),
            backend=backend,
            timeout_ms=int(item.get("timeout_ms", 0) or 0),
            input_schema=item.get("input_schema") if isinstance(item.get("input_schema"), dict) else None,
            concurrency_limit=int(item.get("concurrency_limit", 0) or 0),
            resumable=bool(item.get("resumable", False)),
        ))
    return out


def load_manifest_requires(path: str) -> list[RequiresCapability]:
    """读取 requires.capabilities[]。"""
    data = _read_manifest(path)
    if not data:
        return []
    requires = data.get("requires", {}) or {}
    raw = requires.get("capabilities") or []
    if not isinstance(raw, list):
        return []
    out: list[RequiresCapability] = []
    for item in raw:
        if not isinstance(item, dict):
            continue
        out.append(RequiresCapability(
            canonical_name=str(item.get("canonical_name", "")),
            mode=str(item.get("mode", "required") or "required"),
            reason=str(item.get("reason", "") or ""),
        ))
    return out
