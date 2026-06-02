import { describe, it, expect } from "vitest";
import { createJWKSVerifier } from "./jwks-verifier";

describe("JWKSVerifier", () => {
  it("creates verifier with jwks URL", () => {
    const v = createJWKSVerifier("http://localhost:9999/.well-known/jwks.json");
    expect(v).toBeDefined();
    expect(typeof v.verify).toBe("function");
  });

  it("throws when URL is empty", () => {
    expect(() => createJWKSVerifier("")).toThrow(/jwks URL/);
  });

  it("verify rejects missing Bearer prefix", async () => {
    const v = createJWKSVerifier("http://localhost:9999/jwks");
    await expect(v.verify("NotBearer xxx")).rejects.toThrow(/Bearer/);
  });

  it("verify rejects empty header", async () => {
    const v = createJWKSVerifier("http://localhost:9999/jwks");
    await expect(v.verify(null)).rejects.toThrow(/missing|authorization/i);
    await expect(v.verify("")).rejects.toThrow(/missing|authorization/i);
  });
});
