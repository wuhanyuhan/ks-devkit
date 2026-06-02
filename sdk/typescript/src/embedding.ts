import { LLMRateLimitedError, LLMUnauthorizedError, LLMUpstreamError } from "./llm";

export class EmbeddingNotConfiguredError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "EmbeddingNotConfiguredError";
  }
}

export interface EmbeddingResult {
  dense: number[];
  /** bge-m3 sparse 向量：token-id 串 → 权重；upsert 时可直接塞进 Point.sparse */
  sparse: Record<string, number>;
  tokens: number;
}

export class EmbeddingClient {
  private readonly gatewayUrl: string;
  private readonly relayToken: string;
  private readonly modelName: string;
  private readonly modelDim: number;

  constructor() {
    this.gatewayUrl = (process.env.KS_GATEWAY_URL || "http://localhost:9988").replace(/\/+$/, "");
    this.relayToken = process.env.KS_RELAY_TOKEN || process.env.KEYSTONE_RELAY_TOKEN || "";
    this.modelName = process.env.KS_EMBEDDING_MODEL || "";
    this.modelDim = Number(process.env.KS_EMBEDDING_DIM || "0");
  }

  get model(): string {
    return this.modelName;
  }

  get dim(): number {
    return this.modelDim;
  }

  async embed(text: string): Promise<EmbeddingResult> {
    const results = await this.embedMany([text]);
    if (results.length === 0) throw new LLMUpstreamError("embedding response is empty");
    return results[0]!;
  }

  async embedMany(texts: string[]): Promise<EmbeddingResult[]> {
    if (!this.relayToken) throw new EmbeddingNotConfiguredError("KS_RELAY_TOKEN 未设置");
    if (!this.modelName) throw new EmbeddingNotConfiguredError("KS_EMBEDDING_MODEL 未设置");
    const data = await this.postJson("/v1/mcp/relay/embeddings", {
      model: this.modelName,
      input: texts,
      encoding_format: "dense+sparse",
    });
    const totalTokens = Number(data.usage?.total_tokens || 0);
    const tokens = texts.length > 0 ? Math.floor(totalTokens / texts.length) : 0;
    const results: EmbeddingResult[] = new Array(texts.length);
    for (const item of data.data || []) {
      const idx = Number(item.index);
      results[idx] = { dense: item.embedding || [], sparse: item.sparse_embedding || {}, tokens };
    }
    return results;
  }

  async postJson(path: string, body: unknown): Promise<any> {
    if (!this.relayToken) throw new EmbeddingNotConfiguredError("KS_RELAY_TOKEN 未设置");
    const resp = await fetch(`${this.gatewayUrl}${path}`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${this.relayToken}`,
      },
      body: JSON.stringify(body),
    });
    if (!resp.ok) {
      const text = await resp.text();
      throwHttpError(resp.status, text);
    }
    return await resp.json();
  }
}

function throwHttpError(status: number, body: string): never {
  const msg = `status=${status} body=${body.slice(0, 500)}`;
  if (status === 401 || status === 403) throw new LLMUnauthorizedError(msg);
  if (status === 429) throw new LLMRateLimitedError(msg);
  throw new LLMUpstreamError(msg);
}
