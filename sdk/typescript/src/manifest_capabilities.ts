/**
 * manifest_capabilities：解析 manifest 的 provides/requires capability 段（镜像 Go
 * app.go:loadManifestCapabilities）。与既有 manifest.ts（只取 id/name/version/auth）分离，
 * 不污染其职责。provides 写裸名（去前缀，派生在 canonical.ts）；requires 写全名。
 */
import { existsSync, readFileSync } from "node:fs";
import { parse } from "yaml";
import type { CapabilitySpec, BackendSpec, RequiresCapability } from "./types";

interface RawBackend {
  kind?: string;
  tool_name?: string;
  path?: string;
  method?: string;
}
interface RawCapability {
  name?: string;
  execution_mode?: string;
  backend?: RawBackend;
  timeout_ms?: number;
  input_schema?: unknown;
  concurrency_limit?: number;
  resumable?: boolean;
}
interface RawManifest {
  provides?: { capabilities?: RawCapability[] };
  requires?: { capabilities?: { canonical_name?: string }[] };
}

export interface ManifestCapabilities {
  provides: CapabilitySpec[];
  requires: RequiresCapability[];
}

export function parseManifestCapabilities(path: string): ManifestCapabilities {
  if (!existsSync(path)) return { provides: [], requires: [] };
  const raw = parse(readFileSync(path, "utf-8")) as RawManifest | null;
  if (!raw) return { provides: [], requires: [] };

  const provides: CapabilitySpec[] = (raw.provides?.capabilities ?? []).map((c) => {
    const rb = c.backend ?? {};
    const backend: BackendSpec = {
      kind: rb.kind ?? "",
      tool_name: rb.tool_name ?? "",
      path: rb.path ?? "",
      method: rb.method ?? "",
    };
    return {
      name: c.name ?? "",
      execution_mode: c.execution_mode ?? "",
      backend,
      timeout_ms: typeof c.timeout_ms === "number" ? c.timeout_ms : 0,
      input_schema:
        c.input_schema && typeof c.input_schema === "object"
          ? (c.input_schema as Record<string, unknown>)
          : undefined,
      concurrency_limit: typeof c.concurrency_limit === "number" ? c.concurrency_limit : 0,
      resumable: c.resumable === true,
    };
  });

  const requires: RequiresCapability[] = (raw.requires?.capabilities ?? [])
    .map((r) => ({ canonical_name: r.canonical_name ?? "" }))
    .filter((r) => r.canonical_name !== "");

  return { provides, requires };
}
