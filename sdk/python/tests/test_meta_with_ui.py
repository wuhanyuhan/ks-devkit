"""/meta 端点 widgets-protocol-v1 集成测试。

校验 ui_binding 注入到 tools[]._meta.ui，且任意 binding 触发
capabilities.ui.enabled = true（对齐 ks-types v0.6.0 MetaResponse）。
"""
import pytest
from starlette.testclient import TestClient

from ks_app import App
from ks_app.tool_ui import ToolUIBinding


@pytest.fixture
def test_client_with_ui_tool(monkeypatch):
    """注册 1 个 tool 带 ui_binding + 1 个 tool 不带 binding，混合场景。"""
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    app = App("svc", manifest_path="nonexistent.yaml")

    @app.tool(
        "review_draft",
        "审稿",
        ui_binding=ToolUIBinding(widget="ks://widgets/diff-review@v1"),
    )
    async def review_draft():
        return {}

    @app.tool("plain_tool", "无 UI 的工具")
    async def plain_tool():
        return {}

    return TestClient(app.create_app())


@pytest.fixture
def test_client_plain(monkeypatch):
    """所有 tool 都不带 ui_binding。"""
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    app = App("svc", manifest_path="nonexistent.yaml")

    @app.tool("plain_tool", "无 UI")
    async def plain_tool():
        return {}

    return TestClient(app.create_app())


@pytest.fixture
def test_client_with_sandbox_hints(monkeypatch):
    """带 sandbox_hints 的 ui_binding。"""
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    app = App("svc", manifest_path="nonexistent.yaml")

    @app.tool(
        "browse",
        "浏览",
        ui_binding=ToolUIBinding(
            widget="ui://browser/render", sandbox_hints=["allow-downloads"]
        ),
    )
    async def browse():
        return {}

    return TestClient(app.create_app())


# ---------------------------------------------------------------------------
# /meta — 注入与 capabilities
# ---------------------------------------------------------------------------


def test_meta_includes_tool_ui_binding(test_client_with_ui_tool):
    resp = test_client_with_ui_tool.get("/meta")
    assert resp.status_code == 200
    data = resp.json()
    assert data["capabilities"]["ui"]["enabled"] is True

    # tool 顺序按注册顺序
    tools_by_name = {t["name"]: t for t in data["tools"]}
    review = tools_by_name["review_draft"]
    assert review["_meta"]["ui"] == {"widget": "ks://widgets/diff-review@v1"}

    # 不带 binding 的 tool 不应出现 _meta 字段
    plain = tools_by_name["plain_tool"]
    assert "_meta" not in plain


def test_meta_omits_capabilities_when_no_binding(test_client_plain):
    resp = test_client_plain.get("/meta")
    assert resp.status_code == 200
    data = resp.json()
    assert "capabilities" not in data
    assert all("_meta" not in t for t in data["tools"])


def test_meta_includes_sandbox_hints(test_client_with_sandbox_hints):
    resp = test_client_with_sandbox_hints.get("/meta")
    data = resp.json()
    tools_by_name = {t["name"]: t for t in data["tools"]}
    assert tools_by_name["browse"]["_meta"]["ui"] == {
        "widget": "ui://browser/render",
        "sandbox_hints": ["allow-downloads"],
    }
    assert data["capabilities"]["ui"]["enabled"] is True


# ---------------------------------------------------------------------------
# 装饰器签名 — 向后兼容性
# ---------------------------------------------------------------------------


def test_tool_decorator_without_ui_binding_still_works(monkeypatch):
    """老调用（不传 ui_binding）零变化。"""
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    app = App("svc", manifest_path="nonexistent.yaml")

    @app.tool("greet", "打招呼")
    async def greet():
        return {}

    assert "greet" in app._tools
    assert app._tools["greet"]["ui_binding"] is None


def test_tool_decorator_stores_ui_binding(monkeypatch):
    """新参数被存进 _tools entry。"""
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    app = App("svc", manifest_path="nonexistent.yaml")
    binding = ToolUIBinding(widget="ks://widgets/timeline@v1")

    @app.tool("show_timeline", "时间轴", ui_binding=binding)
    async def show():
        return {}

    assert app._tools["show_timeline"]["ui_binding"] is binding


def test_tool_decorator_ui_binding_keyword_only(monkeypatch):
    """ui_binding 必须以关键字方式传（防止位置参数撞 input_schema）。"""
    monkeypatch.setenv("KS_APP_AUTH_MODE", "insecure")
    app = App("svc", manifest_path="nonexistent.yaml")

    # 关键字传 OK
    @app.tool(
        "ok",
        "ok",
        input_schema={"type": "object"},
        ui_binding=ToolUIBinding(widget="ks://widgets/timeline@v1"),
    )
    async def ok():
        return {}

    assert app._tools["ok"]["ui_binding"] is not None
    assert app._tools["ok"]["input_schema"] == {"type": "object"}
