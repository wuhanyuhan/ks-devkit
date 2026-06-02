"""SDK 扩展点测试：handle / use / health_check / create_app"""
import pytest
from starlette.testclient import TestClient
from starlette.responses import JSONResponse
from starlette.middleware import Middleware
from starlette.middleware.base import BaseHTTPMiddleware
from ks_app import App


def make_app_with_tool():
    """创建带一个简单工具的 App 实例。"""
    app = App("test")

    @app.tool("greet", "打招呼")
    async def greet(name="world"):
        return {"msg": f"hello {name}"}

    return app


# --- handle ---

def test_handle_custom_route():
    app = make_app_with_tool()

    async def ping(request):
        return JSONResponse({"pong": "ok"})

    app.handle("/api/ping", ping, methods=["GET"])

    client = TestClient(app.create_app())
    resp = client.get("/api/ping")
    assert resp.status_code == 200
    assert resp.json() == {"pong": "ok"}


def test_handle_builtin_routes_still_work():
    app = make_app_with_tool()

    async def custom(request):
        return JSONResponse({"custom": True})

    app.handle("/custom", custom, methods=["GET"])

    client = TestClient(app.create_app())
    # 内置端点仍然正常
    assert client.get("/healthz").status_code == 200
    assert client.get("/readyz").status_code == 200
    assert client.get("/meta").status_code == 200
    assert client.get("/mcp/tools/list").status_code == 200


def test_handle_returns_self():
    app = App("test")

    async def noop(request):
        return JSONResponse({})

    result = app.handle("/x", noop, methods=["GET"])
    assert result is app


# --- use ---

class TrackingMiddleware(BaseHTTPMiddleware):
    calls = []

    async def dispatch(self, request, call_next):
        TrackingMiddleware.calls.append("before")
        response = await call_next(request)
        TrackingMiddleware.calls.append("after")
        return response


def test_use_middleware():
    TrackingMiddleware.calls = []
    app = make_app_with_tool()
    app.use(TrackingMiddleware)

    client = TestClient(app.create_app())
    resp = client.get("/healthz")
    assert resp.status_code == 200
    assert TrackingMiddleware.calls == ["before", "after"]


def test_use_applies_to_custom_routes():
    TrackingMiddleware.calls = []
    app = make_app_with_tool()
    app.use(TrackingMiddleware)

    async def custom(request):
        return JSONResponse({"ok": True})

    app.handle("/custom", custom, methods=["GET"])

    client = TestClient(app.create_app())
    resp = client.get("/custom")
    assert resp.status_code == 200
    assert len(TrackingMiddleware.calls) == 2


def test_use_returns_self():
    app = App("test")
    result = app.use(TrackingMiddleware)
    assert result is app


# --- health_check ---

def test_health_check_all_pass():
    app = make_app_with_tool()
    app.health_check("db", lambda: None)
    app.health_check("cache", lambda: None)

    client = TestClient(app.create_app())
    resp = client.get("/healthz")
    assert resp.status_code == 200
    assert resp.json()["status"] == "ok"


def test_health_check_one_fails():
    app = make_app_with_tool()
    app.health_check("db", lambda: None)

    def bad_check():
        raise RuntimeError("磁盘空间不足")

    app.health_check("disk", bad_check)

    client = TestClient(app.create_app())
    resp = client.get("/healthz")
    assert resp.status_code == 503
    data = resp.json()
    assert data["status"] == "unhealthy"
    assert "disk" in data["checks"]
    assert "磁盘空间不足" in data["checks"]["disk"]
    assert "db" not in data["checks"]


def test_health_check_no_checks_backward_compat():
    app = make_app_with_tool()
    # 不注册任何 health check
    client = TestClient(app.create_app())
    resp = client.get("/healthz")
    assert resp.status_code == 200
    assert resp.json()["status"] == "ok"


def test_health_check_returns_self():
    app = App("test")
    result = app.health_check("x", lambda: None)
    assert result is app


# --- create_app ---

def test_create_app_returns_starlette():
    from starlette.applications import Starlette
    app = make_app_with_tool()
    starlette_app = app.create_app()
    assert isinstance(starlette_app, Starlette)


# --- fluent chaining ---

def test_fluent_chaining():
    async def noop(request):
        return JSONResponse({})

    app = App("test")

    @app.tool("t1", "desc")
    async def t1():
        return {}

    result = (
        app
        .handle("/x", noop, methods=["GET"])
        .use(TrackingMiddleware)
        .health_check("db", lambda: None)
    )

    assert result is app
    assert len(app._custom_routes) == 1
    assert len(app._middlewares) == 1
    assert len(app._health_checks) == 1
