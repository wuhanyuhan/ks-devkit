/**
 * CapabilityContext：capability handler 收到的运行时上下文。
 * 镜像 Go capability_context.go / Python capability_context.py。
 *
 * TS 适配：Go context.Context（取消/超时/透传）→ TS AbortSignal；cancelled()=signal.aborted；
 * deadline() 返 unix ms。字段 API 面 camelCase；mcp_tool 路径字段源自 _meta.ks_*。
 */

/** progress 上报回调（由 DispatcherClient.reportProgress 注入；避免循环 import）。 */
export type ProgressReporter = (taskId: string, stage: string, percent?: number) => Promise<void>;

export interface CapabilityContext {
  readonly userId: string;
  readonly callerId: string;
  readonly callerKind: string;
  readonly chainId: string;
  readonly chainHeader: string;
  readonly taskId: string;
  readonly requestId: string;
  readonly canonicalName: string;
  /** 透传给下游 fetch / 数据库的取消信号（对齐 Go ctx 透传）。 */
  readonly signal: AbortSignal;
  /** 上报进度（仅 long_running 任务有效；taskId="" 或无 reporter 时 no-op）。 */
  progress(stage: string, percent?: number): Promise<void>;
  /** 任务超时点 unix ms；timeoutMs<=0 返 0。 */
  deadline(): number;
  /** cooperative cancellation 检查（= signal.aborted）。 */
  cancelled(): boolean;
}

export interface CapabilityContextInit {
  userId?: string;
  callerId?: string;
  callerKind?: string;
  chainId?: string;
  chainHeader?: string;
  taskId?: string;
  requestId?: string;
  canonicalName: string;
  timeoutMs?: number;
  startedAtMs?: number;
  reportProgress?: ProgressReporter;
  signal?: AbortSignal;
}

class CapabilityContextImpl implements CapabilityContext {
  readonly signal: AbortSignal;
  private readonly controller?: AbortController;
  private readonly startedAtMs: number;

  constructor(private readonly init: CapabilityContextInit) {
    if (init.signal) {
      this.signal = init.signal;
    } else {
      this.controller = new AbortController();
      this.signal = this.controller.signal;
    }
    this.startedAtMs = init.startedAtMs ?? Date.now();
  }

  get userId(): string { return this.init.userId ?? ""; }
  get callerId(): string { return this.init.callerId ?? ""; }
  get callerKind(): string { return this.init.callerKind ?? ""; }
  get chainId(): string { return this.init.chainId ?? ""; }
  get chainHeader(): string { return this.init.chainHeader ?? ""; }
  get taskId(): string { return this.init.taskId ?? ""; }
  get requestId(): string { return this.init.requestId ?? ""; }
  get canonicalName(): string { return this.init.canonicalName; }

  async progress(stage: string, percent?: number): Promise<void> {
    if (!this.taskId || !this.init.reportProgress) return;
    try {
      await this.init.reportProgress(this.taskId, stage, percent);
    } catch {
      // best-effort：进度丢失不应让业务 handler 失败
    }
  }

  deadline(): number {
    const t = this.init.timeoutMs ?? 0;
    return t <= 0 ? 0 : this.startedAtMs + t;
  }

  cancelled(): boolean {
    return this.signal.aborted;
  }
}

export function createCapabilityContext(init: CapabilityContextInit): CapabilityContext {
  return new CapabilityContextImpl(init);
}

function metaString(meta: Record<string, unknown> | null | undefined, key: string): string {
  if (!meta || typeof meta !== "object") return "";
  const v = (meta as Record<string, unknown>)[key];
  if (v === undefined || v === null) return "";
  return typeof v === "string" ? v : String(v);
}

/**
 * buildContextFromMeta：从 MCP _meta.ks_* 构造 CapabilityContext（mcp_tool backend 生成 tool 路径）。
 * Bug#2：chainHeader 取 ks_chain_snapshot（keystone mcptool executor 透传后非空，见 plan 关键契约 6）。
 */
export function buildContextFromMeta(
  meta: Record<string, unknown> | null | undefined,
  opts: { canonicalName: string; timeoutMs?: number; reportProgress?: ProgressReporter; signal?: AbortSignal },
): CapabilityContext {
  return createCapabilityContext({
    userId: metaString(meta, "ks_user_id"),
    callerId: metaString(meta, "ks_caller_id"),
    callerKind: metaString(meta, "ks_caller_kind"),
    chainId: metaString(meta, "ks_chain_id"),
    chainHeader: metaString(meta, "ks_chain_snapshot"),
    taskId: metaString(meta, "ks_task_id"),
    requestId: metaString(meta, "ks_request_id"),
    canonicalName: opts.canonicalName,
    timeoutMs: opts.timeoutMs,
    reportProgress: opts.reportProgress,
    signal: opts.signal,
  });
}

export interface CallerContext {
  userId: string;
  callerId: string;
  callerKind: string;
  chainId: string;
  chainHeader: string;
  taskId: string;
  requestId: string;
}

/**
 * extractCallerContext：复用降级 helper。复用普通 app.tool 时 handler 拿不到
 * CapabilityContext，caller 上下文在 args._meta.ks_*——本 helper 从中提取。
 */
export function extractCallerContext(args: Record<string, unknown>): CallerContext {
  const meta = (args?._meta ?? {}) as Record<string, unknown>;
  return {
    userId: metaString(meta, "ks_user_id"),
    callerId: metaString(meta, "ks_caller_id"),
    callerKind: metaString(meta, "ks_caller_kind"),
    chainId: metaString(meta, "ks_chain_id"),
    chainHeader: metaString(meta, "ks_chain_snapshot"),
    taskId: metaString(meta, "ks_task_id"),
    requestId: metaString(meta, "ks_request_id"),
  };
}
