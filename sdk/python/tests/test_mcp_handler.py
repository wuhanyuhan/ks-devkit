"""测试 ks_app.mcpproto 模块的 MCP Streamable HTTP 端点。

覆盖：
- initialize / tools/list / tools/call 三个标准方法
- handler 异常的 sanitization（敏感片段不得回到客户端）
- 通知（无 id）必须返回 202 No Body
- params / name 缺失的精确错误消息
- ContextVar 调用前后的隔离
"""

import json
import logging

import pytest
from starlette.applications import Starlette
from starlette.testclient import TestClient

from ks_app.context import _reset_meta, get_context
from ks_app.mcpproto import MCP_PROTOCOL_VERSION, mcp_route


def _make_client(tools: dict, app_id: str = "test-app", app_version: str = "0.1.0") -> TestClient:
    """构造一个仅挂载 mcp_route 的 TestClient，最小依赖。"""
    routes = [mcp_route(app_id, app_version, tools)]
    return TestClient(Starlette(routes=routes))


@pytest.fixture(autouse=True)
def _clean_context():
    """每个测试前后清空 ContextVar，避免相互污染。"""
    _reset_meta()
    yield
    _reset_meta()


# ---------- initialize ----------

def test_mcp_initialize():
    client = _make_client({})
    resp = client.post("/mcp", json={
        "jsonrpc": "2.0",
        "id": 1,
        "method": "initialize",
        "params": {},
    })
    assert resp.status_code == 200
    body = resp.json()
    assert body["jsonrpc"] == "2.0"
    assert body["id"] == 1
    result = body["result"]
    assert result["protocolVersion"] == MCP_PROTOCOL_VERSION
    assert result["capabilities"] == {"tools": {}}
    assert result["serverInfo"]["name"] == "test-app"
    assert result["serverInfo"]["version"] == "0.1.0"


# ---------- tools/list ----------

def test_mcp_tools_list():
    async def greet(name: str = "world"):
        return f"hello {name}"

    tools = {"greet": {"handler": greet, "description": "打招呼"}}
    client = _make_client(tools)

    resp = client.post("/mcp", json={
        "jsonrpc": "2.0",
        "id": 2,
        "method": "tools/list",
        "params": {},
    })
    assert resp.status_code == 200
    body = resp.json()
    tools_list = body["result"]["tools"]
    assert len(tools_list) == 1
    t = tools_list[0]
    assert t["name"] == "greet"
    assert t["description"] == "打招呼"
    schema = t["inputSchema"]
    assert schema["type"] == "object"
    assert schema["properties"]["name"] == {"type": "string", "default": "world"}
    # 全部参数都有默认值，不应有 required 键
    assert "required" not in schema


def test_mcp_tools_list_empty():
    client = _make_client({})
    resp = client.post("/mcp", json={
        "jsonrpc": "2.0",
        "id": 3,
        "method": "tools/list",
        "params": {},
    })
    assert resp.status_code == 200
    assert resp.json()["result"]["tools"] == []


def test_mcp_tools_list_uses_explicit_input_schema():
    """App.tool(input_schema=...) 显式传入的 schema 必须原样透传到 tools/list，
    不再走自动推导（自动推导无法表达 description / enum / array.items）。

    回归测试：在此修复前，streamable.py 硬编码用 schema_from_func，
    导致显式 schema 被静默丢弃，下游 LLM 拿不到丰富类型信息。
    """
    explicit_schema = {
        "type": "object",
        "properties": {
            "topics": {
                "type": "array",
                "items": {"type": "string"},
                "description": "搜索主题列表",
            },
            "platform": {
                "type": "string",
                "enum": ["wechat", "zhihu"],
                "description": "目标平台",
            },
        },
        "required": ["topics"],
        "additionalProperties": False,
    }

    async def search(topics, platform="wechat"):
        return []

    tools = {
        "search_news": {
            "handler": search,
            "description": "搜索新闻",
            "input_schema": explicit_schema,
        }
    }
    client = _make_client(tools)

    resp = client.post("/mcp", json={
        "jsonrpc": "2.0",
        "id": 100,
        "method": "tools/list",
        "params": {},
    })
    body = resp.json()
    t = body["result"]["tools"][0]
    assert t["inputSchema"] == explicit_schema


def test_mcp_tools_list_falls_back_to_auto_schema():
    """input_schema 为 None 时仍走自动推导（向后兼容现有未声明 schema 的工具）。"""
    async def add(a: int, b: int = 0):
        return a + b

    tools = {
        "add": {
            "handler": add,
            "description": "加法",
            "input_schema": None,
        }
    }
    client = _make_client(tools)

    resp = client.post("/mcp", json={
        "jsonrpc": "2.0",
        "id": 101,
        "method": "tools/list",
        "params": {},
    })
    schema = resp.json()["result"]["tools"][0]["inputSchema"]
    assert schema["properties"]["a"] == {"type": "integer"}
    assert schema["properties"]["b"] == {"type": "integer", "default": 0}
    assert schema["required"] == ["a"]


# ---------- tools/call ----------

def test_mcp_tools_call_success():
    async def add(a: int, b: int):
        return {"sum": a + b}

    tools = {"add": {"handler": add, "description": "加法"}}
    client = _make_client(tools)

    resp = client.post("/mcp", json={
        "jsonrpc": "2.0",
        "id": 4,
        "method": "tools/call",
        "params": {"name": "add", "arguments": {"a": 1, "b": 2}},
    })
    assert resp.status_code == 200
    body = resp.json()
    content = body["result"]["content"]
    assert len(content) == 1
    assert content[0]["type"] == "text"
    # dict 结果会被 JSON 序列化进 text 字段
    inner = json.loads(content[0]["text"])
    assert inner == {"sum": 3}


def test_mcp_tools_call_string_result():
    """string 结果应直接走 text 类型，不进行额外的 JSON wrap。"""
    async def hello():
        return "hello"

    tools = {"hello": {"handler": hello, "description": ""}}
    client = _make_client(tools)

    resp = client.post("/mcp", json={
        "jsonrpc": "2.0",
        "id": 5,
        "method": "tools/call",
        "params": {"name": "hello", "arguments": {}},
    })
    assert resp.status_code == 200
    content = resp.json()["result"]["content"]
    assert content == [{"type": "text", "text": "hello"}]


def test_mcp_tools_call_none_result():
    """None 结果应序列化为字符串 'null'，与 Go SDK / json.Marshal(nil) 行为一致。"""
    async def nothing():
        return None

    tools = {"nothing": {"handler": nothing, "description": ""}}
    client = _make_client(tools)

    resp = client.post("/mcp", json={
        "jsonrpc": "2.0",
        "id": 6,
        "method": "tools/call",
        "params": {"name": "nothing", "arguments": {}},
    })
    assert resp.status_code == 200
    content = resp.json()["result"]["content"]
    assert content == [{"type": "text", "text": "null"}]


def test_mcp_tools_call_with_meta():
    """_meta 中的 ks_resource_scope 应被注入到 ContextVar，handler 内通过 get_context() 可读取。"""
    captured = {}

    async def scoped():
        captured["scope"] = get_context().resource_scope
        return "ok"

    tools = {"scoped": {"handler": scoped, "description": ""}}
    client = _make_client(tools)

    client.post("/mcp", json={
        "jsonrpc": "2.0",
        "id": 7,
        "method": "tools/call",
        "params": {
            "name": "scoped",
            "arguments": {},
            "_meta": {"ks_resource_scope": "instance_abc"},
        },
    })
    assert captured["scope"] == "instance_abc"


def test_mcp_tools_call_reads_meta_from_arguments_and_strips_before_handler():
    """Keystone Capability Mesh 有时把 _meta 放入 arguments；SDK 应读取并避免传给业务 handler。"""
    captured = {}

    async def scoped(value: str = ""):
        captured["value"] = value
        captured["user_id"] = get_context().user_id
        captured["request_id"] = get_context().request_id
        return "ok"

    tools = {"scoped": {"handler": scoped, "description": ""}}
    client = _make_client(tools)

    client.post("/mcp", json={
        "jsonrpc": "2.0",
        "id": 17,
        "method": "tools/call",
        "params": {
            "name": "scoped",
            "arguments": {
                "value": "hello",
                "_meta": {"ks_user_id": 7, "ks_request_id": "req-1"},
            },
        },
    })

    assert captured == {"value": "hello", "user_id": "7", "request_id": "req-1"}


def test_mcp_tools_call_meta_reset_after_call():
    """tools/call 结束后 _reset_meta 必须把 ContextVar 清空，下一个无 meta 的请求看到的是空字符串。"""
    captured = []

    async def reader():
        captured.append(get_context().resource_scope)
        return "ok"

    tools = {"reader": {"handler": reader, "description": ""}}
    client = _make_client(tools)

    # 第一次注入 meta
    client.post("/mcp", json={
        "jsonrpc": "2.0",
        "id": 8,
        "method": "tools/call",
        "params": {
            "name": "reader",
            "arguments": {},
            "_meta": {"ks_resource_scope": "first"},
        },
    })
    # 第二次不带 meta
    client.post("/mcp", json={
        "jsonrpc": "2.0",
        "id": 9,
        "method": "tools/call",
        "params": {"name": "reader", "arguments": {}},
    })
    assert captured == ["first", ""]


def test_mcp_tools_call_meta_reset_after_handler_raises():
    """handler 抛错时 _reset_meta 依然必须运行，否则上下文会泄漏到下一个请求。

    这是 try/finally 而非 try/except 的核心原因 —— 一旦失手会让下一个
    请求读到上一次注入的 resource_scope，是 ContextVar 最容易踩的坑。
    """
    captured = []

    async def crash():
        raise RuntimeError("boom")

    async def reader():
        captured.append(get_context().resource_scope)
        return "ok"

    tools = {
        "crash": {"handler": crash, "description": "抛错"},
        "reader": {"handler": reader, "description": "读上下文"},
    }
    client = _make_client(tools)

    # crash 注入了 meta，但 handler 会抛错
    client.post("/mcp", json={
        "jsonrpc": "2.0", "id": 1, "method": "tools/call",
        "params": {
            "name": "crash", "arguments": {},
            "_meta": {"ks_resource_scope": "leaked_scope"},
        },
    })

    # 下一个不带 meta 的请求：reader 必须读到空字符串
    client.post("/mcp", json={
        "jsonrpc": "2.0", "id": 2, "method": "tools/call",
        "params": {"name": "reader", "arguments": {}},
    })
    assert captured == [""], f"上下文泄漏: reader 读到了 {captured[0]!r}"


def test_mcp_tools_call_not_found():
    client = _make_client({})
    resp = client.post("/mcp", json={
        "jsonrpc": "2.0",
        "id": 10,
        "method": "tools/call",
        "params": {"name": "missing", "arguments": {}},
    })
    body = resp.json()
    assert body["error"]["code"] == -32602
    assert "工具不存在" in body["error"]["message"]
    assert "missing" in body["error"]["message"]


def test_mcp_tools_call_handler_error_sanitized(caplog):
    """handler 抛出含敏感信息的异常时：

    - 客户端只能看到固定的 "工具执行失败"
    - 错误码为 _ERR_INTERNAL (-32603)
    - 响应体不包含任何敏感片段
    - 完整异常通过 logging 记录到服务端（caplog 验证）
    """
    async def leaky():
        raise RuntimeError("数据库连接失败: mysql://user:pass@internal-host:3306")

    tools = {"leaky": {"handler": leaky, "description": ""}}
    client = _make_client(tools)

    with caplog.at_level(logging.ERROR, logger="ks_app.mcp"):
        resp = client.post("/mcp", json={
            "jsonrpc": "2.0",
            "id": 11,
            "method": "tools/call",
            "params": {"name": "leaky", "arguments": {}},
        })

    assert resp.status_code == 200
    body = resp.json()
    assert body["error"]["code"] == -32603
    assert body["error"]["message"] == "工具执行失败"

    # 显式断言敏感片段不在响应体的任何位置
    raw = resp.text
    assert "mysql://" not in raw
    assert "internal-host" not in raw
    assert "数据库连接失败" not in raw

    # 服务端日志应包含完整异常（exc_info=True 会写入 stack trace）
    log_text = "\n".join(r.getMessage() + (r.exc_text or "") for r in caplog.records)
    assert "tool handler 执行失败" in log_text
    # 完整异常含敏感信息，但只允许出现在服务端日志，不能出现在响应里
    assert "数据库连接失败" in log_text
    assert "mysql://" in log_text


def test_mcp_tools_call_missing_params():
    """缺 params 字段应明确报 'tools/call 缺少 params 字段'。"""
    client = _make_client({})
    resp = client.post("/mcp", json={
        "jsonrpc": "2.0",
        "id": 12,
        "method": "tools/call",
    })
    body = resp.json()
    assert body["error"]["code"] == -32602
    assert "缺少 params" in body["error"]["message"]


def test_mcp_tools_call_missing_name():
    """params 中缺 name 字段应明确报 'tools/call 缺少 name 参数'。"""
    client = _make_client({})
    resp = client.post("/mcp", json={
        "jsonrpc": "2.0",
        "id": 13,
        "method": "tools/call",
        "params": {"arguments": {}},
    })
    body = resp.json()
    assert body["error"]["code"] == -32602
    assert "缺少 name" in body["error"]["message"]


# ---------- 协议级错误 ----------

def test_mcp_method_not_found():
    client = _make_client({})
    resp = client.post("/mcp", json={
        "jsonrpc": "2.0",
        "id": 14,
        "method": "unknown/method",
        "params": {},
    })
    body = resp.json()
    assert body["error"]["code"] == -32601
    assert "未知方法" in body["error"]["message"]


def test_mcp_invalid_json():
    """非 JSON body 应返回 -32700 ParseError。"""
    client = _make_client({})
    resp = client.post(
        "/mcp",
        content=b"{this is not json",
        headers={"Content-Type": "application/json"},
    )
    body = resp.json()
    assert body["error"]["code"] == -32700
    # parse error 时 id 字段为 null
    assert body["id"] is None


def test_mcp_invalid_jsonrpc_version():
    """jsonrpc 字段不是 '2.0' 时应返回 -32600 InvalidRequest。"""
    client = _make_client({})
    resp = client.post("/mcp", json={
        "jsonrpc": "1.0",
        "id": 15,
        "method": "initialize",
        "params": {},
    })
    body = resp.json()
    assert body["error"]["code"] == -32600


def test_mcp_notification_no_response():
    """JSON-RPC 2.0 §4.1：无 id 字段是通知，必须返回 202 且无 body。

    MCP 客户端在握手期间会发 notifications/initialized，处理不当会导致
    客户端等待响应超时。
    """
    client = _make_client({})
    resp = client.post("/mcp", json={
        "jsonrpc": "2.0",
        "method": "notifications/initialized",
    })
    assert resp.status_code == 202
    assert resp.content == b""


def test_mcp_content_type():
    """正常响应 Content-Type 应为 application/json。"""
    client = _make_client({})
    resp = client.post("/mcp", json={
        "jsonrpc": "2.0",
        "id": 16,
        "method": "initialize",
        "params": {},
    })
    assert resp.headers["content-type"].startswith("application/json")
