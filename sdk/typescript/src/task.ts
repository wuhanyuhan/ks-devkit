/**
 * CapabilityCall + Task：async capability 调用句柄。镜像 Go task.go。
 * 链式 builder（withOnBehalfOfUser / withChainContext）+ invoke(sync) / submit(async)。
 */
import type { DispatcherClient } from "./dispatcher_client";
import type { EventsClient } from "./events_client";
import { BackendError, CancelledError, DispatcherRestartedError, TimeoutError } from "./errors";

const TERMINAL = new Set(["done", "failed", "cancelled"]);

export interface TaskInit {
  taskId: string;
  status: string;
  canonicalName: string;
  percent?: number;
  stageMessage?: string;
  resultPayload?: Record<string, unknown>;
  errorCode?: string;
  errorMessage?: string;
}

export class Task {
  taskId: string;
  status: string;
  canonicalName: string;
  percent: number;
  stageMessage: string;
  resultPayload: Record<string, unknown>;
  errorCode: string;
  errorMessage: string;

  constructor(
    init: TaskInit,
    private readonly dispatcher: DispatcherClient,
    // 注：构造参数属性名用 eventsClient（避免与下面的 events() 方法名冲突）。
    private readonly eventsClient?: EventsClient,
  ) {
    this.taskId = init.taskId;
    this.status = init.status;
    this.canonicalName = init.canonicalName;
    this.percent = init.percent ?? 0;
    this.stageMessage = init.stageMessage ?? "";
    this.resultPayload = init.resultPayload ?? {};
    this.errorCode = init.errorCode ?? "";
    this.errorMessage = init.errorMessage ?? "";
  }

  /** 主动拉一次快照覆盖本地字段。 */
  async refresh(opts: { signal?: AbortSignal } = {}): Promise<void> {
    const snap = await this.dispatcher.getTask(this.taskId, opts);
    this.status = snap.status;
    this.percent = snap.percent;
    this.stageMessage = snap.stageMessage;
    if (snap.result && Object.keys(snap.result).length > 0) this.resultPayload = snap.result;
    if (snap.errorCode) this.errorCode = snap.errorCode;
    if (snap.errorMessage) this.errorMessage = snap.errorMessage;
  }

  async cancel(opts: { signal?: AbortSignal } = {}): Promise<void> {
    await this.dispatcher.cancelTask(this.taskId, opts);
  }

  /** 等待终态。done→resultPayload；failed→DispatcherRestarted(50000)/BackendError；cancelled→Cancelled。 */
  async result(opts: { pollIntervalMs?: number; signal?: AbortSignal } = {}): Promise<Record<string, unknown>> {
    const interval = opts.pollIntervalMs ?? 2000;
    for (;;) {
      await this.refresh({ signal: opts.signal });
      if (TERMINAL.has(this.status)) break;
      if (opts.signal?.aborted) throw new TimeoutError("ctx cancelled");
      await sleep(interval, opts.signal);
    }
    switch (this.status) {
      case "done":
        return this.resultPayload;
      case "failed":
        if (this.errorCode === "50000" || this.errorCode === "DispatcherRestarted") {
          throw new DispatcherRestartedError(this.errorMessage);
        }
        throw new BackendError(this.errorMessage);
      case "cancelled":
        throw new CancelledError(this.errorMessage);
      default:
        throw new BackendError(`unexpected status=${this.status}`);
    }
  }

  /** lifecycle 事件流：注入 EventsClient → WS/polling；否则发一次 snapshot 后结束。 */
  events(): AsyncIterable<Record<string, unknown>> {
    if (this.eventsClient) {
      return this.eventsClient.register(this.taskId);
    }
    const self = this;
    return {
      async *[Symbol.asyncIterator]() {
        await self.refresh();
        yield {
          type: "snapshot", task_id: self.taskId, status: self.status,
          percent: self.percent, stage_message: self.stageMessage,
        };
      },
    };
  }
}

function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const id = setTimeout(resolve, ms);
    if (signal) {
      signal.addEventListener("abort", () => { clearTimeout(id); resolve(); }, { once: true });
    }
  });
}

/**
 * CapabilityCall：app.callCapability(name) 返的链式构造器。
 * .invoke(args) 走 sync；.submit(args) 走 async 拿 Task。
 * caller 引用他人能力写全名、不派生（不对称）。
 */
export class CapabilityCall {
  private onBehalfOfUserId = 0;
  private chainId = "";
  private chainHeader = "";

  constructor(
    public readonly canonicalName: string,
    private readonly dispatcher: DispatcherClient,
    private readonly events?: EventsClient,
  ) {}

  withOnBehalfOfUser(userId: number): this {
    this.onBehalfOfUserId = userId;
    return this;
  }

  withChainContext(chainId: string, chainHeader: string): this {
    this.chainId = chainId;
    this.chainHeader = chainHeader;
    return this;
  }

  async invoke(args: Record<string, unknown>, opts: { signal?: AbortSignal } = {}): Promise<Record<string, unknown>> {
    const res = await this.dispatcher.invoke({
      capability: this.canonicalName, args, mode: "sync",
      onBehalfOfUserId: this.onBehalfOfUserId, chainId: this.chainId, chainHeader: this.chainHeader,
      signal: opts.signal,
    });
    if (res.sync) return res.sync.result;
    throw new BackendError("expected sync but got async task_id");
  }

  async submit(args: Record<string, unknown>, opts: { signal?: AbortSignal } = {}): Promise<Task> {
    const res = await this.dispatcher.invoke({
      capability: this.canonicalName, args, mode: "async",
      onBehalfOfUserId: this.onBehalfOfUserId, chainId: this.chainId, chainHeader: this.chainHeader,
      signal: opts.signal,
    });
    if (res.async) {
      return new Task(
        { taskId: res.async.taskId, status: res.async.status, canonicalName: this.canonicalName },
        this.dispatcher, this.events,
      );
    }
    if (res.sync) {
      return new Task(
        { taskId: "", status: "done", canonicalName: this.canonicalName, resultPayload: res.sync.result },
        this.dispatcher, this.events,
      );
    }
    throw new BackendError("empty invoke result");
  }
}
