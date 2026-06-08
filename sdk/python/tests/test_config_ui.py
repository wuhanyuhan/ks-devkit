"""mount_config_ui 静态文件挂载 + /meta 反射契约。"""
from starlette.testclient import TestClient

from ks_app import App


def test_mount_config_ui_serves_static(tmp_path, monkeypatch):
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    (tmp_path / "index.html").write_text("<html><body>UI</body></html>")

    app = App("my-app", manifest_path="nonexistent.yaml")
    app.mount_config_ui(str(tmp_path))

    starlette_app = app.create_app()
    client = TestClient(starlette_app)
    r = client.get("/config-ui/")
    assert r.status_code == 200
    assert "UI" in r.text


def test_meta_reports_config_ui(tmp_path, monkeypatch):
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    (tmp_path / "index.html").write_text("x")

    app = App("my-app", manifest_path="nonexistent.yaml")
    app.mount_config_ui(str(tmp_path))

    starlette_app = app.create_app()
    client = TestClient(starlette_app)
    r = client.get("/meta")
    assert r.status_code == 200
    data = r.json()
    assert data.get("config_ui") == {"enabled": True, "url": "/config-ui/"}


def test_mount_config_ui_custom_path(tmp_path, monkeypatch):
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    (tmp_path / "index.html").write_text("y")

    app = App("my-app", manifest_path="nonexistent.yaml")
    app.mount_config_ui(str(tmp_path), path="/ui")

    starlette_app = app.create_app()
    client = TestClient(starlette_app)
    r = client.get("/ui/")
    assert r.status_code == 200

    meta = client.get("/meta").json()
    assert meta["config_ui"] == {"enabled": True, "url": "/ui/"}


def test_manifest_config_ui_fallback(tmp_path, monkeypatch):
    """若 manifest.yaml 定义了 config_ui.path，/meta 优先用 manifest 的值（归一到 {enabled,url}，A5）。"""
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")

    manifest = tmp_path / "manifest.yaml"
    manifest.write_text("""
id: t
name: t
type: service
mount:
  service:
    config_ui:
      path: /declared-ui/
""")
    app = App("my-app", manifest_path=str(manifest))
    starlette_app = app.create_app()
    client = TestClient(starlette_app)
    meta = client.get("/meta").json()
    assert meta["config_ui"] == {"enabled": True, "url": "/declared-ui/"}
