import pytest
from starlette.testclient import TestClient
from ks_app import App


@pytest.fixture
def client():
    app = App("test-app")

    @app.tool("add", "加法")
    async def add(a: int, b: int):
        return {"sum": a + b}

    @app.tool("fail", "always fails")
    async def fail():
        raise RuntimeError("boom")

    @app.tool("leaky", "raises error with sensitive info")
    async def leaky():
        # 模拟真实 handler 可能抛出的"含敏感信息的错误"：数据库 URI / 内部主机 / SQL
        raise RuntimeError("mysql://user:pwd@internal-host:3306 数据库连接失败")

    starlette_app = app.create_app()
    return TestClient(starlette_app)


def test_healthz(client):
    resp = client.get("/healthz")
    assert resp.status_code == 200
    assert resp.json() == {"status": "ok"}


def test_readyz(client):
    resp = client.get("/readyz")
    assert resp.status_code == 200


def test_meta(client):
    resp = client.get("/meta")
    assert resp.status_code == 200
    data = resp.json()
    assert data["name"] == "test-app"
    assert "version" in data
    assert "auth_mode" in data
    assert len(data["tools"]) == 3
    tool_names = {t["name"] for t in data["tools"]}
    assert "add" in tool_names
    assert "fail" in tool_names
    assert "leaky" in tool_names


def test_mcp_tool_call_success(client):
    resp = client.post("/mcp/tools/call", json={"name": "add", "params": {"a": 1, "b": 2}})
    assert resp.status_code == 200
    assert resp.json() == {"result": {"sum": 3}}


def test_mcp_tool_call_not_found(client):
    resp = client.post("/mcp/tools/call", json={"name": "missing", "params": {}})
    assert resp.status_code == 404
    assert "not found" in resp.json()["error"]


def test_mcp_tool_call_handler_error_sanitized(client):
    """handler 抛异常时，客户端收到固定脱敏消息，原始 error 详情不得泄露。

    与 Go SDK sdk/go/mcp_handler.go + TestMCPSensitiveErrorSanitization 保持一致：
    handler 原始错误可能含 SQL / 密码 / 内部路径等敏感信息，必须只记到服务端日志，
    不能直接吐给客户端。
    """
    resp = client.post("/mcp/tools/call", json={"name": "fail", "params": {}})
    assert resp.status_code == 500
    body = resp.json()
    assert body["error"] == "工具执行失败"
    # 显式断言原始 RuntimeError("boom") 的文本未泄露到响应 body
    assert "boom" not in resp.text


def test_mcp_tool_call_sensitive_error_sanitized(client):
    """回归测试：含 mysql URI / 内部主机等敏感片段的原始错误必须被脱敏。"""
    resp = client.post("/mcp/tools/call", json={"name": "leaky", "params": {}})
    assert resp.status_code == 500
    body = resp.json()
    assert body["error"] == "工具执行失败"
    # 以下敏感片段都不得出现在响应 body 中
    assert "mysql://" not in resp.text
    assert "internal-host" not in resp.text
    assert "数据库连接失败" not in resp.text


def test_mcp_tool_call_bad_json(client):
    """body 不是合法 JSON 应当返回 400"""
    resp = client.post(
        "/mcp/tools/call",
        content=b"{this is not json",
        headers={"Content-Type": "application/json"},
    )
    assert resp.status_code == 400
    assert "invalid json" in resp.json()["error"]


def test_mcp_tools_list(client):
    resp = client.get("/mcp/tools/list")
    assert resp.status_code == 200
    data = resp.json()
    assert "tools" in data
    names = {t["name"] for t in data["tools"]}
    assert "add" in names
    assert "fail" in names
    assert "leaky" in names
    for t in data["tools"]:
        assert "name" in t
        assert "description" in t


def test_mcp_tools_list_empty():
    app = App("empty")
    starlette_app = app.create_app()
    c = TestClient(starlette_app)
    resp = c.get("/mcp/tools/list")
    assert resp.status_code == 200
    assert resp.json() == {"tools": []}
