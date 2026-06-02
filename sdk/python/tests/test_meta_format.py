"""/meta 响应格式契约测试 —— 对齐 ks-types.MetaResponse。"""
import pytest
from starlette.testclient import TestClient

from ks_app import App


def test_meta_has_required_fields(monkeypatch):
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    app = App("my-app", keystone_auth=False, version="1.2.3", manifest_path="nonexistent.yaml")

    @app.tool(
        "echo",
        "Echo tool",
        input_schema={"type": "object", "properties": {"msg": {"type": "string"}}},
    )
    async def echo(msg: str = ""):
        return {"msg": msg}

    starlette_app = app.create_app()
    client = TestClient(starlette_app)
    r = client.get("/meta")
    assert r.status_code == 200
    data = r.json()
    assert data["name"] == "my-app"
    assert data["version"] == "1.2.3"
    assert data["auth_mode"] == "none"
    tools = data["tools"]
    assert len(tools) == 1
    assert tools[0]["name"] == "echo"
    assert tools[0]["description"] == "Echo tool"
    assert tools[0]["input_schema"] == {
        "type": "object",
        "properties": {"msg": {"type": "string"}},
    }


def test_meta_reports_keystone_auth_mode(monkeypatch):
    monkeypatch.setenv("KEYSTONE_JWKS_URL", "http://x.invalid/jwks")
    monkeypatch.delenv("KS_APP_AUTH_MODE", raising=False)
    app = App("svc", keystone_auth=True, version="0.3.0", manifest_path="nonexistent.yaml")
    starlette_app = app.create_app()
    client = TestClient(starlette_app)
    r = client.get("/meta")
    assert r.status_code == 200
    data = r.json()
    assert data["auth_mode"] == "keystone_jwks"


def test_meta_tool_without_input_schema_omits_field(monkeypatch):
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    app = App("svc", manifest_path="nonexistent.yaml")

    @app.tool("no_schema", "")
    async def no_schema():
        return {}

    starlette_app = app.create_app()
    client = TestClient(starlette_app)
    r = client.get("/meta")
    assert r.status_code == 200
    tools = r.json()["tools"]
    assert tools[0]["name"] == "no_schema"
    assert "input_schema" not in tools[0]


def test_meta_no_config_ui_when_not_mounted(monkeypatch):
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    app = App("svc", manifest_path="nonexistent.yaml")
    starlette_app = app.create_app()
    client = TestClient(starlette_app)
    r = client.get("/meta")
    assert "config_ui" not in r.json()


# ---------------------------------------------------------------------------
# v0.4.0 新增 5 字段（对齐 ks-types v0.5.0 MetaResponse）的契约测试
# ---------------------------------------------------------------------------


def test_meta_v040_declare_nav_appears_in_response(monkeypatch):
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    app = App("svc", manifest_path="nonexistent.yaml")
    app.declare_nav(
        label="文生图",
        category="应用",
        open_mode="fullpage",
        icon="image",
        order=10,
        entry_path="/",
        required_perms=["mcp.image-gen.use"],
    )
    starlette_app = app.create_app()
    client = TestClient(starlette_app)
    data = client.get("/meta").json()
    assert "nav" in data
    nav = data["nav"]
    assert nav["label"] == "文生图"
    assert nav["category"] == "应用"
    assert nav["open_mode"] == "fullpage"
    assert nav["icon"] == "image"
    assert nav["order"] == 10
    assert nav["entry_path"] == "/"
    assert nav["required_perms"] == ["mcp.image-gen.use"]

    # 反向：未调时缺省
    app2 = App("svc2", manifest_path="nonexistent.yaml")
    data2 = TestClient(app2.create_app()).get("/meta").json()
    assert "nav" not in data2


def test_meta_v040_declare_permission_appears_in_response(monkeypatch):
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    app = App("svc", manifest_path="nonexistent.yaml")
    app.declare_permission(code="mcp.image-gen.use", label="使用文生图", default_roles=["admin"])
    app.declare_permission(code="mcp.image-gen.admin", label="管理文生图")
    starlette_app = app.create_app()
    client = TestClient(starlette_app)
    data = client.get("/meta").json()
    assert "permissions" in data
    perms = data["permissions"]
    assert isinstance(perms, list) and len(perms) == 2
    assert perms[0] == {"code": "mcp.image-gen.use", "label": "使用文生图", "default_roles": ["admin"]}
    assert perms[1] == {"code": "mcp.image-gen.admin", "label": "管理文生图"}
    assert "default_roles" not in perms[1]

    # 反向：未调（空 list）按 omitempty 缺省
    app2 = App("svc2", manifest_path="nonexistent.yaml")
    data2 = TestClient(app2.create_app()).get("/meta").json()
    assert "permissions" not in data2


def test_meta_v040_set_config_mode_appears_in_response(monkeypatch):
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    for mode in ("schema", "iframe", "none"):
        app = App("svc", manifest_path="nonexistent.yaml")
        app.set_config_mode(mode)
        data = TestClient(app.create_app()).get("/meta").json()
        assert data["config_mode"] == mode

    # 反向：未调缺省
    app2 = App("svc", manifest_path="nonexistent.yaml")
    data2 = TestClient(app2.create_app()).get("/meta").json()
    assert "config_mode" not in data2


def test_meta_v040_set_config_mode_validates_enum(monkeypatch):
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    app = App("svc", manifest_path="nonexistent.yaml")
    with pytest.raises(ValueError):
        app.set_config_mode("xxx")


def test_meta_v040_set_protocol_version_and_config_status_appear_in_response(monkeypatch):
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    app = App("svc", manifest_path="nonexistent.yaml")
    app.set_protocol_version("1.0")
    app.set_config_status("via_frontend")
    data = TestClient(app.create_app()).get("/meta").json()
    assert data["protocol_version"] == "1.0"
    assert data["config_status"] == "via_frontend"

    # 反向：未调缺省
    app2 = App("svc", manifest_path="nonexistent.yaml")
    data2 = TestClient(app2.create_app()).get("/meta").json()
    assert "protocol_version" not in data2
    assert "config_status" not in data2


def test_meta_v040_set_config_status_validates_enum(monkeypatch):
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    app = App("svc", manifest_path="nonexistent.yaml")
    for valid in ("unconfigured", "via_frontend", "via_cli", "mixed"):
        app.set_config_status(valid)  # 不抛
    with pytest.raises(ValueError):
        app.set_config_status("invalid_status")
