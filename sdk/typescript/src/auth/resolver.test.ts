import { describe, it, expect } from "vitest";
import { resolveAuth } from "./resolver";

describe("resolveAuth", () => {
  it("code option wins over manifest and env", () => {
    const r = resolveAuth({
      codeMode: "keystone_jwks",
      manifestMode: "none",
      env: { KS_APP_AUTH_MODE: "none", KEYSTONE_JWKS_URL: "http://jwks" },
    });
    expect(r.effectiveMode).toBe("keystone_jwks");
  });

  it("manifest wins over env when code not set", () => {
    const r = resolveAuth({
      manifestMode: "keystone_jwks",
      env: { KEYSTONE_JWKS_URL: "http://jwks" },
    });
    expect(r.effectiveMode).toBe("keystone_jwks");
  });

  it("env KS_APP_AUTH_MODE applies when neither code nor manifest set", () => {
    const r = resolveAuth({
      env: { KS_APP_AUTH_MODE: "keystone_jwks", KEYSTONE_JWKS_URL: "http://jwks" },
    });
    expect(r.effectiveMode).toBe("keystone_jwks");
  });

  it("default is none", () => {
    const r = resolveAuth({ env: {} });
    expect(r.effectiveMode).toBe("none");
  });

  it("strict-by-default: keystone_jwks + no URL + no insecure → throws", () => {
    expect(() =>
      resolveAuth({
        codeMode: "keystone_jwks",
        env: {},
      })
    ).toThrow(/KEYSTONE_JWKS_URL/);
  });

  it("insecure override allows missing URL", () => {
    const r = resolveAuth({
      codeMode: "keystone_jwks",
      env: { KS_APP_AUTH_MODE: "insecure" },
    });
    expect(r.effectiveMode).toBe("none");
    expect(r.jwksUrl).toBe("");
  });

  it("returns jwksUrl from env when keystone_jwks", () => {
    const r = resolveAuth({
      codeMode: "keystone_jwks",
      env: { KEYSTONE_JWKS_URL: "https://x/.well-known/jwks.json" },
    });
    expect(r.jwksUrl).toBe("https://x/.well-known/jwks.json");
  });

  it("jwksUrl empty when mode is none", () => {
    const r = resolveAuth({ codeMode: "none", env: {} });
    expect(r.jwksUrl).toBe("");
  });
});
