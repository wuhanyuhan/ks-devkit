import { describe, it, expect } from "vitest";
import { z } from "zod";
import { createMcpWrapper } from "./wrapper";

describe("McpWrapper", () => {
  it("registers tool and reflects it in toolInfos", () => {
    const w = createMcpWrapper({ name: "svc", version: "1.0.0", poolSize: 3 });
    w.registerTool(
      "echo",
      {
        description: "Echo message",
        inputSchema: { message: z.string() },
      },
      async ({ message }) => ({ echoed: message })
    );
    const infos = w.listToolInfos();
    expect(infos).toHaveLength(1);
    expect(infos[0]).toEqual({ name: "echo", description: "Echo message" });
  });

  it("duplicate tool registration throws", () => {
    const w = createMcpWrapper({ name: "svc", version: "1.0.0" });
    w.registerTool(
      "x",
      { description: "a", inputSchema: {} },
      async () => ({})
    );
    expect(() =>
      w.registerTool(
        "x",
        { description: "b", inputSchema: {} },
        async () => ({})
      )
    ).toThrow(/already registered/i);
  });

  it("pool size defaults to 5", () => {
    const w = createMcpWrapper({ name: "svc", version: "1.0.0" });
    expect(w.poolSize).toBe(5);
  });

  it("primaryServer returns an McpServer instance", () => {
    const w = createMcpWrapper({ name: "svc", version: "1.0.0" });
    expect(w.primaryServer).toBeDefined();
  });

  it("getServer rotates through pool", () => {
    const w = createMcpWrapper({ name: "svc", version: "1.0.0", poolSize: 2 });
    const s1 = w.getServer();
    const s2 = w.getServer();
    const s3 = w.getServer();
    expect(s1).not.toBe(s2); // different instances
    expect(s3).toBe(s1); // wraps around
  });

  it("multiple tools registered to all pool instances", async () => {
    const w = createMcpWrapper({ name: "svc", version: "1.0.0", poolSize: 3 });
    w.registerTool("a", { description: "a", inputSchema: {} }, async () => ({}));
    w.registerTool("b", { description: "b", inputSchema: {} }, async () => ({}));
    // Not directly inspectable, but listToolInfos tracks them
    expect(w.listToolInfos()).toHaveLength(2);
  });
});

describe("registerRawTool（capability 生成 tool 用 JSON-schema，非 Zod）", () => {
  it("加入 listToolInfos + callTool 可调；重复抛错", async () => {
    const mcp = createMcpWrapper({ name: "x", version: "1" });
    mcp.registerRawTool("gen_tool", "capability ks-mcp-x.gen", { type: "object" }, async (args) => ({ got: args.q }));
    expect(mcp.listToolInfos().map((t) => t.name)).toContain("gen_tool");
    expect(await mcp.callTool("gen_tool", { q: "hi" })).toEqual({ got: "hi" });
    expect(() => mcp.registerRawTool("gen_tool", "dup", {}, async () => ({}))).toThrow(/already registered/);
  });
});
