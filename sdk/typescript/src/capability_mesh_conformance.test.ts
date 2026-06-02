import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DispatcherClient } from "./dispatcher_client";
import { canonical } from "./canonical";

// 用 fileURLToPath(import.meta.url) 取目录（bun + node/vitest 都支持；不用 bun 专属 import.meta.dir）。
const HERE = dirname(fileURLToPath(import.meta.url));
const SHARED = resolve(HERE, "..", "..", "shared-fixtures");
const realFetch = globalThis.fetch;
afterEach(() => { globalThis.fetch = realFetch; });

function loadFixture(name: string): any {
  return JSON.parse(readFileSync(resolve(SHARED, name), "utf-8"));
}

describe("三语言 wire-compat：DispatcherClient.invoke", () => {
  it("dispatcher_invoke_with_chain.json：body + chain header 与 Go/Python 一致（capability 全名锁）", async () => {
    const fx = loadFixture("dispatcher_invoke_with_chain.json");
    const captured: any = {};
    globalThis.fetch = vi.fn(async (url: any, init: any) => {
      captured.method = init.method;
      captured.path = new URL(String(url)).pathname;
      captured.body = JSON.parse(init.body);
      captured.headers = {};
      for (const k of Object.keys(fx.request.headers)) captured.headers[k] = init.headers[k];
      return new Response(JSON.stringify(fx.response), { status: 200, headers: { "Content-Type": "application/json" } });
    }) as any;

    const b = fx.request.body;
    const h = fx.request.headers;
    await new DispatcherClient("http://gw", "tk").invoke({
      capability: b.capability, args: b.args, mode: b.mode,
      onBehalfOfUserId: b.on_behalf_of_user_id,
      chainId: h["X-Keystone-Chain-Id"], chainHeader: h["X-Keystone-Call-Chain"],
    });

    expect(captured.method).toBe(fx.request.method);
    expect(captured.path).toBe(fx.request.path);
    expect(captured.body).toEqual(fx.request.body);
    expect(captured.headers).toEqual(fx.request.headers);
  });

  it("dispatcher_invoke_on_behalf_of.json：body（on_behalf_of_user_id>0 守卫）一致，无 chain header", async () => {
    const fx = loadFixture("dispatcher_invoke_on_behalf_of.json");
    const captured: any = {};
    globalThis.fetch = vi.fn(async (url: any, init: any) => {
      captured.method = init.method;
      captured.path = new URL(String(url)).pathname;
      captured.body = JSON.parse(init.body);
      return new Response(JSON.stringify(fx.response), { status: 200, headers: { "Content-Type": "application/json" } });
    }) as any;

    const b = fx.request.body;
    await new DispatcherClient("http://gw", "tk").invoke({
      capability: b.capability, args: b.args, mode: b.mode, onBehalfOfUserId: b.on_behalf_of_user_id,
    });

    expect(captured.method).toBe(fx.request.method);
    expect(captured.path).toBe(fx.request.path);
    expect(captured.body).toEqual(fx.request.body);
  });
});

describe("三语言 wire-compat：canonical 派生", () => {
  it("canonical_derivation.json：三 case 与 Go/Python 一致", () => {
    const fx = loadFixture("canonical_derivation.json");
    for (const c of fx.cases) {
      expect(canonical(c.app_id, c.name)).toBe(c.canonical);
    }
  });
});
