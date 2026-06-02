import { describe, it, expect } from "vitest";
import { Hono } from "hono";
import { createAuthMiddleware } from "./middleware";
import type { JWKSVerifier } from "./jwks-verifier";

const stubVerifier = (shouldFail: boolean, payload: Record<string, unknown> = { sub: "agent:1" }): JWKSVerifier => ({
  verify: async (h) => {
    if (shouldFail) throw new Error("invalid token");
    if (!h) throw new Error("missing");
    return payload;
  },
});

describe("createAuthMiddleware", () => {
  it("returns 401 JSON when verifier throws", async () => {
    const app = new Hono();
    app.use("/mcp", createAuthMiddleware(stubVerifier(true)));
    app.post("/mcp", (c) => c.text("ok"));

    const res = await app.request("/mcp", {
      method: "POST",
      headers: { Authorization: "Bearer xxx" },
    });
    expect(res.status).toBe(401);
    expect(res.headers.get("content-type")).toContain("application/json");
    const body = (await res.json()) as { error: string };
    expect(body.error).toBeTruthy();
    expect(typeof body.error).toBe("string");
  });

  it("passes through when verifier succeeds", async () => {
    const app = new Hono();
    app.use("/mcp", createAuthMiddleware(stubVerifier(false)));
    app.post("/mcp", (c) => c.text("ok"));

    const res = await app.request("/mcp", {
      method: "POST",
      headers: { Authorization: "Bearer valid" },
    });
    expect(res.status).toBe(200);
  });

  it("401 body is non-empty string for error field", async () => {
    const app = new Hono();
    app.use("/mcp", createAuthMiddleware(stubVerifier(true)));
    app.post("/mcp", (c) => c.text("ok"));

    const res = await app.request("/mcp", { method: "POST" });
    const body = (await res.json()) as { error: string };
    expect(body.error.length).toBeGreaterThan(0);
  });

  it("passes mcp_config_ui token when server id matches", async () => {
    process.env.KSAPP_SERVER_ID = "42";
    const app = new Hono();
    app.use("/mcp", createAuthMiddleware(stubVerifier(false, {
      sub: "user:1",
      type: "mcp_config_ui",
      mcp_server_id: 42,
    })));
    app.post("/mcp", (c) => c.text("ok"));

    const res = await app.request("/mcp", {
      method: "POST",
      headers: { Authorization: "Bearer valid" },
    });
    expect(res.status).toBe(200);
  });

  it("rejects mcp_config_ui token when server id mismatches", async () => {
    process.env.KSAPP_SERVER_ID = "42";
    const app = new Hono();
    app.use("/mcp", createAuthMiddleware(stubVerifier(false, {
      sub: "user:1",
      type: "mcp_config_ui",
      mcp_server_id: 999,
    })));
    app.post("/mcp", (c) => c.text("ok"));

    const res = await app.request("/mcp", {
      method: "POST",
      headers: { Authorization: "Bearer valid" },
    });
    expect(res.status).toBe(403);
    const body = (await res.json()) as { error: string };
    expect(body.error).toBe("mcp_server_id 不匹配");
  });
});
