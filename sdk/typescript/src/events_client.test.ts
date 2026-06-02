import { describe, it, expect, afterEach, vi } from "vitest";
import { EventsClient, EventStream } from "./events_client";

const realFetch = globalThis.fetch;
afterEach(() => { globalThis.fetch = realFetch; });

describe("EventStream（AsyncIterable + buffered）", () => {
  it("push 后 for await 拿到事件；close 结束迭代", async () => {
    const s = new EventStream("t1");
    s.push({ task_id: "t1", type: "progress" });
    s.push({ task_id: "t1", type: "done" });
    s.close();
    const got: any[] = [];
    for await (const ev of s) got.push(ev.type);
    expect(got).toEqual(["progress", "done"]);
  });
});

describe("EventsClient.dispatch 路由", () => {
  it("按 task_id 路由到注册 stream；未注册静默丢弃", async () => {
    const c = new EventsClient("http://gw", "tk", "polling");
    const s = c.register("t1");
    (c as any).dispatch({ task_id: "t1", type: "x" });
    (c as any).dispatch({ task_id: "tZ", type: "y" }); // 未注册 → drop
    s.close();
    const got: any[] = [];
    for await (const ev of s) got.push(ev.type);
    expect(got).toEqual(["x"]);
  });
});

describe("EventsClient.pollOnce", () => {
  it("GET ?since=cursor，dispatch events + 更新 cursor", async () => {
    const c = new EventsClient("http://gw", "tk", "polling");
    const s = c.register("t1");
    globalThis.fetch = vi.fn(async (url: any) => {
      expect(String(url)).toContain("/v1/apps/self/events?since=0");
      return new Response(JSON.stringify({
        code: 0, data: { events: [{ task_id: "t1", type: "progress" }], next_cursor: "c2" },
      }), { status: 200 });
    }) as any;
    await (c as any).pollOnce();
    expect((c as any).pollingCursor).toBe("c2");
    s.close();
    const got: any[] = [];
    for await (const ev of s) got.push(ev.type);
    expect(got).toEqual(["progress"]);
  });
});
