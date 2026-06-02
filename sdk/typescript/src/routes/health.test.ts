import { describe, it, expect } from "vitest";
import { Hono } from "hono";
import { registerHealthRoutes } from "./health";

describe("registerHealthRoutes", () => {
  it("/healthz returns {status:ok} with no custom checks", async () => {
    const app = new Hono();
    registerHealthRoutes(app, []);
    const res = await app.request("/healthz");
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ status: "ok" });
  });

  it("/readyz returns {status:ok}", async () => {
    const app = new Hono();
    registerHealthRoutes(app, []);
    const res = await app.request("/readyz");
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ status: "ok" });
  });

  it("/healthz 503 when any check fails", async () => {
    const app = new Hono();
    registerHealthRoutes(app, [
      { name: "db", check: async () => {} },
      { name: "redis", check: async () => { throw new Error("down"); } },
    ]);
    const res = await app.request("/healthz");
    expect(res.status).toBe(503);
    const body = (await res.json()) as { status: string; checks: Record<string, string> };
    expect(body.status).toBe("unhealthy");
    expect(body.checks.redis).toContain("down");
  });

  it("/healthz 200 when all custom checks pass", async () => {
    const app = new Hono();
    registerHealthRoutes(app, [
      { name: "db", check: async () => {} },
    ]);
    const res = await app.request("/healthz");
    expect(res.status).toBe(200);
  });
});
