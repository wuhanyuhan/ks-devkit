/**
 * ScopedJWTVerifier + scopedJwtMiddleware：http_endpoint backend 的 scoped JWT 验签
 * （镜像 scoped_jwt.go + scoped_jwt_middleware.go）。用 jose（已是 dep）。
 *
 * claims 用 kx_ 前缀（注意 ≠ mcp_tool _meta 路径的 ks_ 前缀）。验签 RS256 + 校验 aud。
 *
 * jose v6 适配：无 KeyLike 导出，密钥类型用 CryptoKey（importSPKI 返回类型）。
 */
import { jwtVerify, importSPKI, createRemoteJWKSet, type JWTPayload, type CryptoKey, type JWTVerifyGetKey } from "jose";
import type { MiddlewareHandler } from "hono";
import { TokenAudienceMismatchError, TokenExpiredError, TokenInvalidError } from "./errors";

export interface ScopedClaims {
  userId: string; // sub
  canonicalName: string; // aud
  callerId: string; // kx_caller_id
  callerKind: string; // kx_caller_kind
  chainId: string; // kx_chain_id
  requestId: string; // kx_request_id
  issuedAt: number; // iat
  expiresAt: number; // exp
}

function s(v: unknown): string {
  return typeof v === "string" ? v : v === undefined || v === null ? "" : String(v);
}
function n(v: unknown): number {
  return typeof v === "number" ? v : 0;
}

export class ScopedJWTVerifier {
  private readonly staticKeys = new Map<string, CryptoKey>();
  private readonly jwks?: JWTVerifyGetKey;

  constructor(jwksUrl = "") {
    if (jwksUrl) this.jwks = createRemoteJWKSet(new URL(jwksUrl), { cooldownDuration: 0 });
  }

  /** 注入 kid → RSA 公钥（SPKI PEM）。主要给测试 / 小规模部署。 */
  async setStaticKey(kid: string, spkiPem: string): Promise<void> {
    this.staticKeys.set(kid, await importSPKI(spkiPem, "RS256"));
  }

  /** 验签 + aud 校验，返 ScopedClaims。错误映射：expired/aud/其他 → 三个错误类。 */
  async verify(token: string, expectedAud: string): Promise<ScopedClaims> {
    const getKey: JWTVerifyGetKey =
      this.jwks ??
      (async (header) => {
        const key = header.kid ? this.staticKeys.get(header.kid) : undefined;
        if (!key) throw new TokenInvalidError(`unknown kid ${JSON.stringify(header.kid ?? "")}`);
        return key;
      });
    let payload: JWTPayload;
    try {
      ({ payload } = await jwtVerify(token, getKey, { audience: expectedAud, algorithms: ["RS256"] }));
    } catch (err) {
      if (err instanceof TokenInvalidError) throw err; // getKey 抛的未知 kid
      const code = (err as { code?: string }).code;
      const claim = (err as { claim?: string }).claim;
      if (code === "ERR_JWT_EXPIRED") throw new TokenExpiredError((err as Error).message);
      if (code === "ERR_JWT_CLAIM_VALIDATION_FAILED" && claim === "aud") {
        throw new TokenAudienceMismatchError(`expected=${expectedAud}: ${(err as Error).message}`);
      }
      throw new TokenInvalidError((err as Error).message);
    }
    return {
      userId: s(payload.sub),
      canonicalName: s(payload.aud),
      callerId: s(payload.kx_caller_id),
      callerKind: s(payload.kx_caller_kind),
      chainId: s(payload.kx_chain_id),
      requestId: s(payload.kx_request_id),
      issuedAt: n(payload.iat),
      expiresAt: n(payload.exp),
    };
  }
}

/**
 * scopedJwtMiddleware：保护 http_endpoint capability path（hono 中间件，镜像 scoped_jwt_middleware.go）。
 * path 不在 map → pass-through；缺 Bearer → 401 missing_bearer；验签失败 → 401 分类；成功注入 claims。
 * 验签成功后 claims 同时挂到 c.set("scopedClaims", ...) 与 raw Request（供 App.handle 读）。
 */
export function scopedJwtMiddleware(
  verifier: ScopedJWTVerifier,
  pathToCanonicalName: Record<string, string>,
): MiddlewareHandler {
  return async (c, next) => {
    const expectedAud = pathToCanonicalName[c.req.path];
    if (!expectedAud) return next();
    const authHeader = c.req.header("Authorization") ?? "";
    if (!/^bearer\s/i.test(authHeader)) {
      return c.json({ error: "missing_bearer", message: "missing Bearer authorization header", code: 401 }, 401);
    }
    const token = authHeader.slice(authHeader.indexOf(" ") + 1).trim();
    try {
      const claims = await verifier.verify(token, expectedAud);
      c.set("scopedClaims", claims);
      (c.req.raw as unknown as { scopedClaims?: ScopedClaims }).scopedClaims = claims;
    } catch (err) {
      if (err instanceof TokenAudienceMismatchError) {
        return c.json({ error: "aud_mismatch", message: err.message, code: 401 }, 401);
      }
      if (err instanceof TokenExpiredError) {
        return c.json({ error: "token_expired", message: err.message, code: 401 }, 401);
      }
      return c.json({ error: "token_invalid", message: (err as Error).message, code: 401 }, 401);
    }
    return next();
  };
}
