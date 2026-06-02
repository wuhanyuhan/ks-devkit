"""契约护栏：SSE 路径在 JWKSAuthMiddleware 共存时不被干扰。

未来若改中间件导致 SSE 被缓冲或被误拦截，本测试立即暴露。
"""
import asyncio
import os

from starlette.responses import StreamingResponse
from starlette.testclient import TestClient

from ks_app import App


async def _sse_generator():
    for i in range(3):
        yield f"data: event-{i}\n\n"
        await asyncio.sleep(0.01)


async def _sse_endpoint(request):
    return StreamingResponse(_sse_generator(), media_type="text/event-stream")


def test_sse_streams_progressively_with_jwks_enabled(monkeypatch):
    """insecure 跳过鉴权，验证 middleware 不干扰 SSE 自定义路径。"""
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    app = App("test", keystone_auth=True, manifest_path="nonexistent.yaml")
    app.handle("/sse/events", _sse_endpoint, methods=["GET"])

    starlette_app = app.create_app()
    client = TestClient(starlette_app)

    with client.stream("GET", "/sse/events") as response:
        assert response.status_code == 200
        assert response.headers["content-type"].startswith("text/event-stream")
        content = "".join(chunk for chunk in response.iter_text())
        assert "event-0" in content
        assert "event-1" in content
        assert "event-2" in content


def test_sse_not_blocked_by_middleware_strict_mode(monkeypatch):
    """严格模式下 /sse/... 不在 /mcp 前缀，middleware 应裸放行。"""
    monkeypatch.delenv("KS_APP_AUTH_MODE", raising=False)
    monkeypatch.setenv("KEYSTONE_JWKS_URL", "http://unused.example.com/jwks")
    app = App("test", keystone_auth=True, manifest_path="nonexistent.yaml")
    app.handle("/sse/events", _sse_endpoint, methods=["GET"])

    starlette_app = app.create_app()
    client = TestClient(starlette_app)
    r = client.get("/sse/events")
    assert r.status_code == 200


def test_mcp_still_blocked_by_middleware_strict_mode(monkeypatch):
    """对偶：同样 strict 模式下 /mcp 无 JWT 必须被拒（防止 middleware 被误关）。"""
    monkeypatch.delenv("KS_APP_AUTH_MODE", raising=False)
    monkeypatch.setenv("KEYSTONE_JWKS_URL", "http://unused.example.com/jwks")
    app = App("test", keystone_auth=True, manifest_path="nonexistent.yaml")

    starlette_app = app.create_app()
    client = TestClient(starlette_app)
    r = client.post("/mcp", json={"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
    assert r.status_code == 401
