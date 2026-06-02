import { createLogger, type Logger } from "../logger";
import { SelfClient, KeystoneSelfFetchError } from "./self-client";

export { SelfClient, KeystoneSelfFetchError } from "./self-client";
export type { SelfClientOptions } from "./self-client";

export interface FetchManagedEnvOptions {
  /** 默认 process.env.KS_GATEWAY_URL */
  gateway?: string;
  /** 默认 process.env.KS_APP_TOKEN */
  token?: string;
  /** 默认 SDK 内置 createLogger() */
  logger?: Logger;
  /** 默认 globalThis.fetch（注入测试用） */
  fetch?: typeof globalThis.fetch;
}

export interface FetchManagedEnvResult {
  /** 实际注入到 process.env 的 key 列表 */
  injected: string[];
  /** true = env 缺失导致 no-op；false = 至少试过 fetch */
  skipped: boolean;
}

/**
 * 探测 KS_APP_TOKEN + KS_GATEWAY_URL，存在则拉 keystone 并 setdefault 注入 process.env。
 * - 两个 env 任一缺失 → return {injected:[], skipped:true}
 * - fetch 成功 → 对每个返回 key：若 process.env[key] === undefined 则注入
 * - fetch 失败 → logger.warn(...) 后 return {injected:[], skipped:false}
 *
 * 调用约定：在 entrypoint 顶部（任何 loadConfig / 读 env 之前）顶层 await。
 */
export async function fetchKeystoneManagedEnv(
  opts?: FetchManagedEnvOptions,
): Promise<FetchManagedEnvResult> {
  const token = opts?.token ?? process.env.KS_APP_TOKEN;
  const gateway = opts?.gateway ?? process.env.KS_GATEWAY_URL;

  if (!token || !gateway) {
    return { injected: [], skipped: true };
  }

  const logger = opts?.logger ?? createLogger();
  const client = new SelfClient({ gateway, token, fetch: opts?.fetch });

  let env: Record<string, string>;
  try {
    env = await client.fetchEnv();
  } catch (err) {
    logger.warn("fetchKeystoneManagedEnv failed", {
      err: err instanceof KeystoneSelfFetchError ? err.message : String(err),
      gateway,
    });
    return { injected: [], skipped: false };
  }

  const injected: string[] = [];
  for (const [k, v] of Object.entries(env)) {
    if (process.env[k] === undefined) {
      process.env[k] = v;
      injected.push(k);
    }
  }
  return { injected, skipped: false };
}
