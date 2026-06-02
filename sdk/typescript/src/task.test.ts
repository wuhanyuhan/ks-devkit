import { describe, it, expect, vi } from "vitest";
import { CapabilityCall, Task } from "./task";
import { BackendError, CancelledError, DispatcherRestartedError } from "./errors";
import type { DispatcherClient } from "./dispatcher_client";

function fakeDispatcher(over: Partial<DispatcherClient> = {}): DispatcherClient {
  return { invoke: vi.fn(), getTask: vi.fn(), cancelTask: vi.fn(), reportProgress: vi.fn(), ...over } as unknown as DispatcherClient;
}

describe("CapabilityCall（全名 + 链式 + 透传）", () => {
  it("withOnBehalfOfUser / withChainContext 透传到 invoke；invoke 走 sync", async () => {
    const invoke = vi.fn(async () => ({ sync: { result: { ok: 1 }, durationMs: 1 } }));
    const d = fakeDispatcher({ invoke });
    const out = await new CapabilityCall("ks-mcp-other.generate", d)
      .withOnBehalfOfUser(42).withChainContext("chn_1", "hdr").invoke({ prompt: "hi" });
    expect(out).toEqual({ ok: 1 });
    expect(invoke).toHaveBeenCalledWith(expect.objectContaining({
      capability: "ks-mcp-other.generate", args: { prompt: "hi" }, mode: "sync",
      onBehalfOfUserId: 42, chainId: "chn_1", chainHeader: "hdr",
    }));
  });

  it("submit 走 async 返 Task", async () => {
    const invoke = vi.fn(async () => ({ async: { taskId: "t1", status: "running", submittedAt: "", timeoutAt: "" } }));
    const t = await new CapabilityCall("a.b", fakeDispatcher({ invoke })).submit({ x: 1 });
    expect(t).toBeInstanceOf(Task);
    expect(t.taskId).toBe("t1");
    expect(invoke).toHaveBeenCalledWith(expect.objectContaining({ mode: "async" }));
  });
});

describe("Task.result（轮询到终态）", () => {
  it("done → resultPayload", async () => {
    const getTask = vi.fn(async () => ({
      taskId: "t1", status: "done", canonicalName: "a.b", percent: 100,
      stageMessage: "", result: { y: 2 }, errorCode: "", errorMessage: "",
    }));
    const t = new Task({ taskId: "t1", status: "running", canonicalName: "a.b" }, fakeDispatcher({ getTask }));
    expect(await t.result({ pollIntervalMs: 1 })).toEqual({ y: 2 });
  });

  it("failed errorCode=50000 → DispatcherRestarted；其他 → BackendError；cancelled → Cancelled", async () => {
    const mk = (status: string, errorCode = "") => new Task(
      { taskId: "t", status: "running", canonicalName: "a.b" },
      fakeDispatcher({ getTask: vi.fn(async () => ({
        taskId: "t", status, canonicalName: "a.b", percent: 0, stageMessage: "",
        result: {}, errorCode, errorMessage: "boom",
      })) }),
    );
    await expect(mk("failed", "50000").result({ pollIntervalMs: 1 })).rejects.toBeInstanceOf(DispatcherRestartedError);
    await expect(mk("failed").result({ pollIntervalMs: 1 })).rejects.toBeInstanceOf(BackendError);
    await expect(mk("cancelled").result({ pollIntervalMs: 1 })).rejects.toBeInstanceOf(CancelledError);
  });
});
