import type { EmbeddingClient } from "./embedding";

export interface Point {
  id: string;
  dense: number[];
  /** bge-m3 sparse：token-id 串 → 权重；可直接取自 EmbeddingResult.sparse */
  sparse?: Record<string, number>;
  payload?: Record<string, unknown>;
}

export type Filter = Record<string, unknown>;

export interface SearchOptions {
  topK?: number;
  filter?: Filter;
}

export interface SearchResult {
  id: string;
  score: number;
  payload?: Record<string, unknown>;
}

export class VectorStoreClient {
  constructor(
    private readonly embedding: EmbeddingClient,
    private readonly collection: string,
  ) {}

  async upsert(points: Point[]): Promise<void> {
    await this.embedding.postJson("/v1/mcp/relay/vector_store/upsert", {
      collection: this.collection,
      points,
    });
  }

  // searchText 由服务端 embed dense+sparse 后做 RRF hybrid 检索（托管向量链唯一检索路径）。
  async searchText(text: string, opts: SearchOptions = {}): Promise<SearchResult[]> {
    return await this.searchRaw("/v1/mcp/relay/vector_store/search_text", {
      collection: this.collection,
      text,
      top_k: opts.topK ?? 5,
      ...(opts.filter ? { filter: opts.filter } : {}),
    });
  }

  async delete(ids: string[]): Promise<void> {
    await this.embedding.postJson("/v1/mcp/relay/vector_store/delete", {
      collection: this.collection,
      ids,
    });
  }

  async deleteByFilter(filter: Filter): Promise<void> {
    await this.embedding.postJson("/v1/mcp/relay/vector_store/delete", {
      collection: this.collection,
      filter,
    });
  }

  async count(filter?: Filter): Promise<number> {
    const data = await this.embedding.postJson("/v1/mcp/relay/vector_store/count", {
      collection: this.collection,
      ...(filter ? { filter } : {}),
    });
    return Number(data.count || 0);
  }

  private async searchRaw(path: string, body: unknown): Promise<SearchResult[]> {
    const data = await this.embedding.postJson(path, body);
    return data.results || [];
  }
}
