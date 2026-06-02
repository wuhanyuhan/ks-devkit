import { describe, it, expect } from "vitest";
import { loadManifest } from "./manifest";
import { resolve } from "path";

const FIXTURES = resolve(__dirname, "../tests/fixtures");

describe("loadManifest", () => {
  it("parses service mount manifest", () => {
    const m = loadManifest(resolve(FIXTURES, "manifest-service.yaml"));
    expect(m).toEqual({
      id: "test-service",
      name: "Test Service",
      version: "1.0.0",
      authMode: "keystone_jwks",
    });
  });

  it("parses extension mount manifest", () => {
    const m = loadManifest(resolve(FIXTURES, "manifest-extension.yaml"));
    expect(m).toEqual({
      id: "test-extension",
      name: "Test Extension",
      version: "2.0.0",
      authMode: "none",
    });
  });

  it("returns null when file missing (no throw)", () => {
    const m = loadManifest(resolve(FIXTURES, "does-not-exist.yaml"));
    expect(m).toBeNull();
  });

  it("throws on invalid auth_mode", () => {
    const path = resolve(FIXTURES, "manifest-invalid.yaml");
    const fs = require("fs");
    fs.writeFileSync(path, "id: x\nversion: 1\nmount:\n  service:\n    auth_mode: wrong\n");
    expect(() => loadManifest(path)).toThrow(/invalid auth_mode/);
    fs.unlinkSync(path);
  });
});
