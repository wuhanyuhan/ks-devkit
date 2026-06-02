import { describe, it, expect, afterEach, vi } from "vitest";
import { SelfClient, KeystoneSelfFetchError } from "../../src/keystone-env/self-client";

const realFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = realFetch;
});

function stubFetch(handler: (url: string, init: RequestInit) => Promise<Response>) {
  globalThis.fetch = vi.fn(async (input: any, init: any) => handler(String(input), init)) as any;
}

describe("SelfClient.fetchEnv", () => {
  it("200 happy path 返回 env map", async () => {
    stubFetch(async (url) => {
      expect(url).toBe("http://gw:9988/v1/apps/self/resources");
      return new Response(
        JSON.stringify({ code: 0, data: { env: { DB_HOST: "10.0.0.1", DB_PASSWORD: "s3cret" } } }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    });

    const client = new SelfClient({ gateway: "http://gw:9988", token: "tok" });
    const env = await client.fetchEnv();
    expect(env).toEqual({ DB_HOST: "10.0.0.1", DB_PASSWORD: "s3cret" });
  });

  it("发请求时带 Bearer token", async () => {
    let capturedAuth: string | undefined;
    stubFetch(async (_url, init) => {
      capturedAuth = new Headers(init.headers).get("authorization") ?? undefined;
      return new Response(JSON.stringify({ code: 0, data: { env: {} } }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });

    await new SelfClient({ gateway: "http://gw:9988", token: "mytoken" }).fetchEnv();
    expect(capturedAuth).toBe("Bearer mytoken");
  });

  it("401 抛 KeystoneSelfFetchError(status=401)", async () => {
    stubFetch(async () => new Response("forbidden", { status: 401 }));
    const client = new SelfClient({ gateway: "http://gw:9988", token: "tok" });
    await expect(client.fetchEnv()).rejects.toMatchObject({
      name: "KeystoneSelfFetchError",
      status: 401,
    });
  });

  it("5xx 抛 KeystoneSelfFetchError", async () => {
    stubFetch(async () => new Response("oops", { status: 502 }));
    const client = new SelfClient({ gateway: "http://gw:9988", token: "tok" });
    await expect(client.fetchEnv()).rejects.toBeInstanceOf(KeystoneSelfFetchError);
  });

  it("网络错误抛 KeystoneSelfFetchError 并保留 cause", async () => {
    const original = new Error("ECONNREFUSED");
    stubFetch(async () => { throw original; });
    const client = new SelfClient({ gateway: "http://gw:9988", token: "tok" });
    await expect(client.fetchEnv()).rejects.toMatchObject({
      name: "KeystoneSelfFetchError",
      cause: original,
    });
  });

  it("响应 shape 异常（缺 data.env）抛 KeystoneSelfFetchError", async () => {
    stubFetch(async () =>
      new Response(JSON.stringify({ code: 0, data: {} }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    const client = new SelfClient({ gateway: "http://gw:9988", token: "tok" });
    await expect(client.fetchEnv()).rejects.toBeInstanceOf(KeystoneSelfFetchError);
  });

  it("timeoutMs 触发 timeout", async () => {
    stubFetch(async () => new Promise((_resolve) => { /* never resolves */ }));
    const client = new SelfClient({ gateway: "http://gw:9988", token: "tok", timeoutMs: 50 });
    await expect(client.fetchEnv()).rejects.toBeInstanceOf(KeystoneSelfFetchError);
  });

  it("注入的 fetch 优先于 globalThis.fetch", async () => {
    let injectedCalled = false;
    const injectedFetch = vi.fn(async () => {
      injectedCalled = true;
      return new Response(JSON.stringify({ code: 0, data: { env: { X: "y" } } }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as any;

    globalThis.fetch = vi.fn(async () => {
      throw new Error("globalThis.fetch should not be called");
    }) as any;

    const client = new SelfClient({ gateway: "http://gw:9988", token: "tok", fetch: injectedFetch });
    const env = await client.fetchEnv();
    expect(injectedCalled).toBe(true);
    expect(env).toEqual({ X: "y" });
  });
});
