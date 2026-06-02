import { describe, it, expect } from "vitest";
import { z } from "zod";
import { writeFileSync, mkdtempSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { createApp } from "./index";
import type { MetaResponse } from "./types";

describe("createApp", () => {
  it("creates app with minimal config", () => {
    const app = createApp({ id: "test" });
    expect(app).toBeDefined();
    expect(typeof app.tool).toBe("function");
    expect(typeof app.run).toBe("function");
    expect(typeof app.fetch).toBe("function");
  });

  it("exposes mcpServer escape hatch", () => {
    const app = createApp({ id: "test" });
    expect(app.mcpServer).toBeDefined();
  });

  it("tool() is chainable", () => {
    const app = createApp({ id: "test" });
    const result = app.tool(
      "echo",
      { description: "Echo", inputSchema: { message: z.string() } },
      async ({ message }) => ({ echoed: message })
    );
    expect(result).toBe(app);
  });

  it("/healthz works via app.fetch", async () => {
    const app = createApp({ id: "test" });
    const res = await app.fetch(new Request("http://x/healthz"));
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ status: "ok" });
  });

  it("/readyz works via app.fetch", async () => {
    const app = createApp({ id: "test" });
    const res = await app.fetch(new Request("http://x/readyz"));
    expect(res.status).toBe(200);
  });

  it("/meta includes registered tools", async () => {
    const app = createApp({ id: "test", version: "2.0.0" });
    app.tool(
      "echo",
      { description: "Echo message", inputSchema: { message: z.string() } },
      async ({ message }) => ({ echoed: message })
    );
    const res = await app.fetch(new Request("http://x/meta"));
    const body = (await res.json()) as MetaResponse;
    expect(body.name).toBe("test");
    expect(body.version).toBe("2.0.0");
    expect(body.tools).toEqual([{ name: "echo", description: "Echo message" }]);
  });

  it("/meta includes auth_mode when keystone_jwks set", async () => {
    process.env.KEYSTONE_JWKS_URL = "http://jwks";
    const app = createApp({ id: "test", auth: "keystone_jwks" });
    const res = await app.fetch(new Request("http://x/meta"));
    const body = (await res.json()) as MetaResponse;
    expect(body.auth_mode).toBe("keystone_jwks");
    delete process.env.KEYSTONE_JWKS_URL;
  });

  it("strict-by-default: keystone_jwks without URL throws on creation", () => {
    const prev = process.env.KEYSTONE_JWKS_URL;
    delete process.env.KEYSTONE_JWKS_URL;
    expect(() => createApp({ id: "test", auth: "keystone_jwks" })).toThrow(/KEYSTONE_JWKS_URL/);
    if (prev) process.env.KEYSTONE_JWKS_URL = prev;
  });

  it("insecure override allows missing URL", () => {
    process.env.KS_APP_AUTH_MODE = "insecure";
    const app = createApp({ id: "test", auth: "keystone_jwks" });
    expect(app).toBeDefined();
    delete process.env.KS_APP_AUTH_MODE;
  });

  it("healthCheck contributes to /healthz", async () => {
    const app = createApp({ id: "test" });
    app.healthCheck("custom", async () => { throw new Error("fail"); });
    const res = await app.fetch(new Request("http://x/healthz"));
    expect(res.status).toBe(503);
  });

  it("use() adds middleware and handle() adds custom route", async () => {
    const app = createApp({ id: "test" });
    app.handle("GET", "/custom", (_req) => new Response("custom", { status: 200 }));
    const res = await app.fetch(new Request("http://x/custom"));
    expect(res.status).toBe(200);
    expect(await res.text()).toBe("custom");
  });

  // -------------------------------------------------------------------------
  // v0.2.0 新增 5 个 declare 方法（对齐 Python SDK v0.4.0 + ks-types v0.5.0）
  // 端到端走 /meta 路由，断 wire-format snake_case 字段
  // -------------------------------------------------------------------------

  it("declareNav: 完整字段进入 /meta.nav，未声明时缺省", async () => {
    const app = createApp({ id: "svc" });
    app.declareNav({
      label: "文生图",
      category: "应用",
      open_mode: "fullpage",
      icon: "image",
      order: 10,
      entry_path: "/",
      required_perms: ["mcp.image-gen.use"],
    });
    const res = await app.fetch(new Request("http://x/meta"));
    const body = (await res.json()) as MetaResponse;
    expect(body.nav).toBeDefined();
    expect(body.nav?.label).toBe("文生图");
    expect(body.nav?.category).toBe("应用");
    expect(body.nav?.open_mode).toBe("fullpage");
    expect(body.nav?.icon).toBe("image");
    expect(body.nav?.order).toBe(10);
    expect(body.nav?.entry_path).toBe("/");
    expect(body.nav?.required_perms).toEqual(["mcp.image-gen.use"]);

    // 反向：未调时 omitempty 缺省
    const app2 = createApp({ id: "svc2" });
    const res2 = await app2.fetch(new Request("http://x/meta"));
    const body2 = (await res2.json()) as MetaResponse;
    expect(body2.nav).toBeUndefined();
  });

  it("declarePermission: 多次累加进入 /meta.permissions，default_roles omitempty", async () => {
    const app = createApp({ id: "svc" });
    app.declarePermission({ code: "mcp.image-gen.use", label: "使用文生图", default_roles: ["admin"] });
    app.declarePermission({ code: "mcp.image-gen.admin", label: "管理文生图" });
    const res = await app.fetch(new Request("http://x/meta"));
    const body = (await res.json()) as MetaResponse;
    expect(body.permissions).toBeDefined();
    expect(body.permissions).toHaveLength(2);
    expect(body.permissions?.[0]).toEqual({
      code: "mcp.image-gen.use",
      label: "使用文生图",
      default_roles: ["admin"],
    });
    expect(body.permissions?.[1]).toEqual({
      code: "mcp.image-gen.admin",
      label: "管理文生图",
    });
    expect(body.permissions?.[1]?.default_roles).toBeUndefined();

    // 反向：未调（空数组）按 omitempty 缺省
    const app2 = createApp({ id: "svc2" });
    const res2 = await app2.fetch(new Request("http://x/meta"));
    const body2 = (await res2.json()) as MetaResponse;
    expect(body2.permissions).toBeUndefined();
  });

  it("setConfigMode: schema/iframe/none 三个值都进入 /meta.config_mode，未调缺省", async () => {
    for (const mode of ["schema", "iframe", "none"] as const) {
      const app = createApp({ id: "svc" });
      app.setConfigMode(mode);
      const res = await app.fetch(new Request("http://x/meta"));
      const body = (await res.json()) as MetaResponse;
      expect(body.config_mode).toBe(mode);
    }

    // 反向：未调缺省
    const app2 = createApp({ id: "svc2" });
    const res2 = await app2.fetch(new Request("http://x/meta"));
    const body2 = (await res2.json()) as MetaResponse;
    expect(body2.config_mode).toBeUndefined();
  });

  it("setConfigMode: 非法枚举值抛 Error", () => {
    const app = createApp({ id: "svc" });
    // @ts-expect-error 故意传非法值验证 runtime 校验
    expect(() => app.setConfigMode("xxx")).toThrow(/config_mode/);
  });

  it("setProtocolVersion + setConfigStatus: 进入 /meta，未调缺省", async () => {
    const app = createApp({ id: "svc" });
    app.setProtocolVersion("1.0");
    app.setConfigStatus("via_frontend");
    const res = await app.fetch(new Request("http://x/meta"));
    const body = (await res.json()) as MetaResponse;
    expect(body.protocol_version).toBe("1.0");
    expect(body.config_status).toBe("via_frontend");

    // 反向：未调缺省
    const app2 = createApp({ id: "svc2" });
    const res2 = await app2.fetch(new Request("http://x/meta"));
    const body2 = (await res2.json()) as MetaResponse;
    expect(body2.protocol_version).toBeUndefined();
    expect(body2.config_status).toBeUndefined();
  });

  it("setConfigStatus: 四个有效值不抛，非法值抛 Error", () => {
    const app = createApp({ id: "svc" });
    for (const valid of ["unconfigured", "via_frontend", "via_cli", "mixed"] as const) {
      expect(() => app.setConfigStatus(valid)).not.toThrow();
    }
    // @ts-expect-error 故意传非法值验证 runtime 校验
    expect(() => app.setConfigStatus("invalid_status")).toThrow(/config_status/);
  });
});

describe("App capability mesh 集成", () => {
  function appWithManifest(body: string) {
    const dir = mkdtempSync(join(tmpdir(), "ksapp-"));
    const p = join(dir, "manifest.yaml");
    writeFileSync(p, body);
    return { app: createApp({ id: "ks-mcp-x", manifestPath: p }), dir };
  }

  it("registerCapability + finalize：有 handler 生成 tool", () => {
    const { app, dir } = appWithManifest(`
id: ks-mcp-x
provides:
  capabilities:
    - name: gen
      execution_mode: sync
      backend: {kind: mcp_tool, tool_name: gen_tool}
`);
    app.registerCapability("gen", async () => ({ ok: 1 }));
    app.finalizeCapabilities();
    expect(app.mcpServer).toBeDefined();
    expect((app as any).mcp.listToolInfos().map((t: any) => t.name)).toContain("gen_tool");
    rmSync(dir, { recursive: true, force: true });
  });

  it("orphan → finalizeCapabilities 抛错（BREAKING）", () => {
    const { app, dir } = appWithManifest(`
id: ks-mcp-x
provides:
  capabilities:
    - name: orphan
      execution_mode: sync
      backend: {kind: mcp_tool, tool_name: missing_tool}
`);
    expect(() => app.finalizeCapabilities()).toThrow();
    rmSync(dir, { recursive: true, force: true });
  });

  it("callCapability 全名不派生（不对称）", () => {
    const { app, dir } = appWithManifest(`id: ks-mcp-x`);
    const call = app.callCapability("ks-mcp-other.generate");
    expect((call as any).canonicalName).toBe("ks-mcp-other.generate");
    rmSync(dir, { recursive: true, force: true });
  });
});

describe("App http_endpoint capability（scoped JWT 保护）", () => {
  it("带合法 scoped JWT → handler 执行，claims 进 CapabilityContext", async () => {
    const { SignJWT, exportSPKI, generateKeyPair } = await import("jose");
    const kp = await generateKeyPair("RS256");
    const spki = await exportSPKI(kp.publicKey);
    const dir = mkdtempSync(join(tmpdir(), "ksapp-http-"));
    const p = join(dir, "manifest.yaml");
    writeFileSync(p, `
id: ks-mcp-x
provides:
  capabilities:
    - name: hook
      execution_mode: sync
      backend: {kind: http_endpoint, path: /cap/hook, method: POST}
`);
    process.env.KS_SCOPED_JWKS_URL = ""; // 用 static key 注入
    const app = createApp({ id: "ks-mcp-x", manifestPath: p });
    let seenCaller = "";
    app.registerCapability("hook", async (ctx) => { seenCaller = ctx.callerId; return { ok: 1 }; });
    await app.setScopedStaticKey("kid1", spki);
    app.finalizeCapabilities();

    const now = Math.floor(Date.now() / 1000);
    const token = await new SignJWT({ sub: "7", kx_caller_id: "app-7" })
      .setProtectedHeader({ alg: "RS256", kid: "kid1" }).setIssuedAt(now)
      .setExpirationTime(now + 300).setAudience("ks-mcp-x.hook").sign(kp.privateKey);

    const resp = await app.fetch(new Request("http://x/cap/hook", {
      method: "POST", headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
      body: JSON.stringify({ q: "hi" }),
    }));
    expect(resp.status).toBe(200);
    expect(seenCaller).toBe("app-7");
    rmSync(dir, { recursive: true, force: true });
    delete process.env.KS_SCOPED_JWKS_URL;
  });
});
