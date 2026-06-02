import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import { EmbeddingClient } from "./embedding";
import { VectorStoreClient } from "./vector_store";

const realFetch = globalThis.fetch;
// fileURLToPath(import.meta.url) 取目录：bun + node/vitest 双 runtime 通用
// （不用 bun 专属 import.meta.dir——在 node/vitest 下为 undefined 会抛错）。
const HERE = dirname(fileURLToPath(import.meta.url));
const SHARED_FIXTURES = resolve(HERE, "..", "..", "shared-fixtures");

function loadFixture(name: string): any {
  return JSON.parse(readFileSync(resolve(SHARED_FIXTURES, name), "utf-8"));
}

function setEnv() {
  process.env.KS_GATEWAY_URL = "http://gw";
  process.env.KS_RELAY_TOKEN = "tk";
  process.env.KS_EMBEDDING_MODEL = "bge-m3";
}

afterEach(() => {
  globalThis.fetch = realFetch;
  delete process.env.KS_GATEWAY_URL;
  delete process.env.KS_RELAY_TOKEN;
  delete process.env.KS_EMBEDDING_MODEL;
});

describe("Embedding / VectorStore conformance fixtures", () => {
  it("EmbeddingClient matches embeddings_v1.json", async () => {
    const fixture = loadFixture("embeddings_v1.json");
    const captured: any = {};
    setEnv();
    globalThis.fetch = vi.fn(async (_url: any, init: any) => {
      captured.method = init.method;
      captured.path = "/v1/mcp/relay/embeddings";
      captured.body = JSON.parse(init.body);
      return new Response(JSON.stringify(fixture.response), { status: 200, headers: { "Content-Type": "application/json" } });
    }) as any;

    const got = await new EmbeddingClient().embed("hello");

    expect(captured).toEqual(fixture.request);
    expect(got.dense).toEqual([0.1, 0.2]);
    expect(got.tokens).toBe(2);
    expect(got.sparse).toEqual({ "100": 0.5, "7": 0.25 });
  });

  it("VectorStoreClient matches vector_store_search_text.json", async () => {
    const fixture = loadFixture("vector_store_search_text.json");
    const captured: any = {};
    setEnv();
    globalThis.fetch = vi.fn(async (_url: any, init: any) => {
      captured.method = init.method;
      captured.path = "/v1/mcp/relay/vector_store/search_text";
      captured.body = JSON.parse(init.body);
      return new Response(JSON.stringify(fixture.response), { status: 200, headers: { "Content-Type": "application/json" } });
    }) as any;

    const got = await new VectorStoreClient(new EmbeddingClient(), "documents").searchText("hello", { topK: 5 });

    expect(captured).toEqual(fixture.request);
    expect(got[0]?.id).toBe("doc1");
    expect(got[0]?.payload).toEqual({ doc_id: "doc1" });
  });
});
