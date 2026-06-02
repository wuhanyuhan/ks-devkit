"""@app.capability decorator 注册 + 启动期 manifest 校验单测。"""
import textwrap
from pathlib import Path

import pytest

from ks_app import App
from ks_app.errors import ManifestMismatch


def _write_manifest(tmp_path: Path, body: str) -> str:
    p = tmp_path / "manifest.yaml"
    p.write_text(textwrap.dedent(body), encoding="utf-8")
    return str(p)


def test_capability_decorator_registers_handler(tmp_path):
    manifest_path = _write_manifest(tmp_path, """
        provides:
          capabilities:
            - name: foo
              execution_mode: sync
              backend:
                kind: mcp_tool
                tool_name: foo
    """)
    app = App("ks-mcp-x", manifest_path=manifest_path)

    @app.capability("foo")
    async def handler(ctx, args):
        return {"ok": True}

    assert "ks-mcp-x.foo" in app._capabilities
    entry = app._capabilities["ks-mcp-x.foo"]
    assert entry.handler is handler


def test_capability_must_be_async(tmp_path):
    app = App("ks-mcp-x", manifest_path=str(tmp_path / "missing.yaml"))
    with pytest.raises(TypeError, match="async"):
        @app.capability("foo")
        def sync_handler(ctx, args):
            return {"ok": True}


def test_capability_duplicate_registration_rejected(tmp_path):
    app = App("ks-mcp-x", manifest_path=str(tmp_path / "missing.yaml"))

    @app.capability("foo")
    async def first(ctx, args):
        return {}

    with pytest.raises(ValueError, match="已经注册"):
        @app.capability("foo")
        async def second(ctx, args):
            return {}


def test_create_app_validates_manifest_alignment(tmp_path):
    """create_app() 时校验：所有 @app.capability 注册的 canonical_name 必须在 manifest 内。"""
    manifest_path = _write_manifest(tmp_path, """
        provides:
          capabilities:
            - name: foo
              execution_mode: sync
              backend:
                kind: mcp_tool
                tool_name: foo
    """)
    app = App("ks-mcp-x", manifest_path=manifest_path)

    @app.capability("bar")  # bar 不在 manifest 内
    async def handler(ctx, args):
        return {}

    with pytest.raises(ManifestMismatch) as exc_info:
        app.create_app()
    assert exc_info.value.registered == "ks-mcp-x.bar"
    assert "ks-mcp-x.foo" in exc_info.value.manifest_names


def test_orphan_mcp_tool_without_handler_raises_no_backing(tmp_path):
    """manifest 声明 mcp_tool capability 但既无 handler 也无同名 @app.tool：四象限判「无承载」→ raise。

    BREAKING（复用四象限，否 tool × 否 handler）：旧契约是 warn-not-error
    （容忍多实例生产容器载入同一份完整 manifest），现改为启动期 ValueError。多实例拆分
    部署需各实例用 per-instance 裁剪 manifest，只声明本实例实际承载的 capability。
    """
    manifest_path = _write_manifest(tmp_path, """
        provides:
          capabilities:
            - name: foo
              execution_mode: sync
              backend:
                kind: mcp_tool
                tool_name: foo
    """)
    app = App("ks-mcp-x", manifest_path=manifest_path)

    with pytest.raises(ValueError):
        app.create_app()


def test_capability_decorator_returns_handler_unchanged(tmp_path):
    """decorator 应返回原函数本身（与 @app.tool 一致）。"""
    app = App("ks-mcp-x", manifest_path=str(tmp_path / "missing.yaml"))

    @app.capability("foo")
    async def handler(ctx, args):
        return {"x": 1}

    assert handler.__name__ == "handler"
    # 直接调用应仍是 coroutine
    import asyncio
    result = asyncio.run(handler(None, {}))
    assert result == {"x": 1}


def test_capability_decorator_accepts_bare_name_and_derives_canonical():
    app = App("ks-mcp-x")

    @app.capability("web_search")
    async def _h(ctx, args):
        return {}

    assert "ks-mcp-x.web_search" in app._capabilities
    assert app._capabilities["ks-mcp-x.web_search"].name == "web_search"
    assert app._capabilities["ks-mcp-x.web_search"].canonical_name == "ks-mcp-x.web_search"
