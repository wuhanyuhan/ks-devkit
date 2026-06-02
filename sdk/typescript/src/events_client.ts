/**
 * EventsClient：caller-side inbound 事件通道（镜像 Go events.go）。
 * 双模式：polling（GET ?since=cursor）/ ws（globalThis.WebSocket）。
 * per-task EventStream 以 AsyncIterable 暴露（映射 Go buffered channel）。
 *
 * WS transport 用 globalThis.WebSocket：Bun 原生；Node 22+ 原生；Node 20 需
 * --experimental-websocket（特性探测，缺失则 ws 模式降级为不连，polling 始终可用）。
 */
export type EventsMode = "ws" | "polling";
export type Event = Record<string, unknown>;

/** EventStream：单个 task 的事件流，AsyncIterable。push 入队、close 结束。 */
export class EventStream implements AsyncIterable<Event> {
  private queue: Event[] = [];
  private waiters: ((r: IteratorResult<Event>) => void)[] = [];
  private closed = false;

  constructor(readonly taskId: string) {}

  push(event: Event): void {
    if (this.closed) return;
    const waiter = this.waiters.shift();
    if (waiter) waiter({ value: event, done: false });
    else this.queue.push(event);
  }

  close(): void {
    if (this.closed) return;
    this.closed = true;
    for (const w of this.waiters) w({ value: undefined as unknown as Event, done: true });
    this.waiters = [];
  }

  [Symbol.asyncIterator](): AsyncIterator<Event> {
    return {
      next: (): Promise<IteratorResult<Event>> => {
        if (this.queue.length > 0) {
          return Promise.resolve({ value: this.queue.shift()!, done: false });
        }
        if (this.closed) return Promise.resolve({ value: undefined as unknown as Event, done: true });
        return new Promise((resolve) => this.waiters.push(resolve));
      },
    };
  }
}

function defaultGateway(): string {
  return (process.env.KS_GATEWAY_URL || "http://localhost:9988").replace(/\/+$/, "");
}
function defaultToken(): string {
  return process.env.KS_RELAY_TOKEN || process.env.KEYSTONE_RELAY_TOKEN || "";
}

export class EventsClient {
  private readonly gatewayUrl: string;
  private readonly appToken: string;
  private readonly mode: EventsMode;
  private readonly streams = new Map<string, EventStream>();
  private pollingCursor = "";
  private started = false;
  private stopped = false;
  private ws?: WebSocket;

  constructor(gatewayUrl?: string, appToken?: string, mode: EventsMode = "polling") {
    this.gatewayUrl = (gatewayUrl ?? defaultGateway()).replace(/\/+$/, "");
    this.appToken = appToken ?? defaultToken();
    this.mode = mode;
  }

  register(taskId: string): EventStream {
    const existing = this.streams.get(taskId);
    if (existing) return existing;
    const s = new EventStream(taskId);
    this.streams.set(taskId, s);
    return s;
  }

  unregister(taskId: string): void {
    const s = this.streams.get(taskId);
    if (s) { this.streams.delete(taskId); s.close(); }
  }

  /** 启动后台 loop（幂等）。polling 用定时器；ws 用 globalThis.WebSocket（特性探测）。 */
  start(): void {
    if (this.started) return;
    this.started = true;
    if (this.mode === "polling") void this.pollingLoop();
    else this.wsConnect();
  }

  close(): void {
    this.stopped = true;
    try { this.ws?.close(); } catch { /* ignore */ }
    for (const s of this.streams.values()) s.close();
    this.streams.clear();
  }

  private dispatch(event: Event): void {
    const taskId = typeof event.task_id === "string" ? event.task_id : "";
    if (!taskId) return;
    const stream = this.streams.get(taskId);
    if (!stream) return; // 未注册静默丢弃（事件可重放）
    stream.push(event);
  }

  private async pollingLoop(): Promise<void> {
    while (!this.stopped) {
      try { await this.pollOnce(); } catch { /* transient：保留 cursor 下次再拉 */ }
      await sleep(2000);
    }
  }

  /** pollOnce：GET /v1/apps/self/events?since=<cursor>，dispatch events + 更新 cursor。 */
  private async pollOnce(): Promise<void> {
    const since = this.pollingCursor || "0";
    const resp = await fetch(`${this.gatewayUrl}/v1/apps/self/events?since=${encodeURIComponent(since)}`, {
      method: "GET",
      headers: { Authorization: `Bearer ${this.appToken}` },
    });
    if (resp.status !== 200) return; // transient
    const env = (await resp.json()) as { data?: { events?: Event[]; next_cursor?: string } };
    for (const ev of env.data?.events ?? []) this.dispatch(ev);
    if (env.data?.next_cursor) this.pollingCursor = env.data.next_cursor;
  }

  private wsConnect(): void {
    if (typeof globalThis.WebSocket === "undefined") {
      // Node 20 无原生 WebSocket：ws 模式降级为不连（polling 始终可用作兜底）。
      return;
    }
    const wsUrl = this.gatewayUrl.replace(/^http/, "ws") + "/v1/apps/self/events";
    const ws = new WebSocket(wsUrl);
    this.ws = ws;
    ws.addEventListener("message", (ev: MessageEvent) => {
      try {
        const event = JSON.parse(typeof ev.data === "string" ? ev.data : "") as Event;
        if (event.type === "heartbeat") return;
        this.dispatch(event);
      } catch { /* ignore malformed */ }
    });
    ws.addEventListener("close", () => {
      if (!this.stopped) setTimeout(() => this.wsConnect(), 1000); // 简化重连（Go 指数退避）
    });
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
