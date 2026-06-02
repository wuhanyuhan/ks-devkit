/**
 * DispatcherClient：与 keystone capability dispatcher 通讯的 HTTP 客户端。
 * 镜像 Go dispatcher_client.go。wire body snake_case；API 选项 camelCase；用 globalThis.fetch。
 */
import {
  BackendError, CapabilityNotFoundError, CapabilityUnavailableError, TaskNotFoundError, mapHttpError,
} from "./errors";

const HEADER_CALL_CHAIN = "X-Keystone-Call-Chain";
const HEADER_CHAIN_ID = "X-Keystone-Chain-Id";

export interface InvokeOptions {
  capability: string;
  args?: Record<string, unknown>;
  mode: string; // "sync" | "async" | "auto"
  idempotencyKey?: string;
  timeoutMsOverride?: number;
  chainId?: string;
  chainHeader?: string;
  /** 调用链发起人 user_id；仅 >0 时写入 payload（穿透多跳 capability mesh）。 */
  onBehalfOfUserId?: number;
  signal?: AbortSignal;
}

export interface InvokeSyncResult {
  result: Record<string, unknown>;
  durationMs: number;
}
export interface InvokeAsyncResult {
  taskId: string;
  status: string;
  submittedAt: string;
  timeoutAt: string;
}
export interface InvokeResult {
  sync?: InvokeSyncResult;
  async?: InvokeAsyncResult;
}

export interface TaskSnapshot {
  taskId: string;
  status: string;
  canonicalName: string;
  percent: number;
  stageMessage: string;
  result: Record<string, unknown>;
  errorCode: string;
  errorMessage: string;
}

function defaultGateway(): string {
  return (process.env.KS_GATEWAY_URL || "http://localhost:9988").replace(/\/+$/, "");
}
function defaultToken(): string {
  return process.env.KS_RELAY_TOKEN || process.env.KEYSTONE_RELAY_TOKEN || "";
}

export class DispatcherClient {
  private readonly gatewayUrl: string;
  private readonly appToken: string;

  constructor(gatewayUrl?: string, appToken?: string) {
    this.gatewayUrl = (gatewayUrl ?? defaultGateway()).replace(/\/+$/, "");
    this.appToken = appToken ?? defaultToken();
  }

  async invoke(opts: InvokeOptions): Promise<InvokeResult> {
    const payload: Record<string, unknown> = { capability: opts.capability, mode: opts.mode };
    if (opts.args !== undefined) payload.args = opts.args;
    if (opts.idempotencyKey) payload.idempotency_key = opts.idempotencyKey;
    if (opts.timeoutMsOverride !== undefined) payload.timeout_ms_override = opts.timeoutMsOverride;
    if (opts.onBehalfOfUserId !== undefined && opts.onBehalfOfUserId > 0) {
      payload.on_behalf_of_user_id = opts.onBehalfOfUserId;
    }
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      Authorization: `Bearer ${this.appToken}`,
    };
    if (opts.chainId && opts.chainId.trim()) headers[HEADER_CHAIN_ID] = opts.chainId;
    if (opts.chainHeader && opts.chainHeader.trim()) headers[HEADER_CALL_CHAIN] = opts.chainHeader;

    const data = await this.postEnvelope("/v1/apps/self/invoke", payload, headers, opts.capability, opts.signal);
    const taskId = data.task_id;
    if (typeof taskId === "string" && taskId !== "") {
      return {
        async: {
          taskId,
          status: typeof data.status === "string" ? data.status : "",
          submittedAt: typeof data.submitted_at === "string" ? data.submitted_at : "",
          timeoutAt: typeof data.timeout_at === "string" ? data.timeout_at : "",
        },
      };
    }
    return {
      sync: {
        result: (data.result as Record<string, unknown>) ?? {},
        durationMs: typeof data.duration_ms === "number" ? data.duration_ms : 0,
      },
    };
  }

  async reportProgress(taskId: string, stage: string, percent?: number): Promise<void> {
    const payload: Record<string, unknown> = { stage_message: stage };
    if (percent !== undefined) payload.percent = percent;
    try {
      await this.postEnvelope(`/v1/user-tasks/${taskId}/progress`, payload, this.baseHeaders());
    } catch {
      // best-effort：进度上报失败不抛
    }
  }

  async getTask(taskId: string, opts: { signal?: AbortSignal } = {}): Promise<TaskSnapshot> {
    const resp = await fetch(`${this.gatewayUrl}/v1/user-tasks/${taskId}`, {
      method: "GET",
      headers: { Authorization: `Bearer ${this.appToken}` },
      signal: opts.signal,
    });
    const body = await resp.text();
    if (resp.status >= 400) {
      throw this.remapNotFoundToTask(mapHttpError(resp.status, body, resp.headers, taskId), taskId);
    }
    const env = this.parseEnvelope(body);
    const d = (env.data ?? {}) as Record<string, unknown>;
    return {
      taskId: typeof d.task_id === "string" ? d.task_id : "",
      status: typeof d.status === "string" ? d.status : "",
      canonicalName: typeof d.canonical_name === "string" ? d.canonical_name : "",
      percent: typeof d.percent === "number" ? d.percent : 0,
      stageMessage: typeof d.stage_message === "string" ? d.stage_message : "",
      result: (d.result as Record<string, unknown>) ?? {},
      errorCode: typeof d.error_code === "string" ? d.error_code : "",
      errorMessage: typeof d.error_message === "string" ? d.error_message : "",
    };
  }

  async cancelTask(taskId: string, opts: { signal?: AbortSignal } = {}): Promise<void> {
    try {
      await this.postEnvelope(`/v1/user-tasks/${taskId}/cancel`, {}, this.baseHeaders(), taskId, opts.signal);
    } catch (err) {
      throw this.remapNotFoundToTask(err as Error, taskId);
    }
  }

  private baseHeaders(): Record<string, string> {
    return { "Content-Type": "application/json", Authorization: `Bearer ${this.appToken}` };
  }

  private async postEnvelope(
    path: string,
    payload: Record<string, unknown>,
    headers: Record<string, string>,
    capHint = "",
    signal?: AbortSignal,
  ): Promise<Record<string, unknown>> {
    const resp = await fetch(`${this.gatewayUrl}${path}`, {
      method: "POST",
      headers,
      body: JSON.stringify(payload),
      signal,
    });
    const body = await resp.text();
    if (resp.status >= 400) {
      throw mapHttpError(resp.status, body, resp.headers, capHint);
    }
    const env = this.parseEnvelope(body);
    if (env.code !== 0) {
      throw new BackendError(`business code=${env.code} message=${env.message}`);
    }
    return (env.data ?? {}) as Record<string, unknown>;
  }

  private parseEnvelope(body: string): { code: number; message: string; data?: unknown } {
    try {
      const env = JSON.parse(body) as { code?: number; message?: string; data?: unknown };
      return { code: env.code ?? 0, message: env.message ?? "", data: env.data };
    } catch (err) {
      throw new BackendError(`parse json: ${(err as Error).message}`);
    }
  }

  private remapNotFoundToTask(err: Error, taskId: string): Error {
    if (err instanceof CapabilityNotFoundError) return new TaskNotFoundError(taskId);
    return err;
  }
}

/** 仅为消除 "unused import" 风险的显式重导出占位（CapabilityUnavailableError 由 mapHttpError 内部使用）。 */
export type { CapabilityUnavailableError };
