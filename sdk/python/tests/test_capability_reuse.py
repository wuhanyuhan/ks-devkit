import os
import tempfile
import textwrap

import pytest

from ks_app.app import App


def _app_with_manifest(body: str, app_id: str = "ks-mcp-x") -> App:
    fd, path = tempfile.mkstemp(suffix=".yaml")
    with os.fdopen(fd, "w") as f:
        f.write(textwrap.dedent(body))
    return App(app_id, manifest_path=path)


def test_reuse_existing_tool_does_not_raise():
    app = _app_with_manifest("""
        id: ks-mcp-browser
        provides:
          capabilities:
            - name: web_search
              execution_mode: sync
              backend: {kind: mcp_tool, tool_name: web_search}
    """, app_id="ks-mcp-browser")

    @app.tool("web_search", "原子工具")
    async def _t(**params):
        return {"hit": True}

    app.create_app()  # 不应 raise（复用）
    assert "web_search" in app._tools


def test_generate_new_tool_carries_input_schema():
    app = _app_with_manifest("""
        id: ks-mcp-x
        provides:
          capabilities:
            - name: gen
              execution_mode: sync
              backend: {kind: mcp_tool, tool_name: gen_tool}
              input_schema: {type: object, properties: {q: {type: string}}}
    """)

    @app.capability("gen")
    async def _h(ctx, args):
        return {}

    app.create_app()
    assert app._tools["gen_tool"]["input_schema"] == {
        "type": "object", "properties": {"q": {"type": "string"}},
    }


def test_conflict_handler_and_tool_raises():
    app = _app_with_manifest("""
        id: ks-mcp-x
        provides:
          capabilities:
            - name: dup
              execution_mode: sync
              backend: {kind: mcp_tool, tool_name: dup_tool}
    """)

    @app.tool("dup_tool", "已有")
    async def _t(**params):
        return {}

    @app.capability("dup")
    async def _h(ctx, args):
        return {}

    with pytest.raises(ValueError):
        app.create_app()


def test_no_backend_no_handler_raises():
    app = _app_with_manifest("""
        id: ks-mcp-x
        provides:
          capabilities:
            - name: orphan
              execution_mode: sync
              backend: {kind: mcp_tool, tool_name: missing_tool}
    """)
    with pytest.raises(ValueError):
        app.create_app()
