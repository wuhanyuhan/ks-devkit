"""on_startup / on_shutdown lifespan 钩子契约测试。"""
import pytest
from starlette.testclient import TestClient

from ks_app import App


def test_on_startup_called_before_requests():
    app = App("test", manifest_path="nonexistent.yaml")
    calls = []

    @app.on_startup
    async def startup():
        calls.append("startup")

    @app.on_shutdown
    async def shutdown():
        calls.append("shutdown")

    starlette_app = app.create_app()
    with TestClient(starlette_app) as client:
        r = client.get("/healthz")
        assert r.status_code == 200
        assert calls == ["startup"]
    # TestClient 的 __exit__ 触发 shutdown
    assert calls == ["startup", "shutdown"]


def test_multiple_on_startup_run_in_order():
    app = App("test", manifest_path="nonexistent.yaml")
    calls = []

    @app.on_startup
    async def first():
        calls.append("first")

    @app.on_startup
    async def second():
        calls.append("second")

    starlette_app = app.create_app()
    with TestClient(starlette_app):
        pass
    assert calls == ["first", "second"]


def test_multiple_on_shutdown_run_in_reverse_order():
    app = App("test", manifest_path="nonexistent.yaml")
    calls = []

    @app.on_shutdown
    async def first():
        calls.append("first")

    @app.on_shutdown
    async def second():
        calls.append("second")

    starlette_app = app.create_app()
    with TestClient(starlette_app):
        pass
    # 反序：后注册的先执行
    assert calls == ["second", "first"]


def test_on_startup_rejects_sync_function():
    app = App("test", manifest_path="nonexistent.yaml")

    with pytest.raises(TypeError):

        @app.on_startup
        def not_async():
            pass


def test_on_shutdown_rejects_sync_function():
    app = App("test", manifest_path="nonexistent.yaml")

    with pytest.raises(TypeError):

        @app.on_shutdown
        def not_async():
            pass
