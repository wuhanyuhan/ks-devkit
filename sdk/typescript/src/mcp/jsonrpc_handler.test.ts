import { describe, it, expect } from "vitest";
import { createMcpWrapper } from "./wrapper";
import { createMcpJsonRpcHandler } from "./jsonrpc_handler";

describe("jsonrpc tools/call 透传 params._meta 进 args._meta", () => {
  it("handler 能读到 args._meta", async () => {
    const mcp = createMcpWrapper({ name: "x", version: "1" });
    let seen: any;
    mcp.registerRawTool("gen_tool", "d", {}, async (args) => { seen = args._meta; return { ok: 1 }; });
    const handle = createMcpJsonRpcHandler("x", "1", mcp);
    const req = new Request("http://x/mcp", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        jsonrpc: "2.0", id: 1, method: "tools/call",
        params: { name: "gen_tool", arguments: { q: "hi" }, _meta: { ks_caller_id: "app-7" } },
      }),
    });
    const resp = await handle(req);
    expect(resp.status).toBe(200);
    expect(seen).toEqual({ ks_caller_id: "app-7" });
  });
});
