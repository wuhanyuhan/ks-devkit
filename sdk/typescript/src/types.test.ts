import { describe, it, expect } from "vitest";
import {
  AuthMode,
  isValidAuthMode,
  defaultAuthMode,
  type MetaResponse,
  type ToolInfo,
  type ConfigUIInfo,
  type MetaNavDecl,
  type MetaPermissionDecl,
  type MetaConfigMode,
  type MetaConfigStatus,
} from "./types";
import type { CapabilitySpec, BackendSpec, RequiresCapability } from "./types";

describe("types", () => {
  it("AuthMode exports const enum values", () => {
    expect(AuthMode.None).toBe("none");
    expect(AuthMode.KeystoneJWKS).toBe("keystone_jwks");
    expect(AuthMode.StaticBearer).toBe("static_bearer");
  });

  it("AuthMode type accepts all three values", () => {
    const values: AuthMode[] = ["none", "keystone_jwks", "static_bearer"];
    expect(values).toHaveLength(3);
  });

  it("MetaResponse type allows minimal shape", () => {
    const m: MetaResponse = { name: "x", version: "1.0.0" };
    expect(m.name).toBe("x");
  });

  it("MetaResponse type allows full shape", () => {
    const m: MetaResponse = {
      name: "x",
      version: "1.0.0",
      auth_mode: "keystone_jwks",
      config_ui: { enabled: true, url: "/config-ui/" },
      tools: [{ name: "echo", description: "Echo" }],
    };
    expect(m.tools?.[0]?.name).toBe("echo");
  });

  it("ToolInfo description is optional", () => {
    const t: ToolInfo = { name: "x" };
    expect(t.name).toBe("x");
  });

  it("ConfigUIInfo url is optional", () => {
    const c: ConfigUIInfo = { enabled: false };
    expect(c.enabled).toBe(false);
  });

  it("isValidAuthMode rejects invalid strings", () => {
    expect(isValidAuthMode("none")).toBe(true);
    expect(isValidAuthMode("keystone_jwks")).toBe(true);
    expect(isValidAuthMode("static_bearer")).toBe(true);
    expect(isValidAuthMode("KEYSTONE_JWKS")).toBe(false);
    expect(isValidAuthMode("invalid_mode")).toBe(false);
    expect(isValidAuthMode("")).toBe(false);
  });

  it("defaultAuthMode normalizes empty/null/undefined to None", () => {
    expect(defaultAuthMode("")).toBe(AuthMode.None);
    expect(defaultAuthMode(null)).toBe(AuthMode.None);
    expect(defaultAuthMode(undefined)).toBe(AuthMode.None);
  });

  it("defaultAuthMode passes valid modes through", () => {
    expect(defaultAuthMode("keystone_jwks")).toBe(AuthMode.KeystoneJWKS);
    expect(defaultAuthMode("none")).toBe(AuthMode.None);
    expect(defaultAuthMode("static_bearer")).toBe(AuthMode.StaticBearer);
  });

  it("defaultAuthMode throws on invalid input", () => {
    expect(() => defaultAuthMode("bogus")).toThrow(/invalid auth_mode/);
    expect(() => defaultAuthMode("KEYSTONE_JWKS")).toThrow(/invalid auth_mode/);
  });

  // -------------------------------------------------------------------------
  // v0.2.0 新增 5 字段（对齐 ks-types v0.5.0 MetaResponse）的类型契约
  // -------------------------------------------------------------------------

  it("MetaNavDecl: 完整形态（label/icon/category/order/open_mode/entry_path/required_perms 全有）", () => {
    const nav: MetaNavDecl = {
      label: "文生图",
      icon: "image",
      category: "应用",
      order: 10,
      open_mode: "fullpage",
      entry_path: "/",
      required_perms: ["mcp.image-gen.use"],
    };
    expect(nav.label).toBe("文生图");
    expect(nav.category).toBe("应用");
    expect(nav.open_mode).toBe("fullpage");
    expect(nav.required_perms).toEqual(["mcp.image-gen.use"]);
  });

  it("MetaNavDecl: 最小形态（仅 label / category / open_mode）", () => {
    const nav: MetaNavDecl = {
      label: "工具",
      category: "工具",
      open_mode: "dialog",
    };
    expect(nav.icon).toBeUndefined();
    expect(nav.order).toBeUndefined();
    expect(nav.entry_path).toBeUndefined();
    expect(nav.required_perms).toBeUndefined();
  });

  it("MetaPermissionDecl: 全字段 + 仅必填字段", () => {
    const full: MetaPermissionDecl = {
      code: "mcp.image-gen.use",
      label: "使用文生图",
      default_roles: ["admin"],
    };
    const minimal: MetaPermissionDecl = {
      code: "mcp.image-gen.admin",
      label: "管理文生图",
    };
    expect(full.default_roles).toEqual(["admin"]);
    expect(minimal.default_roles).toBeUndefined();
  });

  it("MetaConfigMode: 三个枚举值都可用", () => {
    const modes: MetaConfigMode[] = ["schema", "iframe", "none"];
    expect(modes).toHaveLength(3);
  });

  it("MetaConfigStatus: 四个枚举值都可用", () => {
    const statuses: MetaConfigStatus[] = [
      "unconfigured",
      "via_frontend",
      "via_cli",
      "mixed",
    ];
    expect(statuses).toHaveLength(4);
  });

  it("MetaResponse: 5 个新字段全部可选 + snake_case wire 命名", () => {
    const m: MetaResponse = {
      name: "image-gen",
      version: "1.0.0",
      nav: { label: "文生图", category: "应用", open_mode: "fullpage" },
      permissions: [{ code: "mcp.image-gen.use", label: "使用文生图" }],
      config_mode: "iframe",
      protocol_version: "1.0",
      config_status: "via_frontend",
    };
    // snake_case 字段名验证（编译期 + 运行期一致）
    expect(m.config_mode).toBe("iframe");
    expect(m.protocol_version).toBe("1.0");
    expect(m.config_status).toBe("via_frontend");
    expect(m.nav?.open_mode).toBe("fullpage");
  });
});

describe("capability mesh wire 镜像类型（snake_case）", () => {
  it("CapabilitySpec / BackendSpec 结构（编译期 + 运行期形态）", () => {
    const backend: BackendSpec = { kind: "mcp_tool", tool_name: "web_search" };
    const spec: CapabilitySpec = {
      name: "web_search",
      execution_mode: "sync",
      backend,
      input_schema: { type: "object" },
    };
    expect(spec.name).toBe("web_search");
    expect(spec.backend.tool_name).toBe("web_search");
    const req: RequiresCapability = { canonical_name: "ks-mcp-other.generate" };
    expect(req.canonical_name).toBe("ks-mcp-other.generate");
  });
});
