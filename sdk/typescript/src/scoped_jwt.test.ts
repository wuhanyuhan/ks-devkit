import { describe, it, expect, beforeAll } from "vitest";
// jose v6：无 KeyLike 导出，密钥类型用 CryptoKey（importSPKI/generateKeyPair 的返回类型）。
import { SignJWT, exportSPKI, generateKeyPair, type CryptoKey } from "jose";
import { ScopedJWTVerifier } from "./scoped_jwt";
import { TokenExpiredError, TokenAudienceMismatchError, TokenInvalidError } from "./errors";

let priv: CryptoKey;
let spki: string;
const KID = "test-kid";

beforeAll(async () => {
  const kp = await generateKeyPair("RS256");
  priv = kp.privateKey;
  spki = await exportSPKI(kp.publicKey);
});

async function sign(claims: Record<string, unknown>, opts: { aud: string; expSec?: number }) {
  const now = Math.floor(Date.now() / 1000);
  return await new SignJWT(claims)
    .setProtectedHeader({ alg: "RS256", kid: KID })
    .setIssuedAt(now)
    .setExpirationTime(now + (opts.expSec ?? 300))
    .setAudience(opts.aud)
    .sign(priv);
}

describe("ScopedJWTVerifier.verify（kx_ claims）", () => {
  it("happy：映射 kx_caller_id 等", async () => {
    const v = new ScopedJWTVerifier();
    await v.setStaticKey(KID, spki);
    const token = await sign(
      { sub: "7", kx_caller_id: "app-7", kx_caller_kind: "app", kx_chain_id: "chn_1", kx_request_id: "req-1" },
      { aud: "ks-mcp-x.foo" },
    );
    const claims = await v.verify(token, "ks-mcp-x.foo");
    expect(claims.userId).toBe("7");
    expect(claims.canonicalName).toBe("ks-mcp-x.foo");
    expect(claims.callerId).toBe("app-7");
    expect(claims.chainId).toBe("chn_1");
  });

  it("过期 → TokenExpired", async () => {
    const v = new ScopedJWTVerifier();
    await v.setStaticKey(KID, spki);
    const token = await sign({ sub: "7" }, { aud: "ks-mcp-x.foo", expSec: -10 });
    await expect(v.verify(token, "ks-mcp-x.foo")).rejects.toBeInstanceOf(TokenExpiredError);
  });

  it("aud 不符 → TokenAudienceMismatch", async () => {
    const v = new ScopedJWTVerifier();
    await v.setStaticKey(KID, spki);
    const token = await sign({ sub: "7" }, { aud: "ks-mcp-x.other" });
    await expect(v.verify(token, "ks-mcp-x.foo")).rejects.toBeInstanceOf(TokenAudienceMismatchError);
  });

  it("未知 kid → TokenInvalid", async () => {
    const v = new ScopedJWTVerifier();
    const token = await sign({ sub: "7" }, { aud: "ks-mcp-x.foo" });
    await expect(v.verify(token, "ks-mcp-x.foo")).rejects.toBeInstanceOf(TokenInvalidError);
  });
});
