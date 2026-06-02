/**
 * Keystone LLM Relay 客户端（relay-only scope）。
 *
 * 通过 Keystone 网关调用大模型，无需自行管理 API key。
 * 要求 manifest 声明 permissions.llm: host_proxy，Keystone 安装应用时注入 KS_RELAY_TOKEN。
 *
 * direct 模式不在 SDK 范围内：开发者想直连请自由选用 openai / anthropic / litellm 等库。
 */

// ── 异常类 ────────────────────────────────────────────────────────

export class LLMNotConfiguredError extends Error {
  constructor(message = "KS_RELAY_TOKEN 未设置，请确认 manifest 声明 permissions.llm: host_proxy") {
    super(message);
    this.name = "LLMNotConfiguredError";
  }
}

export class LLMUnauthorizedError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "LLMUnauthorizedError";
  }
}

export class LLMRateLimitedError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "LLMRateLimitedError";
  }
}

export class LLMUpstreamError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "LLMUpstreamError";
  }
}

// ── 核心类型（字段 snake_case，对齐 keystone/pkg/llmclient/types.go） ────

export interface Message {
  role: string;
  content: unknown; // string 或 ContentPart[]（当前仅 string）
  tool_calls?: ToolCall[];
  tool_call_id?: string;
  name?: string;
}

export interface ToolCall {
  id: string;
  type: string;
  function: ToolCallFunction;
}

export interface ToolCallFunction {
  name: string;
  arguments: string;
}

export interface ToolCallDelta {
  index: number;
  id?: string;
  type?: string;
  function: Partial<ToolCallFunction>;
}

export interface Usage {
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
}

export interface ChatRequest {
  messages: Message[];
  model?: string;
  tools?: Record<string, unknown>[];
  temperature?: number;
  max_tokens?: number;
  stream?: boolean;
  request_options?: Record<string, unknown>;
}

export interface ChatResponse {
  content: string;
  finish_reason: string;
  tool_calls: ToolCall[];
  usage: Usage;
}

export interface Chunk {
  delta_content?: string;
  finish_reason?: string;
  tool_calls_delta?: ToolCallDelta[];
  usage?: Usage;
}

// ── 序列化辅助 ──

/** 移除值为 undefined 的顶层字段；用于请求体 omit-unset。 */
export function serializeChatRequest(req: ChatRequest): Record<string, unknown> {
  const out: Record<string, unknown> = { messages: req.messages };
  if (req.model) out.model = req.model;
  if (req.tools && req.tools.length > 0) out.tools = req.tools;
  if (req.temperature !== undefined) out.temperature = req.temperature;
  if (req.max_tokens && req.max_tokens > 0) out.max_tokens = req.max_tokens;
  if (req.stream) out.stream = req.stream;
  if (req.request_options) out.request_options = req.request_options;
  return out;
}

// ── 客户端 ──────────────────────────────────────────────────────────

export class LLMClient {
  private readonly gatewayUrl: string;
  private readonly relayToken: string;

  constructor() {
    // KS_GATEWAY_URL 空串或未设时都兜底为默认；末尾斜杠 trim，避免拼接产生 http://host//v1/... 双斜杠
    const raw = process.env.KS_GATEWAY_URL || "http://localhost:9988";
    this.gatewayUrl = raw.replace(/\/+$/, "");
    this.relayToken = process.env.KS_RELAY_TOKEN || "";
  }

  async chat(req: ChatRequest): Promise<ChatResponse> {
    if (!this.relayToken) {
      throw new LLMNotConfiguredError();
    }

    // 非流式：serializeChatRequest 对 stream=false 的 falsy 值默认省略，不会写入 body；
    // 上游按非流式解析。无需再 delete body.stream。
    const body = serializeChatRequest({ ...req, stream: false });

    const url = `${this.gatewayUrl}/v1/mcp/relay/chat/completions`;
    let resp: Response;
    try {
      resp = await fetch(url, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${this.relayToken}`,
        },
        body: JSON.stringify(body),
      });
    } catch (e) {
      throw new LLMUpstreamError(`请求 LLM 网关失败: ${e}`);
    }

    if (!resp.ok) {
      const text = await resp.text();
      throwHttpError(resp.status, text);
    }

    let data: any;
    try {
      data = await resp.json();
    } catch (e) {
      throw new LLMUpstreamError(`解析响应失败: ${e}`);
    }

    return parseChatResponse(data);
  }

  async *streamChat(req: ChatRequest): AsyncGenerator<Chunk> {
    if (!this.relayToken) {
      throw new LLMNotConfiguredError();
    }

    // 强制 stream=true 写入 body（即便调用方传 false 也覆盖）
    const body = serializeChatRequest({ ...req, stream: true });
    body.stream = true;

    const url = `${this.gatewayUrl}/v1/mcp/relay/chat/completions`;
    let resp: Response;
    try {
      resp = await fetch(url, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Accept: "text/event-stream",
          Authorization: `Bearer ${this.relayToken}`,
        },
        body: JSON.stringify(body),
      });
    } catch (e) {
      throw new LLMUpstreamError(`请求 LLM 网关失败: ${e}`);
    }

    if (!resp.ok) {
      const text = await resp.text();
      throwHttpError(resp.status, text);
    }

    if (!resp.body) {
      throw new LLMUpstreamError("响应无 body");
    }

    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";

    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });

        // SSE 以 \n 分隔；流式数据可能不按行边界到达，需要 buffer 拼接
        let idx: number;
        while ((idx = buffer.indexOf("\n")) !== -1) {
          const line = buffer.slice(0, idx);
          buffer = buffer.slice(idx + 1);
          const chunk = parseSSELine(line);
          if (chunk) yield chunk;
        }
      }

      // flush decoder 的内部 pending 字节（防止末端多字节字符被截断）
      buffer += decoder.decode();

      // 处理结束时残留的最后一行
      if (buffer.trim()) {
        const chunk = parseSSELine(buffer);
        if (chunk) yield chunk;
      }
    } finally {
      reader.releaseLock();
    }
  }
}

/** 工厂函数，便于 App.llm() 调用 */
export function createLLMClient(): LLMClient {
  return new LLMClient();
}

// ── HTTP 状态码分类 + 响应解析 ─────────────────────────────────────────

function throwHttpError(status: number, body: string): never {
  const short = body.length > 500 ? body.slice(0, 500) : body;
  const msg = `status=${status} body=${short}`;
  if (status === 401 || status === 403) throw new LLMUnauthorizedError(msg);
  if (status === 429) throw new LLMRateLimitedError(msg);
  throw new LLMUpstreamError(msg);
}

function parseChatResponse(data: any): ChatResponse {
  const resp: ChatResponse = {
    content: "",
    finish_reason: "",
    tool_calls: [],
    usage: { prompt_tokens: 0, completion_tokens: 0, total_tokens: 0 },
  };
  const choices = data.choices || [];
  if (choices.length > 0) {
    const ch = choices[0];
    const msg = ch.message || {};
    resp.content = msg.content || "";
    resp.finish_reason = ch.finish_reason || "";
    const rawCalls = msg.tool_calls || [];
    for (const tc of rawCalls) {
      const fn = tc.function || {};
      resp.tool_calls.push({
        id: tc.id || "",
        type: tc.type || "",
        function: { name: fn.name || "", arguments: fn.arguments || "" },
      });
    }
  }
  if (data.usage) {
    resp.usage = {
      prompt_tokens: data.usage.prompt_tokens || 0,
      completion_tokens: data.usage.completion_tokens || 0,
      total_tokens: data.usage.total_tokens || 0,
    };
  }
  return resp;
}

/**
 * 解析单行 SSE：`data: {...}` → Chunk；`data: [DONE]` / 空行 / 非 data: 前缀 → null。
 * 无内容的 chunk（首个 role-only delta）同样过滤为 null。
 */
function parseSSELine(line: string): Chunk | null {
  if (!line.startsWith("data: ")) return null;
  const payload = line.slice("data: ".length);
  if (payload === "[DONE]") return null;

  let raw: any;
  try {
    raw = JSON.parse(payload);
  } catch {
    return null;
  }

  const chunk: Chunk = {};
  const choices = raw.choices || [];
  if (choices.length > 0) {
    const delta = choices[0].delta || {};
    if (delta.content) chunk.delta_content = delta.content;
    if (choices[0].finish_reason) chunk.finish_reason = choices[0].finish_reason;
    if (delta.tool_calls && delta.tool_calls.length > 0) {
      chunk.tool_calls_delta = delta.tool_calls.map((tc: any) => ({
        index: tc.index ?? 0,
        id: tc.id || "",
        type: tc.type || "",
        function: {
          name: tc.function?.name || "",
          arguments: tc.function?.arguments || "",
        },
      }));
    }
  }
  if (raw.usage) {
    chunk.usage = {
      prompt_tokens: raw.usage.prompt_tokens || 0,
      completion_tokens: raw.usage.completion_tokens || 0,
      total_tokens: raw.usage.total_tokens || 0,
    };
  }

  // 空 chunk（如首个 role-only delta）过滤
  if (
    !chunk.delta_content &&
    !chunk.finish_reason &&
    !chunk.tool_calls_delta?.length &&
    !chunk.usage
  ) {
    return null;
  }

  return chunk;
}
