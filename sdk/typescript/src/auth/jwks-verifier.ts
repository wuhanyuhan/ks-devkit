import { createRemoteJWKSet, jwtVerify, type JWTPayload } from "jose";

export interface JWKSVerifier {
  verify(authHeader: string | null | undefined): Promise<JWTPayload>;
}

/**
 * 创建 JWKS 验证器。内部用 jose.createRemoteJWKSet（自带 cache + fetch on unknown kid）。
 *
 * 拒绝规则（对齐 conformance §3）：
 *   - 缺 Authorization header
 *   - 非 Bearer 格式
 *   - alg != RS256 或缺 kid（jose 默认行为）
 *   - 签名验证失败
 *   - exp 过期
 */
export function createJWKSVerifier(jwksUrl: string): JWKSVerifier {
  if (!jwksUrl) {
    throw new Error("createJWKSVerifier: jwks URL is empty");
  }

  // cooldownDuration=0：未知 kid 时立即重拉 JWKS，不等待冷却期。
  // 对齐 conformance §3 "MUST refetch JWKS on unknown kid" 要求。
  const jwks = createRemoteJWKSet(new URL(jwksUrl), { cooldownDuration: 0 });

  return {
    async verify(authHeader) {
      if (!authHeader) {
        throw new Error("missing Authorization header");
      }
      if (!authHeader.startsWith("Bearer ")) {
        throw new Error("Authorization must be Bearer scheme");
      }
      const token = authHeader.slice(7);
      const { payload } = await jwtVerify(token, jwks, {
        algorithms: ["RS256"],
      });
      return payload;
    },
  };
}
