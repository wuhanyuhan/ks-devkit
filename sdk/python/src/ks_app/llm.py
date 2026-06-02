"""Keystone LLM Relay 客户端（relay-only scope）。

通过 Keystone 网关调用大模型，无需自行管理 API key。
要求 manifest 声明 permissions.llm: host_proxy，Keystone 安装应用时会注入 KS_RELAY_TOKEN 环境变量。

direct 模式不在 SDK 范围内：开发者想直连 OpenAI/Anthropic 请自由选用 openai-python /
anthropic SDK / litellm / httpx 等现有库。
"""
import base64
import json
import os
from dataclasses import dataclass, field
from typing import Any, AsyncIterator, NoReturn, Optional

import httpx


# ── 模块级常量 ─────────────────────────────────────────────────────

# LLM 调用可能长尾（长输出 / 上游慢），120s 作为 httpx 超时兜底。
DEFAULT_LLM_TIMEOUT = 120.0


# ── 异常类 ───────────────────────────────────────────────────────────


class LLMNotConfiguredError(RuntimeError):
    """KS_RELAY_TOKEN 未配置。通常意味着 manifest 未声明 permissions.llm: host_proxy
    或本地开发未注入 token。"""


class LLMUnauthorizedError(RuntimeError):
    """网关返回 401/403，relay token 无效。"""


class LLMRateLimitedError(RuntimeError):
    """网关返回 429，被限流。"""


class LLMUpstreamError(RuntimeError):
    """网关返回 5xx 或上游其他错误。"""


class LLMCapabilityUnavailableError(RuntimeError):
    """网关返回 422 capability_unavailable：现场无满足所需能力（如 vision）的模型。
    调用方据此自行降级——偷偷路由到没该能力的模型比报错更糟。
    与 capability-mesh 的 ks_app.errors.CapabilityUnavailable（503 dispatch 域）区分。"""

    def __init__(self, message: str, *, missing: Optional[list[str]] = None):
        self.missing = missing or []
        super().__init__(message)


# ── 核心类型（字段与 keystone/pkg/llmclient/types.go JSON tag 对齐） ────


@dataclass
class ChatRequest:
    """一次聊天请求。

    注：
    - messages 用 list[dict] 而非 list[Message]，直接对齐 OpenAI JSON 结构，
      减少业务侧 dataclass 转换负担
    - temperature 用 Optional[float] 支持显式 0
    - stream 字段由 SDK 在 stream_chat() 中强制置 True，业务无需设置
    """

    messages: list[dict]
    model: str = ""
    tools: Optional[list[dict]] = None
    temperature: Optional[float] = None
    max_tokens: int = 0
    stream: bool = False
    request_options: Optional[dict[str, Any]] = None
    require_capabilities: Optional[list[str]] = None
    tier: str = ""
    reasoning: str = ""

    def to_dict(self) -> dict:
        """序列化为 snake_case JSON dict。省略未设置（None / 零值）字段。"""
        data: dict = {"messages": self.messages}
        if self.model:
            data["model"] = self.model
        if self.tools:
            data["tools"] = self.tools
        if self.temperature is not None:
            data["temperature"] = self.temperature
        if self.max_tokens:
            data["max_tokens"] = self.max_tokens
        if self.stream:
            data["stream"] = self.stream
        opts = dict(self.request_options) if self.request_options else {}
        for cap in self.require_capabilities or []:
            opts["%s_required" % cap] = True
        if self.tier:
            opts["tier"] = self.tier
        if self.reasoning:
            opts["reasoning_mode"] = self.reasoning
        if opts:
            data["request_options"] = opts
        return data


@dataclass
class Message:
    """对话消息。当前 content 只支持 str；list[ContentPart] 未来扩展。"""

    role: str
    content: Any  # str 或 list[ContentPart]
    tool_calls: Optional[list["ToolCall"]] = None
    tool_call_id: Optional[str] = None
    name: Optional[str] = None


@dataclass
class ToolCall:
    """非流式响应中一次完整工具调用。"""

    id: str
    type: str
    function: "ToolCallFunction"


@dataclass
class ToolCallFunction:
    name: str = ""
    arguments: str = ""


@dataclass
class ToolCallDelta:
    """流式工具调用增量片段。"""

    index: int = 0
    id: str = ""
    type: str = ""
    function: "ToolCallFunction" = field(default_factory=lambda: ToolCallFunction())


@dataclass
class Usage:
    prompt_tokens: int = 0
    completion_tokens: int = 0
    total_tokens: int = 0


@dataclass
class ChatResponse:
    content: str = ""
    finish_reason: str = ""
    tool_calls: list[ToolCall] = field(default_factory=list)
    usage: Usage = field(default_factory=Usage)


@dataclass
class Chunk:
    """流式增量块。"""

    delta_content: str = ""
    finish_reason: str = ""
    tool_calls_delta: list[ToolCallDelta] = field(default_factory=list)
    usage: Optional[Usage] = None


# ── 客户端 ──────────────────────────────────────────────────────────


class LLMClient:
    """Keystone LLM Relay 客户端。

    使用示例（manifest 声明 permissions.llm: host_proxy，Keystone 装机时自动注入 KS_RELAY_TOKEN）::

        client = app.llm()
        resp = await client.chat(ChatRequest(
            messages=[{"role": "user", "content": "你好"}],
        ))
        print(resp.content)
    """

    def __init__(self) -> None:
        # KS_GATEWAY_URL 空字符串或未设时都兜底为默认（与 Go 侧 if == "" 对齐）
        self._gateway_url = (os.environ.get("KS_GATEWAY_URL") or "http://localhost:9988").rstrip("/")
        # 兼容两个 env 名：
        #   KS_RELAY_TOKEN        —— SDK 历史规范（用户手填 / standalone 调试）
        #   KEYSTONE_RELAY_TOKEN  —— keystone 平台安装时注入的 token env 名，
        #     self-fetch 拉到 os.environ 后也是这个名字。
        # 两个都没设时下面 chat()/stream_chat() 抛 LLMNotConfiguredError。
        self._relay_token = (
            os.environ.get("KS_RELAY_TOKEN")
            or os.environ.get("KEYSTONE_RELAY_TOKEN")
            or ""
        )

    async def chat(self, req: ChatRequest) -> ChatResponse:
        """发送非流式聊天请求到 keystone relay 端点。"""
        if not self._relay_token:
            raise LLMNotConfiguredError(
                "KS_RELAY_TOKEN 未设置，请确认 manifest 声明 permissions.llm: host_proxy"
            )

        # 非流式
        req.stream = False
        body = req.to_dict()

        url = f"{self._gateway_url}/v1/mcp/relay/chat/completions"
        headers = {
            "Content-Type": "application/json",
            "Authorization": f"Bearer {self._relay_token}",
        }

        async with httpx.AsyncClient(timeout=DEFAULT_LLM_TIMEOUT) as client:
            resp = await client.post(url, json=body, headers=headers)

        if resp.status_code != 200:
            _raise_http_error(resp.status_code, resp.text)

        try:
            data = resp.json()
        except ValueError as e:
            raise LLMUpstreamError(f"解析响应失败: {e}") from e

        return _parse_chat_response(data)

    async def vision_chat(
        self,
        prompt: str,
        images: list[bytes | str],
        *,
        mime: str = "image/png",
        system: str = "",
        temperature: Optional[float] = 0.2,
        max_tokens: int = 0,
        model: str = "",
    ) -> str:
        """带一张或多张图片问模型，返回文本。自动声明 vision 能力路由。

        组装 messages = [system?] + user(text_part(prompt), image_part(每张, mime))，
        置 require_capabilities=["vision"]，调 self.chat()，返回 resp.content.strip()。
        images 为空抛 ValueError；失败按既有类型化异常抛出、不吞错。
        """
        if not images:
            raise ValueError("vision_chat 需要至少一张图片")
        messages: list[dict] = []
        if system:
            messages.append({"role": "system", "content": system})
        content = [text_part(prompt)] + [image_part(img, mime=mime) for img in images]
        messages.append({"role": "user", "content": content})
        req = ChatRequest(
            messages=messages,
            model=model,
            temperature=temperature,
            max_tokens=max_tokens,
            require_capabilities=["vision"],
        )
        resp = await self.chat(req)
        return (resp.content or "").strip()

    async def stream_chat(self, req: ChatRequest) -> AsyncIterator[Chunk]:
        """发送流式聊天请求，async iterator 返回增量 Chunk。"""
        if not self._relay_token:
            raise LLMNotConfiguredError(
                "KS_RELAY_TOKEN 未设置，请确认 manifest 声明 permissions.llm: host_proxy"
            )

        # 强制 stream=true
        req.stream = True
        body = req.to_dict()

        url = f"{self._gateway_url}/v1/mcp/relay/chat/completions"
        headers = {
            "Content-Type": "application/json",
            "Accept": "text/event-stream",
            "Authorization": f"Bearer {self._relay_token}",
        }

        async with httpx.AsyncClient(timeout=DEFAULT_LLM_TIMEOUT) as client:
            async with client.stream("POST", url, json=body, headers=headers) as resp:
                if resp.status_code != 200:
                    # 读取错误 body 后分类
                    err_body = await resp.aread()
                    _raise_http_error(resp.status_code, err_body.decode(errors="replace"))

                async for line in resp.aiter_lines():
                    chunk = _parse_sse_line(line)
                    if chunk is not None:
                        yield chunk


# ── module-level helpers ─────────────────────────────────────────────


def _raise_http_error(status_code: int, body: str) -> NoReturn:
    """按状态码分类异常。"""
    body_short = body[:500] if body else ""
    msg = f"status={status_code} body={body_short}"
    if status_code in (401, 403):
        raise LLMUnauthorizedError(msg)
    if status_code == 429:
        raise LLMRateLimitedError(msg)
    if status_code == 422:
        missing = _parse_capability_missing(body)
        if missing is not None:
            raise LLMCapabilityUnavailableError(msg, missing=missing)
    raise LLMUpstreamError(msg)


def _parse_capability_missing(body: str) -> Optional[list[str]]:
    """从 422 body 解析 capability_unavailable 的 missing 列表；非该形态返回 None。"""
    try:
        data = json.loads(body)
    except (ValueError, TypeError):
        return None
    if isinstance(data, dict) and data.get("code") == "capability_unavailable":
        missing = data.get("missing") or []
        return [str(m) for m in missing]
    return None


def _parse_chat_response(data: dict) -> ChatResponse:
    """把 OpenAI compatible 非流式响应 JSON 解析为 ChatResponse。"""
    resp = ChatResponse()
    choices = data.get("choices", [])
    if choices:
        ch = choices[0]
        msg = ch.get("message", {})
        resp.content = msg.get("content", "") or ""
        resp.finish_reason = ch.get("finish_reason", "")

        raw_calls = msg.get("tool_calls") or []
        for tc in raw_calls:
            fn = tc.get("function", {})
            resp.tool_calls.append(ToolCall(
                id=tc.get("id", ""),
                type=tc.get("type", ""),
                function=ToolCallFunction(
                    name=fn.get("name", ""),
                    arguments=fn.get("arguments", ""),
                ),
            ))

    usage = data.get("usage") or {}
    resp.usage = Usage(
        prompt_tokens=usage.get("prompt_tokens", 0),
        completion_tokens=usage.get("completion_tokens", 0),
        total_tokens=usage.get("total_tokens", 0),
    )
    return resp


def _parse_sse_line(line: str) -> Optional[Chunk]:
    """解析一行 SSE（aiter_lines 已 strip 末尾换行）。

    data: [DONE] / 空行 / 无 data: 前缀 / JSON 失败 / 无实质内容 → 返回 None。
    合法 chunk → 返回 Chunk。
    """
    if not line.startswith("data: "):
        return None
    payload = line[len("data: "):]
    if payload == "[DONE]":
        return None
    try:
        raw = json.loads(payload)
    except ValueError:
        return None

    chunk = Chunk()
    choices = raw.get("choices") or []
    if choices:
        delta = choices[0].get("delta") or {}
        chunk.delta_content = delta.get("content", "") or ""
        chunk.finish_reason = choices[0].get("finish_reason", "") or ""

        for tc in delta.get("tool_calls") or []:
            fn = tc.get("function") or {}
            chunk.tool_calls_delta.append(ToolCallDelta(
                index=int(tc.get("index", 0)),
                id=tc.get("id", "") or "",
                type=tc.get("type", "") or "",
                function=ToolCallFunction(
                    name=fn.get("name", "") or "",
                    arguments=fn.get("arguments", "") or "",
                ),
            ))

    usage = raw.get("usage")
    if usage:
        chunk.usage = Usage(
            prompt_tokens=usage.get("prompt_tokens", 0),
            completion_tokens=usage.get("completion_tokens", 0),
            total_tokens=usage.get("total_tokens", 0),
        )

    # 无实质内容的 chunk 跳过（例如只有 role 无 content 的 first chunk）
    if (
        not chunk.delta_content
        and not chunk.finish_reason
        and not chunk.tool_calls_delta
        and chunk.usage is None
    ):
        return None

    return chunk


def text_part(text: str) -> dict:
    """构造 OpenAI 文本内容块。"""
    return {"type": "text", "text": text}


def image_part(source: bytes | str, *, mime: str = "image/png") -> dict:
    """构造 OpenAI 图片内容块。

    source 为 bytes → base64 data-URI（用 mime）；
    source 为 http(s):// 或 data: 开头的字符串 → 原样作 image_url.url。
    """
    if isinstance(source, bytes):
        b64 = base64.b64encode(source).decode("ascii")
        url = "data:%s;base64,%s" % (mime, b64)
    else:
        url = source
    return {"type": "image_url", "image_url": {"url": url}}
