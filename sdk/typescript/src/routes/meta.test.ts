import { describe, it, expect } from "vitest";
import { Hono } from "hono";
import { registerMetaRoute } from "./meta";
import { AuthMode, type ToolInfo, type MetaResponse } from "../types";

describe("registerMetaRoute", () => {
  it("returns minimal MetaResponse when no tools and auth=none", async () => {
    const app = new Hono();
    registerMetaRoute(app, {
      name: "svc",
      version: "1.0.0",
      effectiveAuthMode: AuthMode.None,
      getToolInfos: () => [],
    });
    const res = await app.request("/meta");
    expect(res.status).toBe(200);
    const body = (await res.json()) as MetaResponse;
    expect(body.name).toBe("svc");
    expect(body.version).toBe("1.0.0");
    expect(body.auth_mode).toBeUndefined(); // none 时省略
    expect(body.tools).toBeUndefined();
    expect(body.config_ui).toBeUndefined();
  });

  it("emits auth_mode when keystone_jwks", async () => {
    const app = new Hono();
    registerMetaRoute(app, {
      name: "svc",
      version: "1.0.0",
      effectiveAuthMode: AuthMode.KeystoneJWKS,
      getToolInfos: () => [],
    });
    const res = await app.request("/meta");
    const body = (await res.json()) as MetaResponse;
    expect(body.auth_mode).toBe("keystone_jwks");
  });

  it("emits tools when registered", async () => {
    const tools: ToolInfo[] = [{ name: "echo", description: "Echo" }];
    const app = new Hono();
    registerMetaRoute(app, {
      name: "svc",
      version: "1.0.0",
      effectiveAuthMode: AuthMode.None,
      getToolInfos: () => tools,
    });
    const res = await app.request("/meta");
    const body = (await res.json()) as MetaResponse;
    expect(body.tools).toEqual(tools);
  });

  it("config_ui null is omitted (not emitted as {})", async () => {
    const app = new Hono();
    registerMetaRoute(app, {
      name: "svc",
      version: "1.0.0",
      effectiveAuthMode: AuthMode.None,
      getToolInfos: () => [],
      configUI: null,
    });
    const res = await app.request("/meta");
    const text = await res.text();
    expect(text).not.toContain("config_ui");
  });
});
