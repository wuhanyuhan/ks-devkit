import { Hono, type MiddlewareHandler } from "hono";
import type { ZodRawShape } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import { resolveConfig, type AppConfig } from "./config";
import { createLogger, type Logger } from "./logger";
import { loadManifest } from "./manifest";
import { resolveAuth } from "./auth/resolver";
import { createJWKSVerifier } from "./auth/jwks-verifier";
import { createAuthMiddleware } from "./auth/middleware";
import { registerHealthRoutes, type HealthCheck } from "./routes/health";
import { registerMetaRoute } from "./routes/meta";
import { createMcpWrapper, type McpWrapper, type ToolMeta, type ToolHandler } from "./mcp/wrapper";
import { createMcpJsonRpcHandler } from "./mcp/jsonrpc_handler";
import { createLifecycle, type LifecycleHook } from "./lifecycle";
import { EmbeddingClient } from "./embedding";
import { createLLMClient, type LLMClient } from "./llm";
import { VectorStoreClient } from "./vector_store";
import { CapabilityRegistry, type CapabilityHandler, type HttpEndpointRoute } from "./capability";
import { parseManifestCapabilities } from "./manifest_capabilities";
import { DispatcherClient } from "./dispatcher_client";
import { EventsClient } from "./events_client";
import { CapabilityCall } from "./task";
import { ScopedJWTVerifier, scopedJwtMiddleware, type ScopedClaims } from "./scoped_jwt";
import { createCapabilityContext } from "./capability_context";
import {
  AuthMode,
  type MetaNavDecl,
  type MetaPermissionDecl,
  type MetaConfigMode,
  type MetaConfigStatus,
} from "./types";

const CONFIG_MODE_VALUES: readonly MetaConfigMode[] = ["schema", "iframe", "none"] as const;
const CONFIG_STATUS_VALUES: readonly MetaConfigStatus[] = [
  "unconfigured",
  "via_frontend",
  "via_cli",
  "mixed",
] as const;

export class App {
  private hono: Hono;
  private mcp: McpWrapper;
  private healthChecks: HealthCheck[] = [];
  private lifecycle = createLifecycle();
  private logger: Logger;
  private llmClient: LLMClient;
  private embeddingClient: EmbeddingClient;
  private config: ReturnType<typeof resolveConfig>;
  private effectiveAuthMode: AuthMode;
  private mcpMounted = false;
  // v0.2.0 新增：声明式 meta 字段（对齐 ks-types v0.5.0 MetaResponse + Python SDK v0.4.0 declare API）
  private _nav?: MetaNavDecl;
  private _permissions: MetaPermissionDecl[] = [];
  private _configMode?: MetaConfigMode;
  private _protocolVersion?: string;
  private _configStatus?: MetaConfigStatus;
  // capability mesh：注册表 + 懒构造 client + finalize 幂等 + scoped JWT 验签器
  private capabilityRegistry: CapabilityRegistry;
  private dispatcherClient?: DispatcherClient;
  private eventsClient?: EventsClient;
  private capabilitiesFinalized = false;
  private _scopedVerifier?: ScopedJWTVerifier;

  constructor(userConfig: AppConfig) {
    this.config = resolveConfig(userConfig, process.env.PORT);
    this.logger = userConfig.logger ?? createLogger();
    this.llmClient = createLLMClient();
    this.embeddingClient = new EmbeddingClient();

    const manifest = loadManifest(this.config.manifestPath);
    const authResult = resolveAuth({
      codeMode: userConfig.auth,
      manifestMode: manifest?.authMode,
      env: process.env as Record<string, string | undefined>,
    });
    this.effectiveAuthMode = authResult.effectiveMode;

    this.hono = new Hono();
    this.mcp = createMcpWrapper({
      name: this.config.id,
      version: this.config.version,
      poolSize: this.config.mcpPoolSize,
    });

    registerHealthRoutes(this.hono, this.healthChecks);
    registerMetaRoute(this.hono, {
      name: this.config.id,
      version: this.config.version,
      effectiveAuthMode: this.effectiveAuthMode,
      getToolInfos: () => this.mcp.listToolInfos(),
      getNav: () => this._nav,
      getPermissions: () => this._permissions,
      getConfigMode: () => this._configMode,
      getProtocolVersion: () => this._protocolVersion,
      getConfigStatus: () => this._configStatus,
    });

    // /mcp 路由 — 使用自定义 JSON-RPC handler，不依赖官方 transport 的 Accept header 检查
    const mcpHandler = createMcpJsonRpcHandler(
      this.config.id,
      this.config.version,
      this.mcp
    );

    if (this.effectiveAuthMode === AuthMode.KeystoneJWKS) {
      const verifier = createJWKSVerifier(authResult.jwksUrl);
      const authMW = createAuthMiddleware(verifier);
      this.hono.use("/mcp", authMW);
    }
    this.hono.post("/mcp", (c) => mcpHandler(c.req.raw));

    // capability mesh 注册表：reportProgress 经懒构造的 DispatcherClient 上报。
    this.capabilityRegistry = new CapabilityRegistry(
      this.config.id,
      (taskId, stage, percent) => this.getDispatcherClient().reportProgress(taskId, stage, percent),
    );
  }

  tool<T extends ZodRawShape>(
    name: string,
    meta: ToolMeta<T>,
    handler: ToolHandler<T>
  ): App {
    this.mcp.registerTool(name, meta, handler);
    return this;
  }

  /** 注册 capability handler（裸名；canonical 派生 <app_id>.<name>）。 */
  registerCapability(name: string, handler: CapabilityHandler): App {
    this.capabilityRegistry.register(name, handler);
    return this;
  }

  /** 调他人 capability：写全名、不派生（不对称）。 */
  callCapability(canonicalName: string): CapabilityCall {
    return new CapabilityCall(canonicalName, this.getDispatcherClient(), this.getEventsClient());
  }

  /** 注入 scoped JWT 静态验签公钥（kid→SPKI PEM）。须在 finalizeCapabilities 之前调。 */
  async setScopedStaticKey(kid: string, spkiPem: string): Promise<App> {
    this._scopedVerifier ??= new ScopedJWTVerifier(process.env.KS_SCOPED_JWKS_URL || "");
    await this._scopedVerifier.setStaticKey(kid, spkiPem);
    return this;
  }

  /**
   * finalizeCapabilities：启动期注入 manifest 元信息 + 四象限 wiring + http_endpoint 挂载（幂等）。
   * run() 内自动调；也可显式调（如测试）。
   */
  finalizeCapabilities(): void {
    if (this.capabilitiesFinalized) return;
    const { provides } = parseManifestCapabilities(this.config.manifestPath);
    this.capabilityRegistry.finalize(provides);

    const existing = new Set(this.mcp.listToolInfos().map((t) => t.name));
    for (const gen of this.capabilityRegistry.wireMcpTools(existing)) {
      this.mcp.registerRawTool(gen.toolName, gen.description, gen.inputSchema ?? {}, gen.handler);
    }

    const httpRoutes = this.capabilityRegistry.wireHttpEndpoints();
    if (httpRoutes.length > 0) {
      const pathToName: Record<string, string> = {};
      for (const r of httpRoutes) pathToName[r.path] = r.canonicalName;
      this._scopedVerifier ??= new ScopedJWTVerifier(process.env.KS_SCOPED_JWKS_URL || "");
      this.hono.use(scopedJwtMiddleware(this._scopedVerifier, pathToName));
      for (const r of httpRoutes) {
        this.handle(r.method, r.path, async (req) => this.handleHttpCapability(r, req));
      }
    }
    this.capabilitiesFinalized = true;
  }

  /** http_endpoint capability 请求处理：从 scoped JWT claims 构造 CapabilityContext 调 handler。 */
  private async handleHttpCapability(route: HttpEndpointRoute, req: Request): Promise<Response> {
    // scopedJwtMiddleware 验签后把 claims 挂到 raw Request（即此处的 req）。
    const claims = (req as unknown as { scopedClaims?: ScopedClaims }).scopedClaims;
    if (!claims) {
      return new Response(JSON.stringify({ error: "scoped claims missing" }), { status: 500 });
    }
    let args: Record<string, unknown> = {};
    try {
      args = (await req.json()) as Record<string, unknown>;
    } catch {
      args = {};
    }
    const ctx = createCapabilityContext({
      userId: claims.userId,
      callerId: claims.callerId,
      callerKind: claims.callerKind || "app",
      chainId: claims.chainId,
      chainHeader: req.headers.get("X-Keystone-Call-Chain") ?? "",
      requestId: claims.requestId,
      canonicalName: route.canonicalName,
      timeoutMs: route.entry.timeoutMs,
      reportProgress: (taskId, stage, percent) => this.getDispatcherClient().reportProgress(taskId, stage, percent),
    });
    const result = await route.entry.handler!(ctx, args);
    return new Response(JSON.stringify(result), { status: 200, headers: { "Content-Type": "application/json" } });
  }

  private getDispatcherClient(): DispatcherClient {
    if (!this.dispatcherClient) this.dispatcherClient = new DispatcherClient();
    return this.dispatcherClient;
  }

  private getEventsClient(): EventsClient {
    if (!this.eventsClient) this.eventsClient = new EventsClient();
    return this.eventsClient;
  }

  handle(
    method: string,
    path: string,
    handler: (req: Request) => Response | Promise<Response>
  ): App {
    const m = method.toLowerCase();
    if (m === "get") this.hono.get(path, (c) => handler(c.req.raw));
    else if (m === "post") this.hono.post(path, (c) => handler(c.req.raw));
    else if (m === "put") this.hono.put(path, (c) => handler(c.req.raw));
    else if (m === "delete") this.hono.delete(path, (c) => handler(c.req.raw));
    else if (m === "patch") this.hono.patch(path, (c) => handler(c.req.raw));
    else this.hono.all(path, (c) => handler(c.req.raw));
    return this;
  }

  use(middleware: MiddlewareHandler): App {
    this.hono.use(middleware);
    return this;
  }

  healthCheck(name: string, fn: () => Promise<void>): App {
    this.healthChecks.push({ name, check: fn });
    return this;
  }

  onStartup(fn: LifecycleHook): App {
    this.lifecycle.onStartup(fn);
    return this;
  }

  onShutdown(fn: LifecycleHook): App {
    this.lifecycle.onShutdown(fn);
    return this;
  }

  /**
   * 声明 MCP 在 keystone 后台左侧菜单的导航项（v0.2.0 新增，对齐 ks-types v0.5.0 MetaNavDecl）。
   *
   * @param nav.label - 菜单显示名（<= 12 字符，中文）
   * @param nav.category - 类目（'应用' / '工具' / '配置' / '集成'）
   * @param nav.open_mode - 打开方式（'dialog' / 'fullpage'）
   * @param nav.icon - lucide-react 图标名（可选）
   * @param nav.order - 排序权重（默认 99）
   * @param nav.entry_path - 入口路径（默认 '/'）
   * @param nav.required_perms - 进入页面所需权限码（AND 语义；空数组 = admin 直通）
   */
  declareNav(nav: MetaNavDecl): App {
    this._nav = { ...nav };
    return this;
  }

  /**
   * 声明 MCP 的权限码目录条目（v0.2.0 新增，对齐 ks-types v0.5.0 MetaPermissionDecl）。
   *
   * 多次调用按顺序累加。
   *
   * @param perm.code - 权限码（`mcp.{mcp_id}.{action}` 格式）
   * @param perm.label - 中文显示名
   * @param perm.default_roles - 默认角色列表（MVP 只 ['admin']）
   */
  declarePermission(perm: MetaPermissionDecl): App {
    this._permissions.push({ ...perm });
    return this;
  }

  /**
   * 设置配置模式（v0.2.0 新增，对齐 ks-types v0.5.0）。
   *
   * 与 mount_config_ui / config_ui 的关系（v0.x.0 共存约定）：
   *   - mode === 'iframe' → 必须配套 config_ui 提供接入信息
   *   - mode === 'schema' → 配置由 keystone SchemaForm 渲染，不需要 config_ui
   *   - mode === 'none'   → 无配置，不需要 config_ui
   *   - 不调本方法 → 走老语义（看 config_ui 是否设置）
   *
   * @throws Error 当 mode 不在 'schema' / 'iframe' / 'none' 之内
   */
  setConfigMode(mode: MetaConfigMode): App {
    if (!CONFIG_MODE_VALUES.includes(mode)) {
      throw new Error(`config_mode 必须是 schema/iframe/none，收到: ${String(mode)}`);
    }
    this._configMode = mode;
    return this;
  }

  /** 设置 MCP 协议版本（SemVer 'MAJOR.MINOR'，MVP '1.0'，v0.2.0 新增）。 */
  setProtocolVersion(version: string): App {
    this._protocolVersion = version;
    return this;
  }

  /**
   * 设置 MCP 配置状态（v0.2.0 新增，对齐 ks-types v0.5.0，由 Spec A §6.4 引入）。
   *
   * 枚举：'unconfigured' / 'via_frontend' / 'via_cli' / 'mixed'
   *
   * @throws Error 当 status 不在枚举内
   */
  setConfigStatus(status: MetaConfigStatus): App {
    if (!CONFIG_STATUS_VALUES.includes(status)) {
      throw new Error(`config_status 枚举越界: ${String(status)}`);
    }
    this._configStatus = status;
    return this;
  }

  llm(): LLMClient {
    return this.llmClient;
  }

  get embedding(): EmbeddingClient {
    return this.embeddingClient;
  }

  vectorStore(collection: string): VectorStoreClient {
    return new VectorStoreClient(this.embeddingClient, collection);
  }

  get mcpServer(): McpServer {
    return this.mcp.primaryServer;
  }

  get fetch(): (req: Request) => Promise<Response> {
    return this.hono.fetch.bind(this.hono) as (req: Request) => Promise<Response>;
  }

  async run(): Promise<void> {
    this.finalizeCapabilities();
    await this.lifecycle.runStartup();
    const port = this.config.port;
    const host = this.config.host;

    const isBun = typeof (globalThis as { Bun?: unknown }).Bun !== "undefined";

    if (isBun) {
      const bunGlobal = (globalThis as { Bun: { serve: (opts: unknown) => { stop: () => void } } }).Bun;
      const server = bunGlobal.serve({
        port,
        hostname: host,
        fetch: this.hono.fetch,
      });
      this.logger.info("server listening (bun)", { host, port });
      await this.installSignalHandlers(async () => {
        server.stop();
        await this.lifecycle.runShutdown();
      });
    } else {
      const { serve } = await import("@hono/node-server");
      const server = serve({ fetch: this.hono.fetch, port, hostname: host });
      this.logger.info("server listening (node)", { host, port });
      await this.installSignalHandlers(async () => {
        server.close();
        await this.lifecycle.runShutdown();
      });
    }
  }

  private installSignalHandlers(onSignal: () => Promise<void>): Promise<void> {
    return new Promise<void>((resolve) => {
      const shutdown = async (sig: string) => {
        this.logger.info("received signal, shutting down", { sig });
        await onSignal();
        resolve();
      };
      process.on("SIGINT", () => void shutdown("SIGINT"));
      process.on("SIGTERM", () => void shutdown("SIGTERM"));
    });
  }
}
