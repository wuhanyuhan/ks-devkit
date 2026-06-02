import { describe, it, expect } from "vitest";
import { createLifecycle } from "./lifecycle";

describe("lifecycle", () => {
  it("runs startup fns in order", async () => {
    const lc = createLifecycle();
    const calls: number[] = [];
    lc.onStartup(async () => { calls.push(1); });
    lc.onStartup(async () => { calls.push(2); });
    await lc.runStartup();
    expect(calls).toEqual([1, 2]);
  });

  it("runs shutdown fns in reverse order", async () => {
    const lc = createLifecycle();
    const calls: number[] = [];
    lc.onShutdown(async () => { calls.push(1); });
    lc.onShutdown(async () => { calls.push(2); });
    await lc.runShutdown();
    expect(calls).toEqual([2, 1]);
  });

  it("shutdown continues even if one fn throws", async () => {
    const lc = createLifecycle();
    const calls: number[] = [];
    lc.onShutdown(async () => { calls.push(1); });
    lc.onShutdown(async () => { throw new Error("fail"); });
    lc.onShutdown(async () => { calls.push(3); });
    await lc.runShutdown();
    expect(calls).toEqual([3, 1]);
  });
});
