import { describe, it, expect } from "vitest";
import {
  KeystoneError, NotFoundError, CapabilityNotFoundError, TaskNotFoundError,
  AuthError, TokenExpiredError, TokenAudienceMismatchError, TokenInvalidError,
  CapabilityForbiddenError, CapabilityDisabledError, InvalidArgsError,
  CapabilityUnavailableError, LoopDetectedError, GuardrailBlockedError,
  BackendError, TimeoutError, RateLimitError, ManifestMismatchError,
  mapHttpError,
} from "./errors";

describe("error hierarchy", () => {
  it("叶子 instanceof 中间类与根（镜像 Go sentinel 树）", () => {
    const e = new CapabilityNotFoundError("ks-mcp-x.foo");
    expect(e).toBeInstanceOf(NotFoundError);
    expect(e).toBeInstanceOf(KeystoneError);
    expect(e.canonicalName).toBe("ks-mcp-x.foo");
    expect(e.name).toBe("CapabilityNotFoundError");
  });
  it("具体类携带上下文字段", () => {
    expect(new TaskNotFoundError("t1").taskId).toBe("t1");
    expect(new CapabilityUnavailableError("down", 1500).retryAfterMs).toBe(1500);
    expect(new RateLimitError("slow", 2000).retryAfterMs).toBe(2000);
    expect(new TimeoutError(undefined, { deadlineMs: 10, elapsedMs: 20 }).elapsedMs).toBe(20);
    const mm = new ManifestMismatchError("ks-mcp-x.foo", ["ks-mcp-x.bar"]);
    expect(mm.registered).toBe("ks-mcp-x.foo");
    expect(mm.manifestNames).toEqual(["ks-mcp-x.bar"]);
  });
});

function H(extra: Record<string, string> = {}): Headers {
  return new Headers(extra);
}

describe("mapHttpError 状态码映射（逐条镜像 dispatcher_client.go）", () => {
  it("400 → InvalidArgs", () => {
    expect(mapHttpError(400, JSON.stringify({ message: "bad" }), H())).toBeInstanceOf(InvalidArgsError);
  });
  it("401 按 code 分流", () => {
    expect(mapHttpError(401, JSON.stringify({ code: 40103 }), H())).toBeInstanceOf(TokenAudienceMismatchError);
    expect(mapHttpError(401, JSON.stringify({ code: 40102 }), H())).toBeInstanceOf(TokenExpiredError);
    expect(mapHttpError(401, JSON.stringify({ code: 1 }), H())).toBeInstanceOf(TokenInvalidError);
    expect(mapHttpError(401, JSON.stringify({ code: 1 }), H())).toBeInstanceOf(AuthError);
  });
  it("403 按 code 分流", () => {
    expect(mapHttpError(403, JSON.stringify({ code: 40301 }), H())).toBeInstanceOf(CapabilityDisabledError);
    expect(mapHttpError(403, JSON.stringify({ code: 1 }), H())).toBeInstanceOf(CapabilityForbiddenError);
  });
  it("404 → CapabilityNotFound(hint)", () => {
    const e = mapHttpError(404, "{}", H(), "ks-mcp-x.foo") as CapabilityNotFoundError;
    expect(e).toBeInstanceOf(CapabilityNotFoundError);
    expect(e.canonicalName).toBe("ks-mcp-x.foo");
  });
  it("408/429/451/502/503/508/其他", () => {
    expect(mapHttpError(408, "{}", H())).toBeInstanceOf(TimeoutError);
    const rl = mapHttpError(429, "{}", H({ "Retry-After": "2" })) as RateLimitError;
    expect(rl).toBeInstanceOf(RateLimitError);
    expect(rl.retryAfterMs).toBe(2000);
    expect(mapHttpError(451, "{}", H())).toBeInstanceOf(GuardrailBlockedError);
    expect(mapHttpError(502, "{}", H())).toBeInstanceOf(BackendError);
    const cu = mapHttpError(503, "{}", H({ "Retry-After": "1.5" })) as CapabilityUnavailableError;
    expect(cu).toBeInstanceOf(CapabilityUnavailableError);
    expect(cu.retryAfterMs).toBe(1500);
    expect(mapHttpError(508, "{}", H())).toBeInstanceOf(LoopDetectedError);
    expect(mapHttpError(599, "{}", H())).toBeInstanceOf(BackendError);
  });
});
