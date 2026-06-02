import { describe, it, expect } from "vitest";
import { CapabilityRegistry } from "./capability";
import { ManifestMismatchError } from "./errors";
import type { CapabilitySpec } from "./types";

function mcpCap(name: string, toolName: string, inputSchema?: Record<string, unknown>): CapabilitySpec {
  return { name, execution_mode: "sync", backend: { kind: "mcp_tool", tool_name: toolName }, input_schema: inputSchema };
}

describe("register 去前缀派生 + 全名不对称", () => {
  it("register 收裸名、派生 canonical key；重复注册抛错", () => {
    const reg = new CapabilityRegistry("ks-mcp-x");
    reg.register("web_search", async () => ({}));
    expect(reg.entries.has("ks-mcp-x.web_search")).toBe(true);
    expect(reg.entries.get("ks-mcp-x.web_search")!.name).toBe("web_search");
    expect(() => reg.register("web_search", async () => ({}))).toThrow(/already registered/);
  });
});

describe("finalize 派生匹配 manifest", () => {
  it("注册名不在 manifest → ManifestMismatch", () => {
    const reg = new CapabilityRegistry("ks-mcp-x");
    reg.register("web_search", async () => ({}));
    expect(() => reg.finalize([mcpCap("other", "other_tool")])).toThrow(ManifestMismatchError);
  });
  it("匹配则注入 backend 元信息", () => {
    const reg = new CapabilityRegistry("ks-mcp-x");
    reg.register("web_search", async () => ({}));
    reg.finalize([mcpCap("web_search", "web_search_tool", { type: "object" })]);
    const e = reg.entries.get("ks-mcp-x.web_search")!;
    expect(e.backendKind).toBe("mcp_tool");
    expect(e.backendToolName).toBe("web_search_tool");
    expect(e.inputSchema).toEqual({ type: "object" });
  });
});

describe("wireMcpTools 四象限", () => {
  it("①无 handler & tool 命中 → 复用（不生成、不抛）", () => {
    const reg = new CapabilityRegistry("ks-mcp-browser");
    reg.finalize([mcpCap("web_search", "web_search")]);
    const generated = reg.wireMcpTools(new Set(["web_search"]));
    expect(generated).toHaveLength(0);
  });
  it("②有 handler & 未命中 → 生成新 tool（带 input_schema）", () => {
    const reg = new CapabilityRegistry("ks-mcp-x");
    reg.register("gen", async () => ({ ok: 1 }));
    reg.finalize([mcpCap("gen", "gen_tool", { type: "object", properties: { q: { type: "string" } } })]);
    const generated = reg.wireMcpTools(new Set());
    expect(generated).toHaveLength(1);
    expect(generated[0]!.toolName).toBe("gen_tool");
    expect(generated[0]!.inputSchema).toEqual({ type: "object", properties: { q: { type: "string" } } });
  });
  it("③有 handler & tool 命中 → 冲突抛错", () => {
    const reg = new CapabilityRegistry("ks-mcp-x");
    reg.register("dup", async () => ({}));
    reg.finalize([mcpCap("dup", "dup_tool")]);
    expect(() => reg.wireMcpTools(new Set(["dup_tool"]))).toThrow(/collides/);
  });
  it("④无 handler & 未命中 → orphan→error（BREAKING）", () => {
    const reg = new CapabilityRegistry("ks-mcp-x");
    reg.finalize([mcpCap("orphan", "missing_tool")]);
    expect(() => reg.wireMcpTools(new Set())).toThrow(ManifestMismatchError);
  });
});

describe("生成 tool 的 handler 从 args._meta 构造 CapabilityContext", () => {
  it("wrapped handler 传 CapabilityContext，chainHeader 取 ks_chain_snapshot（Bug#2 SDK 侧）", async () => {
    const reg = new CapabilityRegistry("ks-mcp-x");
    let seenChain = "";
    let seenCaller = "";
    reg.register("gen", async (ctx) => { seenChain = ctx.chainHeader; seenCaller = ctx.callerId; return { done: 1 }; });
    reg.finalize([mcpCap("gen", "gen_tool")]);
    const [tool] = reg.wireMcpTools(new Set());
    const out = await tool!.handler({ q: "x", _meta: { ks_caller_id: "app-7", ks_chain_snapshot: "snap" } });
    expect(out).toEqual({ done: 1 });
    expect(seenCaller).toBe("app-7");
    expect(seenChain).toBe("snap");
  });
});

describe("wireHttpEndpoints", () => {
  it("http_endpoint capability → path/method/canonicalName 路由项", () => {
    const reg = new CapabilityRegistry("ks-mcp-x");
    reg.register("hook", async () => ({ ok: 1 }));
    reg.finalize([{ name: "hook", execution_mode: "sync", backend: { kind: "http_endpoint", path: "/cap/hook", method: "POST" } }]);
    const routes = reg.wireHttpEndpoints();
    expect(routes).toHaveLength(1);
    expect(routes[0]!.path).toBe("/cap/hook");
    expect(routes[0]!.method).toBe("POST");
    expect(routes[0]!.canonicalName).toBe("ks-mcp-x.hook");
  });
});
