"""mount_static_root：业务前端 dist 挂到 MCP 根路径 `/`。

对应 Go SDK MountStaticRoot；仅 config_mode='none' 时允许调用，与 mount_config_ui 互斥。
open_mode='fullpage' 场景下业务主界面由 keystone 前端通过反代承载。
"""
import pytest
from starlette.testclient import TestClient

from ks_app import App


def test_mount_static_root_success(monkeypatch):
    """设 config_mode='none' 后调用成功，_static_root_dir 被写入。"""
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    app = App("my-app", manifest_path="nonexistent.yaml")
    app.set_config_mode("none")
    app.mount_static_root("/tmp/dist")
    assert app._static_root_dir == "/tmp/dist"


def test_mount_static_root_requires_config_mode_none(monkeypatch):
    """未设 config_mode 或设为 schema/iframe 时调用抛 ValueError。"""
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    # 未调 set_config_mode
    app1 = App("my-app", manifest_path="nonexistent.yaml")
    with pytest.raises(ValueError, match="mount_static_root 只能在 config_mode='none'"):
        app1.mount_static_root("/tmp/dist")

    # config_mode='schema'
    app2 = App("my-app", manifest_path="nonexistent.yaml")
    app2.set_config_mode("schema")
    with pytest.raises(ValueError, match="mount_static_root 只能在 config_mode='none'"):
        app2.mount_static_root("/tmp/dist")

    # config_mode='iframe'
    app3 = App("my-app", manifest_path="nonexistent.yaml")
    app3.set_config_mode("iframe")
    with pytest.raises(ValueError, match="mount_static_root 只能在 config_mode='none'"):
        app3.mount_static_root("/tmp/dist")


def test_mount_static_root_excludes_mount_config_ui(monkeypatch):
    """先 mount_config_ui 再 mount_static_root 抛 RuntimeError。"""
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    app = App("my-app", manifest_path="nonexistent.yaml")
    app.mount_config_ui("/tmp/ui")
    app.set_config_mode("none")
    with pytest.raises(RuntimeError, match="mount_static_root 与 mount_config_ui 互斥"):
        app.mount_static_root("/tmp/dist")


def test_mount_config_ui_excludes_mount_static_root(monkeypatch):
    """先 mount_static_root 再 mount_config_ui 抛 RuntimeError。"""
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    app = App("my-app", manifest_path="nonexistent.yaml")
    app.set_config_mode("none")
    app.mount_static_root("/tmp/dist")
    with pytest.raises(RuntimeError, match="mount_config_ui 与 mount_static_root 互斥"):
        app.mount_config_ui("/tmp/ui")


def test_mount_static_root_serves_files(tmp_path, monkeypatch):
    """端到端：GET / 返回 index.html、GET 静态资源正常、GET 未知路径 SPA fallback 到 index.html。"""
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")

    # 造 dist 结构：index.html + assets/main.js
    (tmp_path / "index.html").write_text("<!doctype html><title>SPA</title>")
    assets_dir = tmp_path / "assets"
    assets_dir.mkdir()
    (assets_dir / "main.js").write_text("console.log('hello');")

    app = App("my-app", manifest_path="nonexistent.yaml")
    app.set_config_mode("none")
    app.mount_static_root(str(tmp_path))

    starlette_app = app.create_app()
    client = TestClient(starlette_app)

    # GET / → index.html
    r = client.get("/")
    assert r.status_code == 200
    assert "SPA" in r.text

    # GET /assets/main.js → js 内容
    r = client.get("/assets/main.js")
    assert r.status_code == 200
    assert "console.log" in r.text

    # GET /unknown-spa-route → fallback 到 index.html（StaticFiles html=True）
    r = client.get("/unknown-spa-route")
    assert r.status_code == 200
    assert "SPA" in r.text

    # 具体路径 /healthz /meta 不被静态服务兜住（linear route 优先匹配更具体的）
    r = client.get("/healthz")
    assert r.status_code == 200
    assert r.headers.get("content-type") == "application/json"

    r = client.get("/meta")
    assert r.status_code == 200
    assert r.headers.get("content-type") == "application/json"
