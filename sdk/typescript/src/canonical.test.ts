import { describe, it, expect } from "vitest";
import { canonical } from "./canonical";

describe("canonical", () => {
  it("派生 appId.name", () => {
    expect(canonical("ks-mcp-browser", "web_search")).toBe("ks-mcp-browser.web_search");
    expect(canonical("translator", "translate")).toBe("translator.translate");
  });
});
