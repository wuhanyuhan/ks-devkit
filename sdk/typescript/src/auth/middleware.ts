import type { MiddlewareHandler } from "hono";
import type { JWKSVerifier } from "./jwks-verifier";

type JWTPayload = Awaited<ReturnType<JWKSVerifier["verify"]>>;

/**
 * 挂到 /mcp 路由前的 auth middleware。
 *
 * 对齐 conformance §5：401 响应 Content-Type: application/json，body = {"error": "<非空 string>"}
 */
export function createAuthMiddleware(verifier: JWKSVerifier): MiddlewareHandler {
  return async (c, next) => {
    const authHeader = c.req.header("Authorization");
    try {
      const payload = await verifier.verify(authHeader);
      const configUIError = validateConfigUIServerID(payload);
      if (configUIError) {
        return c.json({ error: configUIError.message }, configUIError.status as 403 | 500);
      }
      c.set("jwtPayload", payload);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      return c.json({ error: msg }, 401);
    }
    await next();
  };
}

function validateConfigUIServerID(payload: JWTPayload): { status: 403 | 500; message: string } | null {
  if (payload.type !== "mcp_config_ui") {
    return null;
  }

  const expectedServerID = process.env.KSAPP_SERVER_ID ?? "";
  if (!expectedServerID) {
    return { status: 500, message: "KSAPP_SERVER_ID 环境变量未配置" };
  }

  const rawClaim = payload.mcp_server_id;
  if (typeof rawClaim !== "string" && typeof rawClaim !== "number") {
    return { status: 403, message: "mcp_server_id 类型不支持" };
  }

  const claimServerID = typeof rawClaim === "number" ? Math.trunc(rawClaim).toString() : rawClaim;
  if (claimServerID !== expectedServerID) {
    return { status: 403, message: "mcp_server_id 不匹配" };
  }
  return null;
}
