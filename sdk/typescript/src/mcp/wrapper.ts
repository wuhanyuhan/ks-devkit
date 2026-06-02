import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { ZodRawShape } from "zod";
import type { ToolInfo } from "../types";

export type ToolHandler<T extends ZodRawShape = ZodRawShape> = (
  args: Record<string, unknown>
) => Promise<unknown>;

export interface ToolMeta<T extends ZodRawShape = ZodRawShape> {
  description: string;
  inputSchema: T;
}

export interface McpWrapperConfig {
  name: string;
  version: string;
  poolSize?: number;
}

export interface ToolEntry {
  info: ToolInfo;
  handler: ToolHandler;
}

export interface McpWrapper {
  readonly poolSize: number;
  readonly primaryServer: McpServer;
  registerTool<T extends ZodRawShape>(
    name: string,
    meta: ToolMeta<T>,
    handler: ToolHandler<T>
  ): void;
  /**
   * registerRawTool：注册 capability 生成的 tool（input_schema 是 manifest 的 JSON Schema，
   * 非 Zod raw shape）。走 handlers map + toolInfos（自定义 JSON-RPC dispatcher 的 live 路径）；
   * pool server.tool() 用空 params 注册（capability 入参由 keystone dispatcher 校验，不在本地 Zod 校）。
   */
  registerRawTool(
    name: string,
    description: string,
    inputSchema: Record<string, unknown>,
    handler: ToolHandler
  ): void;
  listToolInfos(): ToolInfo[];
  listToolEntries(): ToolEntry[];
  getServer(): McpServer;
  callTool(name: string, args: Record<string, unknown>): Promise<unknown>;
}

/**
 * 创建 MCP server pool + tool registry。
 *
 * 原因 pool：官方 SDK 在 stateless mode 下每请求一个 transport，
 * 但 McpServer 实例可复用（实践中验证过 size=5 合理）。
 *
 * 所有 pool 实例共享同一套 tool 注册——注册时对每个实例都调一次 server.tool()。
 * handler 同时保存在 handlers map 中，供自定义 JSON-RPC dispatcher 直接调用。
 */
export function createMcpWrapper(cfg: McpWrapperConfig): McpWrapper {
  const poolSize = cfg.poolSize ?? 5;
  const pool: McpServer[] = [];
  for (let i = 0; i < poolSize; i++) {
    pool.push(new McpServer({ name: cfg.name, version: cfg.version }));
  }

  const toolInfos: ToolInfo[] = [];
  const handlers = new Map<string, ToolHandler>();
  const registered = new Set<string>();
  let rotationIndex = 0;

  return {
    poolSize,
    primaryServer: pool[0]!,

    registerTool(name, meta, handler) {
      if (registered.has(name)) {
        throw new Error(`tool "${name}" already registered`);
      }
      registered.add(name);
      toolInfos.push({ name, description: meta.description });
      handlers.set(name, handler as ToolHandler);
      for (const server of pool) {
        server.tool(name, meta.inputSchema, handler as never);
      }
    },

    registerRawTool(name, description, _inputSchema, handler) {
      if (registered.has(name)) {
        throw new Error(`tool "${name}" already registered`);
      }
      registered.add(name);
      toolInfos.push({ name, description });
      handlers.set(name, handler);
      for (const server of pool) {
        // 空 params：capability tool 的入参由 keystone dispatcher 校验，不在本地 Zod 校。
        server.tool(name, {}, handler as never);
      }
    },

    listToolInfos() {
      return [...toolInfos];
    },

    listToolEntries() {
      return toolInfos.map((info) => ({
        info,
        handler: handlers.get(info.name)!,
      }));
    },

    getServer() {
      const s = pool[rotationIndex % poolSize]!;
      rotationIndex++;
      return s;
    },

    async callTool(name, args) {
      const handler = handlers.get(name);
      if (!handler) {
        throw new Error(`tool not found: ${name}`);
      }
      return handler(args);
    },
  };
}
