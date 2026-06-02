import { describe, it, expect, afterEach, vi } from "vitest";
import { DispatcherClient } from "./dispatcher_client";
import { CapabilityNotFoundError, TaskNotFoundError, RateLimitError } from "./errors";

const realFetch = globalThis.fetch;
afterEach(() => { globalThis.fetch = realFetch; });

function okEnvelope(data: unknown) {
  return new Response(JSON.stringify({ code: 0, message: "", data }), {
    status: 200, headers: { "Content-Type": "application/json" },
  });
}

describe("DispatcherClient.invoke", () => {
  it("sync：body 仅含 capability/mode/args/on_behalf_of_user_id(>0)，chain header 仅非空设置", async () => {
    const captured: any = {};
    globalThis.fetch = vi.fn(async (url: any, init: any) => {
      captured.url = String(url);
      captured.method = init.method;
      captured.body = JSON.parse(init.body);
      captured.chainId = init.headers["X-Keystone-Chain-Id"];
      captured.chainHeader = init.headers["X-Keystone-Call-Chain"];
      return okEnvelope({ result: { ok: true }, duration_ms: 1 });
    }) as any;

    const client = new DispatcherClient("http://gw", "tk");
    const res = await client.invoke({
      capability: "ks-mcp-other.generate", args: { prompt: "hi" }, mode: "sync",
      onBehalfOfUserId: 42, chainId: "chn_1", chainHeader: "eyJjaGFpbiI6IDF9",
    });
    expect(captured.url).toBe("http://gw/v1/apps/self/invoke");
    expect(captured.body).toEqual({
      capability: "ks-mcp-other.generate", mode: "sync", args: { prompt: "hi" }, on_behalf_of_user_id: 42,
    });
    expect(captured.chainId).toBe("chn_1");
    expect(captured.chainHeader).toBe("eyJjaGFpbiI6IDF9");
    expect(res.sync?.result).toEqual({ ok: true });
    expect(res.sync?.durationMs).toBe(1);
  });

  it("on_behalf_of_user_id<=0 不进 body；无 chain 时不设 header", async () => {
    const captured: any = {};
    globalThis.fetch = vi.fn(async (_url: any, init: any) => {
      captured.body = JSON.parse(init.body);
      captured.keys = Object.keys(init.headers);
      return okEnvelope({ result: {}, duration_ms: 0 });
    }) as any;
    await new DispatcherClient("http://gw", "tk").invoke({ capability: "a.b", args: {}, mode: "sync", onBehalfOfUserId: 0 });
    expect(captured.body).toEqual({ capability: "a.b", mode: "sync", args: {} });
    expect(captured.keys).not.toContain("X-Keystone-Chain-Id");
    expect(captured.keys).not.toContain("X-Keystone-Call-Chain");
  });

  it("async：data.task_id 存在 → InvokeResult.async", async () => {
    globalThis.fetch = vi.fn(async () =>
      okEnvelope({ task_id: "t1", status: "running", submitted_at: "s", timeout_at: "e" })) as any;
    const res = await new DispatcherClient("http://gw", "tk").invoke({ capability: "a.b", mode: "async" });
    expect(res.async?.taskId).toBe("t1");
    expect(res.async?.status).toBe("running");
  });

  it("404 → CapabilityNotFound(hint=capability)", async () => {
    globalThis.fetch = vi.fn(async () => new Response("{}", { status: 404 })) as any;
    await expect(new DispatcherClient("http://gw", "tk").invoke({ capability: "x.y", mode: "sync" }))
      .rejects.toBeInstanceOf(CapabilityNotFoundError);
  });

  it("429 → RateLimitError(retryAfterMs)", async () => {
    globalThis.fetch = vi.fn(async () => new Response("{}", { status: 429, headers: { "Retry-After": "3" } })) as any;
    await expect(new DispatcherClient("http://gw", "tk").invoke({ capability: "x.y", mode: "sync" }))
      .rejects.toMatchObject({ retryAfterMs: 3000 });
  });
});

describe("DispatcherClient.getTask / cancelTask（404 重映射 TaskNotFound）", () => {
  it("getTask snake→camel", async () => {
    globalThis.fetch = vi.fn(async () => okEnvelope({
      task_id: "t1", status: "done", canonical_name: "a.b", percent: 100,
      stage_message: "ok", result: { x: 1 }, error_code: "", error_message: "",
    })) as any;
    const snap = await new DispatcherClient("http://gw", "tk").getTask("t1");
    expect(snap.taskId).toBe("t1");
    expect(snap.canonicalName).toBe("a.b");
    expect(snap.stageMessage).toBe("ok");
    expect(snap.result).toEqual({ x: 1 });
  });

  it("getTask 404 → TaskNotFound 而非 CapabilityNotFound", async () => {
    globalThis.fetch = vi.fn(async () => new Response("{}", { status: 404 })) as any;
    await expect(new DispatcherClient("http://gw", "tk").getTask("t1")).rejects.toBeInstanceOf(TaskNotFoundError);
  });

  it("cancelTask 404 → TaskNotFound", async () => {
    globalThis.fetch = vi.fn(async () => new Response("{}", { status: 404 })) as any;
    await expect(new DispatcherClient("http://gw", "tk").cancelTask("t1")).rejects.toBeInstanceOf(TaskNotFoundError);
  });
});

describe("DispatcherClient.reportProgress（best-effort）", () => {
  it("网络错误吞掉、不抛", async () => {
    globalThis.fetch = vi.fn(async () => { throw new Error("network"); }) as any;
    await expect(new DispatcherClient("http://gw", "tk").reportProgress("t1", "step", 10)).resolves.toBeUndefined();
  });
});
