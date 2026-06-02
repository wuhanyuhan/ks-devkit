import { describe, it, expect } from "vitest";
import * as ks from "../src/index";

describe("MCP SDK subpath imports", () => {
  it("imports McpServer from server/mcp.js", async () => {
    const mod = await import("@modelcontextprotocol/sdk/server/mcp.js");
    expect(mod.McpServer).toBeDefined();
    expect(typeof mod.McpServer).toBe("function");
  });

  it("imports streamable HTTP transport", async () => {
    const mod = await import(
      "@modelcontextprotocol/sdk/server/webStandardStreamableHttp.js"
    );
    expect(mod.WebStandardStreamableHTTPServerTransport).toBeDefined();
  });

  it("can construct McpServer instance", async () => {
    const { McpServer } = await import(
      "@modelcontextprotocol/sdk/server/mcp.js"
    );
    const server = new McpServer({ name: "test", version: "0.0.1" });
    expect(server).toBeDefined();
  });
});

describe("capability mesh 公共导出", () => {
  it("导出核心类/函数", () => {
    expect(typeof ks.canonical).toBe("function");
    expect(typeof ks.DispatcherClient).toBe("function");
    expect(typeof ks.CapabilityCall).toBe("function");
    expect(typeof ks.Task).toBe("function");
    expect(typeof ks.EventsClient).toBe("function");
    expect(typeof ks.ScopedJWTVerifier).toBe("function");
    expect(typeof ks.KeystoneError).toBe("function");
    expect(typeof ks.CapabilityNotFoundError).toBe("function");
    expect(typeof ks.buildContextFromMeta).toBe("function");
    expect(typeof ks.extractCallerContext).toBe("function");
  });
});
