"""mcp_tool backend wiring 集成单测：dispatcher 调 MCP tools/call → SDK 路由到 capability handler。"""
import textwrap
from pathlib import Path

import pytest
from starlette.testclient import TestClient

from ks_app import App


def _write_manifest(tmp_path: Path, body: str) -> str:
    p = tmp_path / "manifest.yaml"
    p.write_text(textwrap.dedent(body), encoding="utf-8")
    return str(p)


def test_capability_mcp_tool_backend_routed_via_mcp_tool_call(tmp_path):
    """mcp_tool backend：SDK 把 @capability 注册项自动挂成同名 MCP tool；
    dispatcher 走 MCP tools/call → SDK 路由到 capability handler；
    ctx.user_id / ctx.caller_id / ctx.chain_id 从 _meta.ks_* 还原。
    """
    manifest_path = _write_manifest(tmp_path, """
        provides:
          capabilities:
            - name: foo
              execution_mode: sync
              timeout_ms: 30000
              backend:
                kind: mcp_tool
                tool_name: foo
    """)
    app = App("ks-mcp-x", manifest_path=manifest_path)

    received_ctx = {}

    @app.capability("foo")
    async def foo(ctx, args):
        received_ctx["user_id"] = ctx.user_id
        received_ctx["caller_id"] = ctx.caller_id
        received_ctx["chain_id"] = ctx.chain_id
        received_ctx["canonical_name"] = ctx.canonical_name
        received_ctx["caller_kind"] = ctx.caller_kind
        return {"echo": args.get("input", "")}

    starlette_app = app.create_app()
    client = TestClient(starlette_app)

    resp = client.post("/mcp", json={
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/call",
        "params": {
            "name": "foo",
            "arguments": {"input": "hello"},
            "_meta": {
                "ks_user_id": "100",
                "ks_caller_id": "ks-mcp-writer",
                "ks_caller_kind": "app",
                "ks_chain_id": "chain-abc",
                "ks_request_id": "req-1",
            },
        },
    })
    assert resp.status_code == 200
    assert received_ctx["user_id"] == "100"
    assert received_ctx["caller_id"] == "ks-mcp-writer"
    assert received_ctx["caller_kind"] == "app"
    assert received_ctx["chain_id"] == "chain-abc"
    assert received_ctx["canonical_name"] == "ks-mcp-x.foo"


def test_capability_mcp_tool_no_collision_with_existing_tool(tmp_path):
    """capability 注册的 tool 与既有 @app.tool 注册的 tool 在 tools/list 里并存。"""
    manifest_path = _write_manifest(tmp_path, """
        provides:
          capabilities:
            - name: foo
              execution_mode: sync
              backend:
                kind: mcp_tool
                tool_name: cap_foo
    """)
    app = App("ks-mcp-x", manifest_path=manifest_path)

    @app.tool("legacy_foo", "legacy MCP tool")
    async def legacy(input: str = ""):
        return {"legacy": input}

    @app.capability("foo")
    async def cap_handler(ctx, args):
        return {"cap": args.get("input", "")}

    starlette_app = app.create_app()
    client = TestClient(starlette_app)

    resp = client.post("/mcp", json={
        "jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": {},
    })
    assert resp.status_code == 200
    body = resp.json()
    tool_names = {t["name"] for t in body["result"]["tools"]}
    assert "legacy_foo" in tool_names
    assert "cap_foo" in tool_names


def test_capability_mcp_tool_name_collision_with_legacy_tool_rejected(tmp_path):
    """同名冲突：@app.tool('foo') + @capability(... backend.tool_name='foo') 必须启动期失败。"""
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

    @app.tool("foo", "existing")
    async def existing(input: str = ""):
        return {}

    @app.capability("foo")
    async def cap_foo(ctx, args):
        return {}

    with pytest.raises(ValueError, match="tool_name.*已被.*tool"):
        app.create_app()


def test_capability_mcp_tool_backend_missing_tool_name_rejected(tmp_path):
    """mcp_tool backend 但 manifest 没写 backend.tool_name → 启动期失败。"""
    manifest_path = _write_manifest(tmp_path, """
        provides:
          capabilities:
            - name: foo
              execution_mode: sync
              backend:
                kind: mcp_tool
    """)
    app = App("ks-mcp-x", manifest_path=manifest_path)

    @app.capability("foo")
    async def cap_foo(ctx, args):
        return {}

    with pytest.raises(ValueError, match="backend.tool_name"):
        app.create_app()
