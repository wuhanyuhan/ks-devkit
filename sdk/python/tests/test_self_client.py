"""SelfClient 单元测试（应用自查 / managed-resources self-fetch）。

覆盖 spec managed resources self-fetch contract：
- fetch_env 正常返回 env dict
- 401 / 5xx / 网络错 / 解析错 各自抛 KeystoneSelfFetchError
- Authorization header / URL 拼接 / 末尾斜杠兜底
"""
from __future__ import annotations

import httpx
import pytest
import respx

from ks_app.keystone_client import KeystoneSelfFetchError, SelfClient


# ── 正常路径 ───────────────────────────────────────────────────────


@respx.mock
def test_fetch_env_returns_dict():
    """200 + 标准响应 → 返回 env dict（顶层 data.env）。"""
    respx.get("http://gw:9988/v1/apps/self/resources").mock(
        return_value=httpx.Response(
            200,
            json={
                "code": 0,
                "data": {
                    "app_id": "ks-mcp-writer",
                    "version": "1.0.0",
                    "install_id": 42,
                    "env": {
                        "DB_HOST": "keystone-mysql",
                        "DB_PORT": "3306",
                        "DB_PASSWORD": "secret",
                        "HMAC_SECRET": "hex32",
                    },
                },
            },
        )
    )

    c = SelfClient("http://gw:9988", "ks-app:42:1:1:abc")
    env = c.fetch_env()

    assert env == {
        "DB_HOST": "keystone-mysql",
        "DB_PORT": "3306",
        "DB_PASSWORD": "secret",
        "HMAC_SECRET": "hex32",
    }


@respx.mock
def test_fetch_env_sends_bearer_authorization():
    """请求带 Authorization: Bearer <token> header。"""
    route = respx.get("http://gw:9988/v1/apps/self/resources").mock(
        return_value=httpx.Response(200, json={"code": 0, "data": {"env": {}}})
    )

    SelfClient("http://gw:9988", "ks-app:42:1:1:abc").fetch_env()

    assert route.called
    sent = route.calls.last.request
    assert sent.headers["authorization"] == "Bearer ks-app:42:1:1:abc"


@respx.mock
def test_fetch_env_strips_trailing_slash_from_gateway():
    """gateway_url 末尾斜杠应被兜底，避免 //v1/... 双斜杠。"""
    route = respx.get("http://gw:9988/v1/apps/self/resources").mock(
        return_value=httpx.Response(200, json={"code": 0, "data": {"env": {}}})
    )

    SelfClient("http://gw:9988/", "ks-app:42:1:1:abc").fetch_env()

    assert route.called  # 命中无双斜杠的 URL


@respx.mock
def test_fetch_env_empty_env_returns_empty_dict():
    """env 字段空 dict 也是合法响应，应返回 {}，不抛异常。"""
    respx.get("http://gw:9988/v1/apps/self/resources").mock(
        return_value=httpx.Response(200, json={"code": 0, "data": {"env": {}}})
    )

    env = SelfClient("http://gw:9988", "tok").fetch_env()
    assert env == {}


# ── 失败路径：业务/HTTP 错误码 ─────────────────────────────────────


@respx.mock
def test_fetch_env_401_raises():
    """token 缺/过期/无效 keystone 返回 40101 + http 401，应抛 KeystoneSelfFetchError。"""
    respx.get("http://gw:9988/v1/apps/self/resources").mock(
        return_value=httpx.Response(
            401,
            json={"code": 40101, "message": "invalid app token"},
        )
    )

    with pytest.raises(KeystoneSelfFetchError) as info:
        SelfClient("http://gw:9988", "bad").fetch_env()
    # 错误消息应包含状态码，便于运维定位
    assert "401" in str(info.value)


@respx.mock
def test_fetch_env_5xx_raises():
    """keystone 5xx → KeystoneSelfFetchError。"""
    respx.get("http://gw:9988/v1/apps/self/resources").mock(
        return_value=httpx.Response(503, text="upstream down")
    )

    with pytest.raises(KeystoneSelfFetchError) as info:
        SelfClient("http://gw:9988", "tok").fetch_env()
    assert "503" in str(info.value)


@respx.mock
def test_fetch_env_429_raises():
    """限流 429 → KeystoneSelfFetchError（每 install_id 60/min）。"""
    respx.get("http://gw:9988/v1/apps/self/resources").mock(
        return_value=httpx.Response(429, text="rate limited")
    )

    with pytest.raises(KeystoneSelfFetchError):
        SelfClient("http://gw:9988", "tok").fetch_env()


@respx.mock
def test_fetch_env_business_code_nonzero_raises():
    """HTTP 200 但 code != 0（业务错误）应抛 KeystoneSelfFetchError。"""
    respx.get("http://gw:9988/v1/apps/self/resources").mock(
        return_value=httpx.Response(
            200,
            json={"code": 40004, "message": "install not found"},
        )
    )

    with pytest.raises(KeystoneSelfFetchError) as info:
        SelfClient("http://gw:9988", "tok").fetch_env()
    assert "40004" in str(info.value) or "install not found" in str(info.value)


# ── 失败路径：网络层 / 响应解析 ──────────────────────────────────


@respx.mock
def test_fetch_env_network_error_raises():
    """connection error / DNS 失败 → KeystoneSelfFetchError。"""
    respx.get("http://gw:9988/v1/apps/self/resources").mock(
        side_effect=httpx.ConnectError("conn refused")
    )

    with pytest.raises(KeystoneSelfFetchError):
        SelfClient("http://gw:9988", "tok").fetch_env()


@respx.mock
def test_fetch_env_timeout_raises():
    """httpx 读超时 → KeystoneSelfFetchError。"""
    respx.get("http://gw:9988/v1/apps/self/resources").mock(
        side_effect=httpx.ReadTimeout("read timeout")
    )

    with pytest.raises(KeystoneSelfFetchError):
        SelfClient("http://gw:9988", "tok", timeout=0.1).fetch_env()


@respx.mock
def test_fetch_env_invalid_json_raises():
    """响应不是合法 JSON → KeystoneSelfFetchError。"""
    respx.get("http://gw:9988/v1/apps/self/resources").mock(
        return_value=httpx.Response(200, text="not json")
    )

    with pytest.raises(KeystoneSelfFetchError):
        SelfClient("http://gw:9988", "tok").fetch_env()


@respx.mock
def test_fetch_env_missing_env_field_raises():
    """响应 JSON 合法但缺 data.env 字段 → KeystoneSelfFetchError（schema 错）。"""
    respx.get("http://gw:9988/v1/apps/self/resources").mock(
        return_value=httpx.Response(200, json={"code": 0, "data": {}})
    )

    with pytest.raises(KeystoneSelfFetchError):
        SelfClient("http://gw:9988", "tok").fetch_env()


@respx.mock
def test_fetch_env_env_not_dict_raises():
    """data.env 不是 object/dict → KeystoneSelfFetchError。"""
    respx.get("http://gw:9988/v1/apps/self/resources").mock(
        return_value=httpx.Response(200, json={"code": 0, "data": {"env": "oops"}})
    )

    with pytest.raises(KeystoneSelfFetchError):
        SelfClient("http://gw:9988", "tok").fetch_env()


@respx.mock
def test_fetch_env_values_coerced_to_str():
    """env value 即使后端给了 int 也应被强转成 str（os.environ 只吃 str）。"""
    respx.get("http://gw:9988/v1/apps/self/resources").mock(
        return_value=httpx.Response(
            200,
            json={
                "code": 0,
                "data": {"env": {"DB_PORT": 3306, "DEBUG": True, "NAME": "x"}},
            },
        )
    )

    env = SelfClient("http://gw:9988", "tok").fetch_env()
    assert env == {"DB_PORT": "3306", "DEBUG": "True", "NAME": "x"}
