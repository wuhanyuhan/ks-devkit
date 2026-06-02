import { describe, it, expect, afterEach } from "vitest";
import { writeFileSync, mkdtempSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { parseManifestCapabilities } from "./manifest_capabilities";

let dir: string;
afterEach(() => { if (dir) rmSync(dir, { recursive: true, force: true }); });

function write(body: string): string {
  dir = mkdtempSync(join(tmpdir(), "ksman-"));
  const p = join(dir, "manifest.yaml");
  writeFileSync(p, body);
  return p;
}

describe("parseManifestCapabilities", () => {
  it("provides 裸名 + input_schema；requires 全名", () => {
    const p = write(`
id: ks-mcp-x
provides:
  capabilities:
    - name: web_search
      execution_mode: sync
      backend: {kind: mcp_tool, tool_name: web_search}
      input_schema: {type: object, properties: {q: {type: string}}}
requires:
  capabilities:
    - canonical_name: ks-mcp-other.generate
`);
    const { provides, requires } = parseManifestCapabilities(p);
    expect(provides).toHaveLength(1);
    expect(provides[0]!.name).toBe("web_search");
    expect(provides[0]!.backend.tool_name).toBe("web_search");
    expect(provides[0]!.input_schema).toEqual({ type: "object", properties: { q: { type: "string" } } });
    expect(requires).toEqual([{ canonical_name: "ks-mcp-other.generate" }]);
  });

  it("文件不存在 → 空数组（非错误）", () => {
    const r = parseManifestCapabilities(join(tmpdir(), "nope-" + Math.floor(performance.now()) + ".yaml"));
    expect(r.provides).toEqual([]);
    expect(r.requires).toEqual([]);
  });
});
