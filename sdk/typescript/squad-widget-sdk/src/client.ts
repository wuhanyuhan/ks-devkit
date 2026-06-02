/**
 * @wuhanyuhan/squad-widget-sdk — iframe-side widget client。
 *
 * 在 squad 自定义 widget 的 iframe 内部调用 `createWidgetClient`，与 keystone host 通过
 * postMessage 双向 JSON-RPC 通讯。所有 method 名严格对齐 ks-types `widgets.d.ts`。
 *
 * 核心承诺：
 * - 自动发 `app.ready`（构造时立即触发）
 * - `callServerTool` 30s 超时
 * - `resize` 100ms 节流：连续 N 次 → 首发 + 末尾 trailing 两次
 * - 入站消息严格 source 校验（仅接受 `event.source === window.parent`）
 * - 入站消息必须满足 `data.jsonrpc === '2.0'` 才进入分发
 */

import {
  WIDGET_METHOD,
  type HostToIframeMessage,
  type IframeToHostMessage,
  type NotifyLevel,
  type NotifyParams,
  type OpenLinkParams,
  type ResizeParams,
  type UpdateModelContextParams,
  type WidgetContext,
  type WidgetDataParams,
  type WidgetError,
} from './protocol'

/** 默认 callServerTool 超时（ms） */
const DEFAULT_TOOL_TIMEOUT_MS = 30_000

/** resize 节流窗口（ms） */
const RESIZE_THROTTLE_MS = 100

/** 调用方传入的工厂参数 */
export interface CreateWidgetClientOptions<TData = unknown> {
  /**
   * host 下发 `app.data` 时的回调（首帧 + 后续更新）。
   * data 与 context 都来自 host，业务侧在此渲染 UI。
   */
  onData?: (data: TData, context: WidgetContext) => void
  /**
   * 自定义 callServerTool 超时（ms），默认 30000。
   */
  timeoutMs?: number
  /**
   * 注入 window 引用（仅供测试覆盖，生产侧不要传）。
   */
  win?: Window
  /**
   * 注入 parent window 引用（仅供测试覆盖，生产侧不要传）。
   */
  parentWin?: Window
}

/** SDK 暴露给业务侧的 widget client 句柄 */
export interface WidgetClient {
  /**
   * 反向调用 server-side tool。
   * @param name tool 名（squad 自定义）
   * @param args tool 参数（任意可序列化对象）
   * @returns Promise，host 回 result 时 resolve；回 error 或 30s 超时时 reject
   */
  callServerTool: <TResult = unknown>(name: string, args?: unknown) => Promise<TResult>
  /** 通知 host iframe 高度变化（节流 100ms：首发 + trailing） */
  resize: (height: number) => void
  /** 请求 host 关闭当前 widget */
  close: () => void
  /** 通知 host 弹出全局通知（level + 文本） */
  notify: (level: NotifyLevel, message: string) => void
  /** 上报一段需要合入下轮模型上下文的自然语言 note */
  updateModelContext: (note: string) => void
  /** 请求 host 打开链接（host 根据 external 决定打开策略） */
  openLink: (url: string, external?: boolean) => void
  /** 销毁 client：移除 listener、reject 所有 pending、停掉 trailing timer */
  destroy: () => void
}

/** 内部 pending 调用记录 */
interface PendingCall {
  resolve: (value: unknown) => void
  reject: (reason: Error) => void
  timer: ReturnType<typeof setTimeout>
}

/**
 * 创建 widget client 实例。构造瞬间会立刻发送 `app.ready` postMessage。
 *
 * 在 iframe 内（`window.parent` 指向 host）才能正常工作；非 iframe 环境下
 * 仍然会构造，但所有出站消息会发给 `window.parent`（即 self），不会有 host 响应。
 */
export function createWidgetClient<TData = unknown>(
  opts: CreateWidgetClientOptions<TData> = {},
): WidgetClient {
  const win = opts.win ?? globalThis.window
  const parentWin = opts.parentWin ?? win.parent
  const timeoutMs = opts.timeoutMs ?? DEFAULT_TOOL_TIMEOUT_MS

  const pending = new Map<string, PendingCall>()
  let destroyed = false

  // resize 节流状态
  let lastResizeAt = 0
  let pendingResizeHeight: number | null = null
  let trailingTimer: ReturnType<typeof setTimeout> | null = null

  /** 出站：发 JSON-RPC 消息给 host（含 jsonrpc 字段） */
  function postToHost<T>(method: string, params?: T, id?: string): void {
    if (destroyed) return
    const msg: IframeToHostMessage<T> = id !== undefined
      ? { jsonrpc: '2.0', id, method, ...(params !== undefined ? { params } : {}) }
      : { jsonrpc: '2.0', method, ...(params !== undefined ? { params } : {}) }
    parentWin.postMessage(msg, '*')
  }

  /** 入站：处理 host → iframe 消息（已在 listener 内做过 source / jsonrpc 校验） */
  function handleHostMessage(msg: HostToIframeMessage): void {
    // host → iframe response（带 id 的 result / error）
    if (msg.id !== undefined && pending.has(msg.id)) {
      const call = pending.get(msg.id)!
      pending.delete(msg.id)
      clearTimeout(call.timer)
      if (msg.method === WIDGET_METHOD.AppToolError && msg.error) {
        call.reject(new Error(`${msg.error.code}: ${msg.error.message}`))
        return
      }
      // AppToolResult 或其他带 id 的成功响应
      call.resolve(msg.result)
      return
    }

    // host → iframe push（无 id 的事件，目前只有 app.data）
    if (msg.method === WIDGET_METHOD.AppData && opts.onData) {
      const params = msg.params as WidgetDataParams<TData> | undefined
      if (params) {
        opts.onData(params.data, params.context)
      }
    }
  }

  /** window message listener（含 source / jsonrpc 校验） */
  function onMessage(event: MessageEvent): void {
    if (destroyed) return
    if (event.source !== parentWin) return
    const data = event.data as unknown
    if (!isJsonRpcMessage(data)) return
    handleHostMessage(data as HostToIframeMessage)
  }

  win.addEventListener('message', onMessage)

  // 构造瞬间立即发送 app.ready
  postToHost(WIDGET_METHOD.AppReady)

  /** 内部：发 resize（更新 lastResizeAt，清掉 pending） */
  function flushResize(height: number): void {
    pendingResizeHeight = null
    lastResizeAt = nowMs()
    const params: ResizeParams = { height }
    postToHost(WIDGET_METHOD.AppResize, params)
  }

  function callServerTool<TResult = unknown>(name: string, args?: unknown): Promise<TResult> {
    if (destroyed) {
      return Promise.reject(new Error('widget client destroyed'))
    }
    return new Promise<TResult>((resolve, reject) => {
      const id = randomId()
      const timer = setTimeout(() => {
        if (pending.has(id)) {
          pending.delete(id)
          reject(new Error(`callServerTool timeout: ${name}`))
        }
      }, timeoutMs)
      pending.set(id, {
        resolve: (v) => resolve(v as TResult),
        reject,
        timer,
      })
      postToHost(WIDGET_METHOD.AppCallServerTool, { name, args }, id)
    })
  }

  function resize(height: number): void {
    if (destroyed) return
    const now = nowMs()
    const elapsed = now - lastResizeAt
    // 首次（lastResizeAt === 0）或距上次发出 ≥ 100ms：立即发
    if (lastResizeAt === 0 || elapsed >= RESIZE_THROTTLE_MS) {
      // 取消任何 pending trailing（避免 trailing 与 immediate 双发）
      if (trailingTimer !== null) {
        clearTimeout(trailingTimer)
        trailingTimer = null
        pendingResizeHeight = null
      }
      flushResize(height)
      return
    }
    // 100ms 窗口内：调度 trailing call（只保留最后一次 height）
    pendingResizeHeight = height
    if (trailingTimer !== null) {
      clearTimeout(trailingTimer)
    }
    const remaining = RESIZE_THROTTLE_MS - elapsed
    trailingTimer = setTimeout(() => {
      trailingTimer = null
      const h = pendingResizeHeight
      if (h !== null) {
        flushResize(h)
      }
    }, remaining)
  }

  function close(): void {
    postToHost(WIDGET_METHOD.AppClose)
  }

  function notify(level: NotifyLevel, message: string): void {
    const params: NotifyParams = { level, message }
    postToHost(WIDGET_METHOD.AppNotify, params)
  }

  function updateModelContext(note: string): void {
    const params: UpdateModelContextParams = { note }
    postToHost(WIDGET_METHOD.AppUpdateModelContext, params)
  }

  function openLink(url: string, external?: boolean): void {
    const params: OpenLinkParams = external !== undefined ? { url, external } : { url }
    postToHost(WIDGET_METHOD.AppOpenLink, params)
  }

  function destroy(): void {
    if (destroyed) return
    destroyed = true
    win.removeEventListener('message', onMessage)
    if (trailingTimer !== null) {
      clearTimeout(trailingTimer)
      trailingTimer = null
    }
    pendingResizeHeight = null
    // reject 所有 pending callServerTool
    for (const [, call] of pending) {
      clearTimeout(call.timer)
      call.reject(new Error('widget client destroyed'))
    }
    pending.clear()
  }

  return {
    callServerTool,
    resize,
    close,
    notify,
    updateModelContext,
    openLink,
    destroy,
  }
}

// ----- helpers -----

function nowMs(): number {
  // 用 Date.now（ms 精度对 100ms 节流足够；fake timers 拦截 Date 时也能正常工作）
  return Date.now()
}

function randomId(): string {
  const c = (globalThis as { crypto?: { randomUUID?: () => string } }).crypto
  if (c && typeof c.randomUUID === 'function') {
    return c.randomUUID()
  }
  // fallback：高熵随机串（仅在测试 / 老环境兜底；不依赖 crypto）
  return `r-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}

function isJsonRpcMessage(data: unknown): data is { jsonrpc: '2.0'; method?: string; id?: string } {
  if (typeof data !== 'object' || data === null) return false
  return (data as { jsonrpc?: unknown }).jsonrpc === '2.0'
}
