import { describe, expect, it, afterEach, vi } from "vitest";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  ChatRequest,
  Chunk,
  LLMClient,
  LLMNotConfiguredError,
  LLMUnauthorizedError,
  LLMRateLimitedError,
  LLMUpstreamError,
  serializeChatRequest,
} from "./llm.js";

// 保留 fetch 的真实引用，以便测试结束后还原
const realFetch = globalThis.fetch;

function resetEnv() {
  delete process.env.KS_GATEWAY_URL;
  delete process.env.KS_RELAY_TOKEN;
}

afterEach(() => {
  globalThis.fetch = realFetch;
  resetEnv();
});

function stubFetchOnce(
  status: number,
  body: unknown,
  captureTo?: { call?: { url: string; init: any } },
) {
  globalThis.fetch = vi.fn(async (url: any, init: any) => {
    if (captureTo) {
      captureTo.call = { url: String(url), init };
    }
    const bodyStr = typeof body === "string" ? body : JSON.stringify(body);
    return new Response(bodyStr, { status, headers: { "Content-Type": "application/json" } });
  }) as any;
}

describe("LLMClient - constructor + env", () => {
  afterEach(resetEnv);

  it("reads KS_GATEWAY_URL and KS_RELAY_TOKEN", () => {
    process.env.KS_GATEWAY_URL = "http://test-gw";
    process.env.KS_RELAY_TOKEN = "tk";
    const c = new LLMClient();
    expect((c as any).gatewayUrl).toBe("http://test-gw");
    expect((c as any).relayToken).toBe("tk");
  });

  it("defaults to http://localhost:9988", () => {
    process.env.KS_RELAY_TOKEN = "tk";
    const c = new LLMClient();
    expect((c as any).gatewayUrl).toBe("http://localhost:9988");
  });

  it("chat throws LLMNotConfiguredError when no token", async () => {
    const c = new LLMClient();
    await expect(c.chat({ messages: [{ role: "user", content: "hi" }] })).rejects.toBeInstanceOf(
      LLMNotConfiguredError,
    );
  });

  it("streamChat throws LLMNotConfiguredError when no token", async () => {
    const c = new LLMClient();
    const consume = async () => {
      for await (const _ of c.streamChat({ messages: [{ role: "user", content: "hi" }] })) {
        // unreachable
      }
    };
    await expect(consume()).rejects.toBeInstanceOf(LLMNotConfiguredError);
  });
});

describe("serializeChatRequest", () => {
  it("snake_case + omit unset", () => {
    const req: ChatRequest = {
      model: "gpt-4o",
      messages: [{ role: "user", content: "hi" }],
      temperature: 0.7,
      max_tokens: 100,
    };
    const body = serializeChatRequest(req);
    expect(body).toEqual({
      model: "gpt-4o",
      messages: [{ role: "user", content: "hi" }],
      temperature: 0.7,
      max_tokens: 100,
    });
  });

  it("omits unset fields", () => {
    const body = serializeChatRequest({ messages: [{ role: "user", content: "hi" }] });
    expect(body).toEqual({ messages: [{ role: "user", content: "hi" }] });
    expect(body.model).toBeUndefined();
    expect(body.temperature).toBeUndefined();
  });
});

describe("LLMClient.chat", () => {
  it("POST to /v1/mcp/relay/chat/completions with Bearer token", async () => {
    process.env.KS_GATEWAY_URL = "http://gw";
    process.env.KS_RELAY_TOKEN = "tk";
    const captured: any = {};
    stubFetchOnce(
      200,
      {
        object: "chat.completion",
        choices: [
          { index: 0, message: { role: "assistant", content: "你好!" }, finish_reason: "stop" },
        ],
        usage: { prompt_tokens: 5, completion_tokens: 3, total_tokens: 8 },
      },
      captured,
    );

    const c = new LLMClient();
    const resp = await c.chat({
      model: "gpt-4o",
      messages: [{ role: "user", content: "hi" }],
      temperature: 0.5,
      max_tokens: 100,
    });

    expect(captured.call.url).toBe("http://gw/v1/mcp/relay/chat/completions");
    expect(captured.call.init.method).toBe("POST");
    expect(captured.call.init.headers["Authorization"]).toBe("Bearer tk");
    const body = JSON.parse(captured.call.init.body as string);
    expect(body.model).toBe("gpt-4o");
    expect(body.temperature).toBe(0.5);
    expect(body.max_tokens).toBe(100);
    expect(body.stream).toBeUndefined();

    expect(resp.content).toBe("你好!");
    expect(resp.finish_reason).toBe("stop");
    expect(resp.usage.total_tokens).toBe(8);
  });

  it("parses tool_calls in response", async () => {
    process.env.KS_RELAY_TOKEN = "t";
    stubFetchOnce(200, {
      object: "chat.completion",
      choices: [
        {
          index: 0,
          message: {
            role: "assistant",
            content: "",
            tool_calls: [
              {
                id: "call_1",
                type: "function",
                function: { name: "get_weather", arguments: '{"city":"北京"}' },
              },
            ],
          },
          finish_reason: "tool_calls",
        },
      ],
      usage: { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15 },
    });

    const c = new LLMClient();
    const resp = await c.chat({ messages: [{ role: "user", content: "北京天气" }] });

    expect(resp.tool_calls.length).toBe(1);
    expect(resp.tool_calls[0]!.id).toBe("call_1");
    expect(resp.tool_calls[0]!.function.name).toBe("get_weather");
    expect(resp.tool_calls[0]!.function.arguments).toContain("北京");
    expect(resp.finish_reason).toBe("tool_calls");
  });

  it.each([
    [401, LLMUnauthorizedError],
    [403, LLMUnauthorizedError],
    [429, LLMRateLimitedError],
    [500, LLMUpstreamError],
    [502, LLMUpstreamError],
  ])("status %d raises correct error class", async (status, ErrorClass) => {
    process.env.KS_RELAY_TOKEN = "t";
    stubFetchOnce(status as number, "err body");
    const c = new LLMClient();
    await expect(
      c.chat({ messages: [{ role: "user", content: "x" }] }),
    ).rejects.toBeInstanceOf(ErrorClass as any);
  });

  it("invalid JSON response raises LLMUpstreamError", async () => {
    process.env.KS_RELAY_TOKEN = "t";
    stubFetchOnce(200, "not a json");
    const c = new LLMClient();
    await expect(c.chat({ messages: [{ role: "user", content: "x" }] })).rejects.toBeInstanceOf(
      LLMUpstreamError,
    );
  });
});

// ── streamChat：共享 fixture 回归 ─────────────────────────────────────
//
// 使用 import.meta.url + fileURLToPath 解析 __dirname，使 bun test / vitest
// （均为 ESM 环境）下都能稳定工作；原 plan 用 __dirname，但 ESM 下 vitest 不提供。

const __filename_here = fileURLToPath(import.meta.url);
const __dirname_here = dirname(__filename_here);
const SHARED_DIR = resolve(__dirname_here, "..", "..", "shared-fixtures", "sse");

function loadFixture(name: string): { sse: Uint8Array; expected: any[] } {
  const sseStr = readFileSync(resolve(SHARED_DIR, `${name}.sse`));
  // Task 1 fixture 实际命名为 "编号-expected-chunks.json"，这里按编号前缀拼接
  const numberPrefix = name.split("-")[0];
  const expected = JSON.parse(
    readFileSync(resolve(SHARED_DIR, `${numberPrefix}-expected-chunks.json`), "utf-8"),
  );
  return { sse: new Uint8Array(sseStr), expected };
}

function stubStreamFetch(sseBytes: Uint8Array) {
  globalThis.fetch = vi.fn(async () => {
    const stream = new ReadableStream({
      start(controller) {
        controller.enqueue(sseBytes);
        controller.close();
      },
    });
    return new Response(stream, {
      status: 200,
      headers: { "Content-Type": "text/event-stream" },
    });
  }) as any;
}

describe("LLMClient.streamChat (shared fixtures)", () => {
  it.each(["01-text-only", "02-tool-calls", "03-with-usage"])("fixture %s", async (name) => {
    process.env.KS_RELAY_TOKEN = "t";
    const { sse, expected } = loadFixture(name);
    stubStreamFetch(sse);

    const c = new LLMClient();
    const chunks: Chunk[] = [];
    for await (const ch of c.streamChat({ messages: [{ role: "user", content: "x" }] })) {
      chunks.push(ch);
    }

    expect(chunks.length).toBe(expected.length);
    for (let i = 0; i < chunks.length; i++) {
      assertChunkMatches(chunks[i]!, expected[i], i);
    }
  });

  it("non-200 status raises correct error", async () => {
    process.env.KS_RELAY_TOKEN = "t";
    globalThis.fetch = vi.fn(async () => new Response("err", { status: 429 })) as any;
    const c = new LLMClient();
    const consume = async () => {
      for await (const _ of c.streamChat({ messages: [{ role: "user", content: "x" }] })) {
        // unreachable
      }
    };
    // 直接把 Promise 交给 rejects，确保 bun test / vitest 行为一致
    await expect(consume()).rejects.toBeInstanceOf(LLMRateLimitedError);
  });

  it("forces stream=true", async () => {
    process.env.KS_RELAY_TOKEN = "t";
    let capturedBody: any = null;
    globalThis.fetch = vi.fn(async (_url: any, init: any) => {
      capturedBody = JSON.parse(init.body);
      const stream = new ReadableStream({
        start(controller) {
          controller.enqueue(new TextEncoder().encode("data: [DONE]\n\n"));
          controller.close();
        },
      });
      return new Response(stream, {
        status: 200,
        headers: { "Content-Type": "text/event-stream" },
      });
    }) as any;

    const c = new LLMClient();
    for await (const _ of c.streamChat({
      messages: [{ role: "user", content: "x" }],
      stream: false, // 用户传 false，SDK 必须覆盖
    })) {
      // unreachable
    }

    expect(capturedBody.stream).toBe(true);
  });
});

function assertChunkMatches(got: Chunk, want: any, index: number) {
  if ("delta_content" in want)
    expect(got.delta_content, `chunk[${index}].delta_content`).toBe(want.delta_content);
  if ("finish_reason" in want)
    expect(got.finish_reason, `chunk[${index}].finish_reason`).toBe(want.finish_reason);
  if ("tool_calls_delta" in want) {
    // 跨语言 fixture 比对按"结构等价"：got 和 want 同时剥离空字符串字段（id/type/name/arguments）
    // 再逐项深比较。与 Go Task 4 / Python Task 7 保持一致。
    const normalize = (arr: any[]) =>
      arr.map((d) => {
        const obj: any = { index: d.index ?? 0 };
        if (d.id) obj.id = d.id;
        if (d.type) obj.type = d.type;
        const fn: any = {};
        if (d.function?.name) fn.name = d.function.name;
        if (d.function?.arguments) fn.arguments = d.function.arguments;
        obj.function = fn;
        return obj;
      });
    const actual = normalize(got.tool_calls_delta || []);
    const expected = normalize(want.tool_calls_delta || []);
    expect(actual, `chunk[${index}].tool_calls_delta`).toEqual(expected);
  }
  if ("usage" in want) {
    expect(got.usage, `chunk[${index}].usage`).toEqual(want.usage);
  }
}
