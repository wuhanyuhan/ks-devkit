import { describe, it, expect, vi } from "vitest";
import { buildContextFromMeta, createCapabilityContext, extractCallerContext } from "./capability_context";

describe("buildContextFromMeta（mcp_tool 路径 _meta.ks_* → CapabilityContext）", () => {
  it("camelCase getter 映射 ks_* + Bug#2 chainHeader 取 ks_chain_snapshot", () => {
    const ctx = buildContextFromMeta(
      {
        ks_user_id: "7", ks_caller_id: "app-7", ks_caller_kind: "app",
        ks_chain_id: "chn_1", ks_chain_snapshot: "eyJjaGFpbiI6IDF9",
        ks_task_id: "t1", ks_request_id: "req-1",
      },
      { canonicalName: "ks-mcp-x.foo", timeoutMs: 0 },
    );
    expect(ctx.userId).toBe("7");
    expect(ctx.callerId).toBe("app-7");
    expect(ctx.callerKind).toBe("app");
    expect(ctx.chainId).toBe("chn_1");
    expect(ctx.chainHeader).toBe("eyJjaGFpbiI6IDF9");
    expect(ctx.taskId).toBe("t1");
    expect(ctx.requestId).toBe("req-1");
    expect(ctx.canonicalName).toBe("ks-mcp-x.foo");
    expect(ctx.deadline()).toBe(0); // timeoutMs<=0
    expect(ctx.cancelled()).toBe(false);
  });

  it("deadline = startedAtMs + timeoutMs", () => {
    const ctx = createCapabilityContext({ canonicalName: "x.y", timeoutMs: 5000, startedAtMs: 1000 });
    expect(ctx.deadline()).toBe(6000);
  });

  it("progress 仅 taskId 非空且有 reporter 时上报；失败吞掉", async () => {
    const reporter = vi.fn(async () => {});
    const ctx = createCapabilityContext({ canonicalName: "x.y", taskId: "t1", reportProgress: reporter });
    await ctx.progress("step", 50);
    expect(reporter).toHaveBeenCalledWith("t1", "step", 50);

    const noTask = createCapabilityContext({ canonicalName: "x.y", reportProgress: reporter });
    reporter.mockClear();
    await noTask.progress("step");
    expect(reporter).not.toHaveBeenCalled();
  });
});

describe("extractCallerContext（mcp_tool 复用降级 — 从 args._meta.ks_* 提取）", () => {
  it("读 args._meta", () => {
    const cc = extractCallerContext({ q: "hi", _meta: { ks_caller_id: "app-9", ks_chain_id: "chn_2" } });
    expect(cc.callerId).toBe("app-9");
    expect(cc.chainId).toBe("chn_2");
    expect(cc.callerKind).toBe("");
  });
  it("无 _meta 时全空", () => {
    expect(extractCallerContext({ q: "hi" }).callerId).toBe("");
  });
});
