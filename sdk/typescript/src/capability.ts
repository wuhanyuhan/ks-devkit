/**
 * CapabilityRegistry：capability 注册表 + 启动期 finalize + mcp_tool 复用四象限 +
 * http_endpoint wiring（镜像 Go capability.go）。
 *
 * 去前缀：register 收裸名、canonical(appId,name) 派生做 key。
 * 四象限：existingToolNames = wire 前快照。
 */
import { canonical } from "./canonical";
import { ManifestMismatchError } from "./errors";
import { buildContextFromMeta, type CapabilityContext, type ProgressReporter } from "./capability_context";
import type { CapabilitySpec } from "./types";

export type CapabilityHandler = (
  ctx: CapabilityContext,
  args: Record<string, unknown>,
) => Promise<unknown>;

/** mcp wrapper 的 tool handler 签名（与 mcp/wrapper.ts ToolHandler 对齐）。 */
export type RawToolHandler = (args: Record<string, unknown>) => Promise<unknown>;

export interface CapabilityEntry {
  canonicalName: string;
  name: string;
  handler?: CapabilityHandler;
  backendKind: string;
  backendToolName: string;
  backendPath: string;
  backendMethod: string;
  executionMode: string;
  timeoutMs: number;
  inputSchema?: Record<string, unknown>;
}

export interface GeneratedTool {
  toolName: string;
  description: string;
  inputSchema?: Record<string, unknown>;
  handler: RawToolHandler;
}

export interface HttpEndpointRoute {
  path: string;
  method: string;
  canonicalName: string;
  entry: CapabilityEntry;
}

export class CapabilityRegistry {
  readonly entries = new Map<string, CapabilityEntry>();
  private manifestProvides: CapabilitySpec[] = [];

  constructor(
    private readonly appId: string,
    private readonly reportProgress?: ProgressReporter,
  ) {}

  /** register 收裸名 name，派生 canonical 做 key；重复注册抛错。 */
  register(name: string, handler: CapabilityHandler): void {
    const cn = canonical(this.appId, name);
    if (this.entries.has(cn)) {
      throw new Error(`ksapp: capability ${JSON.stringify(cn)} already registered`);
    }
    this.entries.set(cn, {
      canonicalName: cn, name, handler,
      backendKind: "", backendToolName: "", backendPath: "", backendMethod: "",
      executionMode: "", timeoutMs: 0,
    });
  }

  /** finalize：注入 manifest 元信息 + 校验已注册项都在 manifest（去前缀派生匹配）。 */
  finalize(provides: CapabilitySpec[]): void {
    this.manifestProvides = provides;
    const byCanonical = new Map<string, CapabilitySpec>();
    const manifestNames: string[] = [];
    for (const spec of provides) {
      const cn = canonical(this.appId, spec.name);
      byCanonical.set(cn, spec);
      manifestNames.push(cn);
    }
    for (const [cn, entry] of this.entries) {
      const spec = byCanonical.get(cn);
      if (!spec) throw new ManifestMismatchError(cn, manifestNames);
      entry.backendKind = spec.backend.kind;
      entry.backendToolName = spec.backend.tool_name ?? "";
      entry.backendPath = spec.backend.path ?? "";
      entry.backendMethod = spec.backend.method ?? "";
      entry.executionMode = spec.execution_mode ?? "";
      entry.timeoutMs = spec.timeout_ms ?? 0;
      entry.inputSchema = spec.input_schema;
    }
  }

  /**
   * wireMcpTools 四象限。existingToolNames = wire 前已注册 app.tool 名快照。
   * 返回需新生成的 tool（复用项 skip；冲突/orphan 抛错）。必须在 finalize 后调。
   */
  wireMcpTools(existingToolNames: Set<string>): GeneratedTool[] {
    const generated: GeneratedTool[] = [];
    for (const spec of this.manifestProvides) {
      if (spec.backend.kind !== "mcp_tool") continue;
      const cn = canonical(this.appId, spec.name);
      const toolName = spec.backend.tool_name ?? "";
      if (!toolName) {
        throw new ManifestMismatchError(cn, [`${cn} backend.tool_name empty`]);
      }
      const entry = this.entries.get(cn);
      const hasHandler = entry !== undefined && entry.handler !== undefined;
      const toolExists = existingToolNames.has(toolName);

      if (hasHandler && toolExists) {
        throw new Error(
          `capability ${JSON.stringify(cn)} backend.tool_name=${JSON.stringify(toolName)} ` +
            `collides with existing tool registration`,
        );
      } else if (hasHandler && !toolExists) {
        generated.push(this.makeGeneratedTool(entry!, cn, toolName));
      } else if (!hasHandler && toolExists) {
        // 复用已有 app.tool（join）；caller 上下文经 args._meta.ks_* + extractCallerContext。
        continue;
      } else {
        // orphan → error（BREAKING）：声明 mcp_tool 但既无 handler 也无同名 tool。
        throw new ManifestMismatchError(cn, [
          `${cn} backend.tool_name=${toolName} 既无已注册 tool 也无 registerCapability handler`,
        ]);
      }
    }
    return generated;
  }

  private makeGeneratedTool(entry: CapabilityEntry, cn: string, toolName: string): GeneratedTool {
    const reporter = this.reportProgress;
    const handler: RawToolHandler = async (args) => {
      const meta = (args?._meta ?? {}) as Record<string, unknown>;
      const ctx = buildContextFromMeta(meta, {
        canonicalName: cn, timeoutMs: entry.timeoutMs, reportProgress: reporter,
      });
      return entry.handler!(ctx, args);
    };
    return { toolName, description: `capability ${cn}`, inputSchema: entry.inputSchema, handler };
  }

  /** http_endpoint capability → 路由项（path/method/canonicalName/entry），给 app.ts 挂 hono + scoped JWT。 */
  wireHttpEndpoints(): HttpEndpointRoute[] {
    const routes: HttpEndpointRoute[] = [];
    for (const [cn, entry] of this.entries) {
      if (entry.backendKind !== "http_endpoint") continue;
      if (!entry.backendPath) {
        throw new ManifestMismatchError(cn, [`${cn} backend.path empty`]);
      }
      routes.push({
        path: entry.backendPath,
        method: (entry.backendMethod || "POST").toUpperCase(),
        canonicalName: cn,
        entry,
      });
    }
    return routes;
  }
}
