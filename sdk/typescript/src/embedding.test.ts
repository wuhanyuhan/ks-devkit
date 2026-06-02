import { afterEach, describe, expect, it, vi } from "vitest";
import { EmbeddingClient, EmbeddingNotConfiguredError } from "./embedding";

const realFetch = globalThis.fetch;

function resetEnv() {
  delete process.env.KS_GATEWAY_URL;
  delete process.env.KS_RELAY_TOKEN;
  delete process.env.KS_EMBEDDING_MODEL;
  delete process.env.KS_EMBEDDING_DIM;
}

afterEach(() => {
  globalThis.fetch = realFetch;
  resetEnv();
});

describe("EmbeddingClient", () => {
  it("POSTs to /v1/mcp/relay/embeddings", async () => {
    process.env.KS_GATEWAY_URL = "http://gw";
    process.env.KS_RELAY_TOKEN = "tk";
    process.env.KS_EMBEDDING_MODEL = "bge-m3";
    const captured: any = {};
    globalThis.fetch = vi.fn(async (url: any, init: any) => {
      captured.url = String(url);
      captured.init = init;
      return new Response(JSON.stringify({
        object: "list",
        model: "bge-m3",
        data: [{ object: "embedding", index: 0, embedding: [0.1, 0.2] }],
        usage: { prompt_tokens: 2, total_tokens: 2 },
      }), { status: 200, headers: { "Content-Type": "application/json" } });
    }) as any;

    const got = await new EmbeddingClient().embed("hello");
    expect(captured.url).toBe("http://gw/v1/mcp/relay/embeddings");
    expect(captured.init.headers.Authorization).toBe("Bearer tk");
    expect(got.dense).toEqual([0.1, 0.2]);
    expect(got.tokens).toBe(2);
  });

  it("throws when token is missing", async () => {
    process.env.KS_EMBEDDING_MODEL = "bge-m3";
    await expect(new EmbeddingClient().embed("x")).rejects.toBeInstanceOf(EmbeddingNotConfiguredError);
  });
});
