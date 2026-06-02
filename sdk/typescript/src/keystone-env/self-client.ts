export interface SelfClientOptions {
  /** keystone API gateway base URL，e.g. "http://keystone-api:9988" */
  gateway: string;
  /** KS_APP_TOKEN（keystone 平台安装时签发） */
  token: string;
  /** 默认 globalThis.fetch；测试时可注入 mock */
  fetch?: typeof globalThis.fetch;
  /** 默认 10_000ms */
  timeoutMs?: number;
}

export class KeystoneSelfFetchError extends Error {
  readonly name = "KeystoneSelfFetchError";
  constructor(
    message: string,
    public readonly status?: number,
    public readonly cause?: unknown,
  ) {
    super(message);
  }
}

interface SelfResourcesResponse {
  code?: number;
  data?: { env?: Record<string, string> };
}

export class SelfClient {
  constructor(private readonly opts: SelfClientOptions) {}

  async fetchEnv(): Promise<Record<string, string>> {
    const url = `${this.opts.gateway.replace(/\/$/, "")}/v1/apps/self/resources`;
    const timeoutMs = this.opts.timeoutMs ?? 10_000;
    const fetchImpl = this.opts.fetch ?? globalThis.fetch;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    const timeoutPromise = new Promise<never>((_resolve, reject) => {
      timer = setTimeout(() => {
        controller.abort();
        reject(new KeystoneSelfFetchError(`keystone self-resources fetch timed out after ${timeoutMs}ms`));
      }, timeoutMs);
    });

    let resp: Response;
    try {
      resp = await Promise.race([
        fetchImpl(url, {
          method: "GET",
          headers: {
            authorization: `Bearer ${this.opts.token}`,
            accept: "application/json",
          },
          signal: controller.signal,
        }),
        timeoutPromise,
      ]);
    } catch (err) {
      if (err instanceof KeystoneSelfFetchError) throw err;
      throw new KeystoneSelfFetchError(
        `keystone self-resources fetch failed: ${(err as Error).message ?? String(err)}`,
        undefined,
        err,
      );
    } finally {
      if (timer !== undefined) clearTimeout(timer);
    }

    if (resp.status >= 400) {
      throw new KeystoneSelfFetchError(
        `keystone returned HTTP ${resp.status}`,
        resp.status,
      );
    }

    let body: SelfResourcesResponse;
    try {
      body = (await resp.json()) as SelfResourcesResponse;
    } catch (err) {
      throw new KeystoneSelfFetchError("keystone response is not valid JSON", resp.status, err);
    }

    if (!body || typeof body !== "object" || !body.data || typeof body.data.env !== "object" || body.data.env === null) {
      throw new KeystoneSelfFetchError("keystone response missing data.env");
    }

    return body.data.env;
  }
}
