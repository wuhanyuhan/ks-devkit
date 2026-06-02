/**
 * 自定义 MCP JSON-RPC HTTP 处理器。
 *
 * 与 Go SDK mcpproto/streamable.go 和 Python SDK mcpproto/streamable.py 行为完全对齐：
 * - 不校验 Accept header（官方 SDK 的 WebStandardStreamableHTTPServerTransport 会拒绝缺少
 *   "text/event-stream" 的请求，但 conformance 套件的 curl 不发该 header）
 * - 无 id 字段（JSON-RPC notification）→ 202 Accepted 无 body
 * - initialize → protocolVersion "2025-03-26"
 * - tools/list → { tools: [...] }
 * - tools/call → 调用注册的 handler
 * - 未知方法 → -32601 method not found
 */

import type { McpWrapper } from "./wrapper";

/** MCP Streamable HTTP 协议版本，对齐 Go/Python SDK */
const MCP_PROTOCOL_VERSION = "2025-03-26";

const ERR_PARSE_ERROR = -32700;
const ERR_INVALID_REQUEST = -32600;
const ERR_METHOD_NOT_FOUND = -32601;
const ERR_INVALID_PARAMS = -32602;
const ERR_INTERNAL = -32603;

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function rpcResult(id: unknown, result: unknown): Response {
  return jsonResponse({ jsonrpc: "2.0", id, result });
}

function rpcError(id: unknown, code: number, message: string): Response {
  return jsonResponse({ jsonrpc: "2.0", id, error: { code, message } });
}

/**
 * 创建自定义 MCP JSON-RPC HTTP handler。
 *
 * 用于替代官方 WebStandardStreamableHTTPServerTransport，避免 Accept header 强制检查。
 */
export function createMcpJsonRpcHandler(
  appId: string,
  appVersion: string,
  mcp: McpWrapper
): (req: Request) => Promise<Response> {
  return async function handleMcpRequest(req: Request): Promise<Response> {
    // 解析请求体
    let body: Record<string, unknown>;
    try {
      body = (await req.json()) as Record<string, unknown>;
    } catch {
      return rpcError(null, ERR_PARSE_ERROR, "JSON 解析失败");
    }

    // JSON-RPC notification 检测（无 id 字段）→ 202 Accepted
    if (typeof body !== "object" || body === null || !("id" in body)) {
      return new Response(null, { status: 202 });
    }

    const id = body["id"] as unknown;

    // JSON-RPC 版本校验
    if (body["jsonrpc"] !== "2.0") {
      return rpcError(id, ERR_INVALID_REQUEST, "仅支持 JSON-RPC 2.0");
    }

    const method = body["method"] as string | undefined;

    switch (method) {
      case "initialize":
        return rpcResult(id, {
          protocolVersion: MCP_PROTOCOL_VERSION,
          capabilities: { tools: {} },
          serverInfo: { name: appId, version: appVersion },
        });

      case "tools/list": {
        const toolInfos = mcp.listToolInfos();
        const tools = toolInfos.map((t) => ({
          name: t.name,
          description: t.description ?? "",
          inputSchema: { type: "object" },
        }));
        return rpcResult(id, { tools });
      }

      case "tools/call": {
        const params = body["params"] as Record<string, unknown> | null | undefined;
        if (!params) {
          return rpcError(id, ERR_INVALID_PARAMS, "tools/call 缺少 params 字段");
        }
        const toolName = params["name"] as string | undefined;
        const rawArgs = (params["arguments"] ?? {}) as Record<string, unknown>;
        // capability mesh：把 MCP params._meta（keystone 透传的 ks_*）并进 args._meta，
        // 供 capability 生成 tool / 复用降级 handler 经 args._meta.ks_* 提取 caller 上下文。
        const meta = params["_meta"];
        const args = meta !== undefined ? { ...rawArgs, _meta: meta } : rawArgs;
        if (!toolName) {
          return rpcError(id, ERR_INVALID_PARAMS, "params.name 必填");
        }
        try {
          const result = await mcp.callTool(toolName, args);
          return rpcResult(id, result);
        } catch (err) {
          const msg = err instanceof Error ? err.message : String(err);
          if (msg.startsWith("tool not found:")) {
            return rpcError(id, ERR_INVALID_PARAMS, msg);
          }
          // 内部错误不暴露细节
          return rpcError(id, ERR_INTERNAL, "工具执行失败");
        }
      }

      default:
        return rpcError(
          id,
          ERR_METHOD_NOT_FOUND,
          `未知方法: ${method ?? "(null)"}`
        );
    }
  };
}
