"""ks_app LLM Relay 客户端测试（relay-only scope）。"""
import json
import os
from pathlib import Path

import httpx
import pytest
import respx

from ks_app.llm import (
    ChatRequest,
    ChatResponse,
    Chunk,
    LLMClient,
    LLMNotConfiguredError,
    LLMRateLimitedError,
    LLMUnauthorizedError,
    LLMUpstreamError,
    Message,
    ToolCall,
    ToolCallDelta,
    Usage,
    image_part,
    text_part,
)


def test_new_llm_client_reads_env(monkeypatch):
    monkeypatch.setenv("KS_GATEWAY_URL", "http://test-gateway:9988")
    monkeypatch.setenv("KS_RELAY_TOKEN", "token-abc")

    c = LLMClient()
    assert c._gateway_url == "http://test-gateway:9988"
    assert c._relay_token == "token-abc"


def test_new_llm_client_default_gateway_url(monkeypatch):
    monkeypatch.delenv("KS_GATEWAY_URL", raising=False)
    monkeypatch.setenv("KS_RELAY_TOKEN", "token")

    c = LLMClient()
    assert c._gateway_url == "http://localhost:9988"


@pytest.mark.asyncio
async def test_chat_raises_when_no_token(monkeypatch):
    monkeypatch.delenv("KS_RELAY_TOKEN", raising=False)
    c = LLMClient()
    with pytest.raises(LLMNotConfiguredError):
        await c.chat(ChatRequest(messages=[{"role": "user", "content": "hi"}]))


@pytest.mark.asyncio
async def test_stream_chat_raises_when_no_token(monkeypatch):
    monkeypatch.delenv("KS_RELAY_TOKEN", raising=False)
    c = LLMClient()
    with pytest.raises(LLMNotConfiguredError):
        async for _ in c.stream_chat(ChatRequest(messages=[{"role": "user", "content": "hi"}])):
            pass


def test_chat_request_serializes_snake_case():
    req = ChatRequest(
        model="gpt-4o",
        messages=[{"role": "user", "content": "hi"}],
        temperature=0.7,
        max_tokens=100,
    )
    data = req.to_dict()
    assert data["model"] == "gpt-4o"
    assert data["temperature"] == 0.7
    assert data["max_tokens"] == 100
    assert data["messages"][0]["role"] == "user"


def test_chat_request_omits_none_and_zero():
    req = ChatRequest(messages=[{"role": "user", "content": "hi"}])
    data = req.to_dict()
    assert "model" not in data
    assert "temperature" not in data
    assert "max_tokens" not in data
    assert "tools" not in data


@pytest.mark.asyncio
@respx.mock
async def test_chat_success(monkeypatch):
    monkeypatch.setenv("KS_GATEWAY_URL", "http://gw")
    monkeypatch.setenv("KS_RELAY_TOKEN", "tk")

    route = respx.post("http://gw/v1/mcp/relay/chat/completions").mock(
        return_value=httpx.Response(
            200,
            json={
                "object": "chat.completion",
                "choices": [
                    {
                        "index": 0,
                        "message": {"role": "assistant", "content": "你好！"},
                        "finish_reason": "stop",
                    }
                ],
                "usage": {"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8},
            },
        )
    )

    c = LLMClient()
    resp = await c.chat(ChatRequest(
        model="gpt-4o",
        messages=[{"role": "user", "content": "你好"}],
        temperature=0.5,
        max_tokens=100,
    ))

    assert route.called
    req = route.calls.last.request
    assert req.headers["authorization"] == "Bearer tk"
    body = json.loads(req.content)
    assert body["model"] == "gpt-4o"
    assert body["temperature"] == 0.5
    assert body["max_tokens"] == 100
    assert body.get("stream") is not True  # chat 非流式

    assert resp.content == "你好！"
    assert resp.finish_reason == "stop"
    assert resp.usage.total_tokens == 8


@pytest.mark.asyncio
@respx.mock
async def test_chat_tool_calls_response(monkeypatch):
    monkeypatch.setenv("KS_RELAY_TOKEN", "t")
    respx.post("http://localhost:9988/v1/mcp/relay/chat/completions").mock(
        return_value=httpx.Response(
            200,
            json={
                "object": "chat.completion",
                "choices": [
                    {
                        "index": 0,
                        "message": {
                            "role": "assistant",
                            "content": "",
                            "tool_calls": [
                                {
                                    "id": "call_1",
                                    "type": "function",
                                    "function": {
                                        "name": "get_weather",
                                        "arguments": '{"city":"北京"}',
                                    },
                                }
                            ],
                        },
                        "finish_reason": "tool_calls",
                    }
                ],
                "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
            },
        )
    )

    c = LLMClient()
    resp = await c.chat(ChatRequest(messages=[{"role": "user", "content": "北京天气"}]))

    assert len(resp.tool_calls) == 1
    tc = resp.tool_calls[0]
    assert tc.id == "call_1"
    assert tc.function.name == "get_weather"
    assert "北京" in tc.function.arguments
    assert resp.finish_reason == "tool_calls"


@pytest.mark.asyncio
@respx.mock
@pytest.mark.parametrize("code,exc", [
    (401, LLMUnauthorizedError),
    (403, LLMUnauthorizedError),
    (429, LLMRateLimitedError),
    (500, LLMUpstreamError),
    (502, LLMUpstreamError),
])
async def test_chat_error_classification(monkeypatch, code, exc):
    monkeypatch.setenv("KS_RELAY_TOKEN", "t")
    respx.post("http://localhost:9988/v1/mcp/relay/chat/completions").mock(
        return_value=httpx.Response(code, text="err body")
    )
    c = LLMClient()
    with pytest.raises(exc) as info:
        await c.chat(ChatRequest(messages=[{"role": "user", "content": "x"}]))
    assert str(code) in str(info.value)


@pytest.mark.asyncio
@respx.mock
async def test_chat_invalid_json(monkeypatch):
    monkeypatch.setenv("KS_RELAY_TOKEN", "t")
    respx.post("http://localhost:9988/v1/mcp/relay/chat/completions").mock(
        return_value=httpx.Response(200, text="not a json")
    )
    c = LLMClient()
    with pytest.raises(LLMUpstreamError) as info:
        await c.chat(ChatRequest(messages=[{"role": "user", "content": "x"}]))
    assert "解析" in str(info.value)


def test_raise_http_error_422_capability_unavailable():
    from ks_app.llm import LLMCapabilityUnavailableError, _raise_http_error

    body = '{"code":"capability_unavailable","missing":["vision"],"message":"无 vision 模型"}'
    with pytest.raises(LLMCapabilityUnavailableError) as ei:
        _raise_http_error(422, body)
    assert ei.value.missing == ["vision"]


def test_raise_http_error_422_non_capability_falls_through_to_upstream():
    from ks_app.llm import LLMUpstreamError, _raise_http_error

    with pytest.raises(LLMUpstreamError):
        _raise_http_error(422, '{"code":"some_other_validation"}')


# ── stream_chat 测试（共享 fixture 跨语言回归） ──────────────────────

# 从 sdk/python/tests/ 回溯到 ks-devkit 根下的 sdk/shared-fixtures
SHARED_FIXTURES = Path(__file__).parent.parent.parent / "shared-fixtures" / "sse"


def _load_fixture(name: str) -> tuple[bytes, list[dict]]:
    sse = (SHARED_FIXTURES / f"{name}.sse").read_bytes()
    # fixture 命名规则是 "编号-场景.sse" + "编号-expected-chunks.json"
    number = name.split("-", 1)[0]
    expected = json.loads((SHARED_FIXTURES / f"{number}-expected-chunks.json").read_text())
    return sse, expected


def _assert_chunk_matches(got: Chunk, want: dict, index: int) -> None:
    """chunk 断言：want 中有的字段必须在 got 中精确匹配（空串也算"存在"）。

    got 的额外零值字段（want 未声明的键）允许。tool_calls_delta 按 want 中
    实际出现的键投影 got，保证"want 有的 got 有对齐、got 多余允许"的语义。
    """
    if "delta_content" in want:
        assert got.delta_content == want["delta_content"], f"chunk[{index}] delta_content"
    if "finish_reason" in want:
        assert got.finish_reason == want["finish_reason"], f"chunk[{index}] finish_reason"
    if "tool_calls_delta" in want:
        want_tcs = want["tool_calls_delta"]
        assert len(got.tool_calls_delta) == len(want_tcs), (
            f"chunk[{index}] tool_calls_delta 数量"
        )
        for j, (d, w) in enumerate(zip(got.tool_calls_delta, want_tcs)):
            # 按 want 实际有的键比较，保持"空串也要相等"的语义
            if "index" in w:
                assert d.index == w["index"], f"chunk[{index}].tool_calls_delta[{j}] index"
            if "id" in w:
                assert d.id == w["id"], f"chunk[{index}].tool_calls_delta[{j}] id"
            if "type" in w:
                assert d.type == w["type"], f"chunk[{index}].tool_calls_delta[{j}] type"
            if "function" in w:
                wf = w["function"]
                if "name" in wf:
                    assert d.function.name == wf["name"], (
                        f"chunk[{index}].tool_calls_delta[{j}].function.name"
                    )
                if "arguments" in wf:
                    assert d.function.arguments == wf["arguments"], (
                        f"chunk[{index}].tool_calls_delta[{j}].function.arguments"
                    )
    if "usage" in want:
        assert got.usage is not None
        assert got.usage.prompt_tokens == want["usage"]["prompt_tokens"]
        assert got.usage.completion_tokens == want["usage"]["completion_tokens"]
        assert got.usage.total_tokens == want["usage"]["total_tokens"]


@pytest.mark.asyncio
@respx.mock
@pytest.mark.parametrize("name", ["01-text-only", "02-tool-calls", "03-with-usage"])
async def test_stream_chat_fixture(monkeypatch, name):
    monkeypatch.setenv("KS_RELAY_TOKEN", "t")
    sse_bytes, expected = _load_fixture(name)

    respx.post("http://localhost:9988/v1/mcp/relay/chat/completions").mock(
        return_value=httpx.Response(
            200,
            content=sse_bytes,
            headers={"Content-Type": "text/event-stream"},
        )
    )

    c = LLMClient()
    chunks: list[Chunk] = []
    async for ch in c.stream_chat(ChatRequest(messages=[{"role": "user", "content": "x"}])):
        chunks.append(ch)

    assert len(chunks) == len(expected), f"chunk 数：实际 {len(chunks)} 预期 {len(expected)}"

    for i, (got, want) in enumerate(zip(chunks, expected)):
        _assert_chunk_matches(got, want, index=i)


@pytest.mark.asyncio
@respx.mock
async def test_stream_chat_error_status(monkeypatch):
    monkeypatch.setenv("KS_RELAY_TOKEN", "t")
    respx.post("http://localhost:9988/v1/mcp/relay/chat/completions").mock(
        return_value=httpx.Response(429)
    )
    c = LLMClient()
    with pytest.raises(LLMRateLimitedError):
        async for _ in c.stream_chat(ChatRequest(messages=[{"role": "user", "content": "x"}])):
            pass


@pytest.mark.asyncio
@respx.mock
async def test_stream_chat_forces_stream_true(monkeypatch):
    monkeypatch.setenv("KS_RELAY_TOKEN", "t")
    route = respx.post("http://localhost:9988/v1/mcp/relay/chat/completions").mock(
        return_value=httpx.Response(
            200,
            content=b"data: [DONE]\n\n",
            headers={"Content-Type": "text/event-stream"},
        )
    )

    c = LLMClient()
    async for _ in c.stream_chat(ChatRequest(
        messages=[{"role": "user", "content": "x"}],
        stream=False,  # 用户传 False，SDK 必须覆盖
    )):
        pass

    body = json.loads(route.calls.last.request.content)
    assert body["stream"] is True


def test_text_part():
    assert text_part("hi") == {"type": "text", "text": "hi"}


def test_image_part_bytes_to_data_uri():
    import base64

    part = image_part(b"\x89PNG", mime="image/png")
    assert part["type"] == "image_url"
    expected = "data:image/png;base64," + base64.b64encode(b"\x89PNG").decode("ascii")
    assert part["image_url"]["url"] == expected


def test_image_part_http_url_passthrough():
    part = image_part("https://example.com/x.png")
    assert part["image_url"]["url"] == "https://example.com/x.png"


def test_image_part_data_uri_passthrough():
    part = image_part("data:image/jpeg;base64,QUJD")
    assert part["image_url"]["url"] == "data:image/jpeg;base64,QUJD"


def test_require_capabilities_vision_maps_to_request_options():
    req = ChatRequest(messages=[{"role": "user", "content": "hi"}], require_capabilities=["vision"])
    data = req.to_dict()
    assert data["request_options"]["vision_required"] is True


def test_require_capabilities_tool_calls():
    req = ChatRequest(messages=[{"role": "user", "content": "hi"}], require_capabilities=["tool_calls"])
    assert req.to_dict()["request_options"]["tool_calls_required"] is True


def test_require_capabilities_generic_convention():
    req = ChatRequest(messages=[{"role": "user", "content": "hi"}], require_capabilities=["audio"])
    assert req.to_dict()["request_options"]["audio_required"] is True


def test_require_capabilities_merges_with_existing_request_options():
    req = ChatRequest(
        messages=[{"role": "user", "content": "hi"}],
        request_options={"reasoning_mode": "on"},
        require_capabilities=["vision"],
    )
    opts = req.to_dict()["request_options"]
    assert opts["reasoning_mode"] == "on"
    assert opts["vision_required"] is True


def test_no_capabilities_omits_request_options():
    req = ChatRequest(messages=[{"role": "user", "content": "hi"}])
    assert "request_options" not in req.to_dict()


def test_tier_maps_to_request_options():
    req = ChatRequest(messages=[{"role": "user", "content": "hi"}], tier="flagship")
    assert req.to_dict()["request_options"]["tier"] == "flagship"


def test_reasoning_maps_to_reasoning_mode():
    req = ChatRequest(messages=[{"role": "user", "content": "hi"}], reasoning="on")
    assert req.to_dict()["request_options"]["reasoning_mode"] == "on"


def test_tier_reasoning_capability_merge_into_request_options():
    req = ChatRequest(
        messages=[{"role": "user", "content": "hi"}],
        tier="economy",
        reasoning="off",
        require_capabilities=["vision"],
    )
    opts = req.to_dict()["request_options"]
    assert opts["tier"] == "economy"
    assert opts["reasoning_mode"] == "off"
    assert opts["vision_required"] is True


def test_no_intent_omits_request_options():
    req = ChatRequest(messages=[{"role": "user", "content": "hi"}])
    assert "request_options" not in req.to_dict()


def test_explicit_request_options_reasoning_preserved_when_field_unset():
    # 不设 reasoning 字段、只在 request_options 里给 reasoning_mode → 原样保留（不被空字段覆盖）
    req = ChatRequest(
        messages=[{"role": "user", "content": "hi"}],
        request_options={"reasoning_mode": "auto"},
    )
    assert req.to_dict()["request_options"]["reasoning_mode"] == "auto"


@pytest.mark.asyncio
async def test_vision_chat_assembles_message_and_sets_vision(monkeypatch):
    from unittest.mock import AsyncMock

    monkeypatch.setenv("KS_RELAY_TOKEN", "t")
    c = LLMClient()
    c.chat = AsyncMock(return_value=ChatResponse(content="  一只猫  "))
    out = await c.vision_chat("描述", [b"\x89PNG"], mime="image/png")
    assert out == "一只猫"
    sent = c.chat.await_args.args[0]
    assert sent.require_capabilities == ["vision"]
    content = sent.messages[0]["content"]
    assert content[0] == {"type": "text", "text": "描述"}
    assert content[1]["type"] == "image_url"
    assert content[1]["image_url"]["url"].startswith("data:image/png;base64,")
    assert sent.to_dict()["request_options"]["vision_required"] is True


@pytest.mark.asyncio
async def test_vision_chat_with_system_prepends_system_message(monkeypatch):
    from unittest.mock import AsyncMock

    monkeypatch.setenv("KS_RELAY_TOKEN", "t")
    c = LLMClient()
    c.chat = AsyncMock(return_value=ChatResponse(content="x"))
    await c.vision_chat("p", [b"img"], system="你是助手")
    sent = c.chat.await_args.args[0]
    assert sent.messages[0] == {"role": "system", "content": "你是助手"}
    assert sent.messages[1]["role"] == "user"


@pytest.mark.asyncio
async def test_vision_chat_multiple_images(monkeypatch):
    from unittest.mock import AsyncMock

    monkeypatch.setenv("KS_RELAY_TOKEN", "t")
    c = LLMClient()
    c.chat = AsyncMock(return_value=ChatResponse(content="ok"))
    await c.vision_chat("两张图", [b"a", b"b"], mime="image/jpeg")
    content = c.chat.await_args.args[0].messages[0]["content"]
    assert len(content) == 3  # text + 2 images
    assert content[1]["image_url"]["url"].startswith("data:image/jpeg;base64,")
    assert content[2]["image_url"]["url"].startswith("data:image/jpeg;base64,")


@pytest.mark.asyncio
async def test_vision_chat_empty_images_raises(monkeypatch):
    monkeypatch.setenv("KS_RELAY_TOKEN", "t")
    c = LLMClient()
    with pytest.raises(ValueError, match="至少一张图片"):
        await c.vision_chat("p", [])


@pytest.mark.asyncio
async def test_vision_chat_propagates_chat_error(monkeypatch):
    from unittest.mock import AsyncMock

    monkeypatch.setenv("KS_RELAY_TOKEN", "t")
    c = LLMClient()
    c.chat = AsyncMock(side_effect=LLMUpstreamError("boom"))
    with pytest.raises(LLMUpstreamError):
        await c.vision_chat("p", [b"img"])
