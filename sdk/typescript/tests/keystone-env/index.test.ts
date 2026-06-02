import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { fetchKeystoneManagedEnv } from "../../src/keystone-env";

const realFetch = globalThis.fetch;
const snapshotEnv: Record<string, string | undefined> = {};

function snapshot(keys: string[]) {
  for (const k of keys) snapshotEnv[k] = process.env[k];
}
function restore(keys: string[]) {
  for (const k of keys) {
    if (snapshotEnv[k] === undefined) delete process.env[k];
    else process.env[k] = snapshotEnv[k];
  }
}

const TOUCHED = ["KS_APP_TOKEN", "KS_GATEWAY_URL", "DB_HOST", "DB_PASSWORD", "PRESET_KEY", "NEW_KEY", "X"];

beforeEach(() => {
  snapshot(TOUCHED);
  for (const k of TOUCHED) delete process.env[k];
});

afterEach(() => {
  globalThis.fetch = realFetch;
  restore(TOUCHED);
});

function stubFetchOk(env: Record<string, string>) {
  globalThis.fetch = vi.fn(async () =>
    new Response(JSON.stringify({ code: 0, data: { env } }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    }),
  ) as any;
}

describe("fetchKeystoneManagedEnv", () => {
  it("env 完全缺失时 no-op skipped=true", async () => {
    const result = await fetchKeystoneManagedEnv();
    expect(result).toEqual({ injected: [], skipped: true });
  });

  it("只缺 token 时 skipped=true", async () => {
    process.env.KS_GATEWAY_URL = "http://gw:9988";
    const result = await fetchKeystoneManagedEnv();
    expect(result.skipped).toBe(true);
    expect(result.injected).toEqual([]);
  });

  it("只缺 gateway 时 skipped=true", async () => {
    process.env.KS_APP_TOKEN = "tok";
    const result = await fetchKeystoneManagedEnv();
    expect(result.skipped).toBe(true);
  });

  it("env 都有 + fetch 成功 → 注入 process.env", async () => {
    process.env.KS_APP_TOKEN = "tok";
    process.env.KS_GATEWAY_URL = "http://gw:9988";
    stubFetchOk({ DB_HOST: "10.0.0.1", DB_PASSWORD: "s3cret" });

    const result = await fetchKeystoneManagedEnv();
    expect(result.skipped).toBe(false);
    expect(result.injected.sort()).toEqual(["DB_HOST", "DB_PASSWORD"]);
    expect(process.env.DB_HOST).toBe("10.0.0.1");
    expect(process.env.DB_PASSWORD).toBe("s3cret");
  });

  it("setdefault 风格：已存在的 key 不被覆盖", async () => {
    process.env.KS_APP_TOKEN = "tok";
    process.env.KS_GATEWAY_URL = "http://gw:9988";
    process.env.PRESET_KEY = "from-application";
    stubFetchOk({ PRESET_KEY: "from-keystone", NEW_KEY: "new-value" });

    const result = await fetchKeystoneManagedEnv();
    expect(result.injected).toEqual(["NEW_KEY"]);
    expect(process.env.PRESET_KEY).toBe("from-application"); // 未覆盖
    expect(process.env.NEW_KEY).toBe("new-value");
  });

  it("fetch 失败时 warn 不 throw", async () => {
    process.env.KS_APP_TOKEN = "tok";
    process.env.KS_GATEWAY_URL = "http://gw:9988";
    globalThis.fetch = vi.fn(async () => { throw new Error("ECONNREFUSED"); }) as any;

    const warnMock = vi.fn();
    const result = await fetchKeystoneManagedEnv({
      logger: { info: vi.fn(), warn: warnMock, error: vi.fn(), debug: vi.fn() },
    });
    expect(result).toEqual({ injected: [], skipped: false });
    expect(warnMock).toHaveBeenCalledTimes(1);
  });

  it("opts.gateway / opts.token 覆盖环境变量", async () => {
    // 环境变量为空，靠 opts 显式传入
    let capturedUrl = "";
    globalThis.fetch = vi.fn(async (url: any) => {
      capturedUrl = String(url);
      return new Response(JSON.stringify({ code: 0, data: { env: {} } }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as any;

    const result = await fetchKeystoneManagedEnv({
      gateway: "http://explicit:9988",
      token: "explicit-tok",
    });
    expect(result.skipped).toBe(false);
    expect(capturedUrl).toContain("explicit:9988");
  });

  it("keystone 返回空 env map 也算 skipped=false（no-op 但非异常）", async () => {
    process.env.KS_APP_TOKEN = "tok";
    process.env.KS_GATEWAY_URL = "http://gw:9988";
    stubFetchOk({});

    const result = await fetchKeystoneManagedEnv();
    expect(result).toEqual({ injected: [], skipped: false });
  });
});
