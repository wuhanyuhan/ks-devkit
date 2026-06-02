import { afterEach, describe, expect, it, vi } from "vitest";
import { EmbeddingClient } from "./embedding";
import { VectorStoreClient } from "./vector_store";

const realFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = realFetch;
  delete process.env.KS_GATEWAY_URL;
  delete process.env.KS_RELAY_TOKEN;
  delete process.env.KS_EMBEDDING_MODEL;
});

describe("VectorStoreClient", () => {
  it("searchText posts collection and text", async () => {
    process.env.KS_GATEWAY_URL = "http://gw";
    process.env.KS_RELAY_TOKEN = "tk";
    process.env.KS_EMBEDDING_MODEL = "bge-m3";
    const captured: any = {};
    globalThis.fetch = vi.fn(async (url: any, init: any) => {
      captured.url = String(url);
      captured.body = JSON.parse(init.body);
      return new Response(JSON.stringify({
        results: [{ id: "doc1", score: 0.92, payload: { doc_id: "doc1" } }],
      }), { status: 200, headers: { "Content-Type": "application/json" } });
    }) as any;

    const store = new VectorStoreClient(new EmbeddingClient(), "documents");
    const got = await store.searchText("hello", { topK: 5 });
    expect(captured.url).toBe("http://gw/v1/mcp/relay/vector_store/search_text");
    expect(captured.body.collection).toBe("documents");
    expect(captured.body.text).toBe("hello");
    expect(got[0]?.id).toBe("doc1");
  });
});
