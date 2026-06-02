/**
 * @wuhanyuhan/squad-widget-sdk — client 单测。
 * 跑在 happy-dom（vitest config `environment: 'happy-dom'`）下，提供 window/MessageEvent。
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createWidgetClient } from '../client'
import { WIDGET_METHOD } from '../protocol'

/** 模拟一个独立的 host window（与 iframe 内的 window 区分），并设置 window.parent 指向它 */
function setupHostParent(): { hostPostMessage: ReturnType<typeof vi.fn>; restore: () => void } {
  const hostPostMessage = vi.fn()
  const fakeParent = { postMessage: hostPostMessage } as unknown as Window
  const original = Object.getOwnPropertyDescriptor(window, 'parent')
  Object.defineProperty(window, 'parent', {
    configurable: true,
    get: () => fakeParent,
  })
  return {
    hostPostMessage,
    restore: () => {
      if (original) {
        Object.defineProperty(window, 'parent', original)
      } else {
        delete (window as unknown as { parent?: unknown }).parent
      }
    },
  }
}

/** dispatch 一条 host → iframe message（带可控 source） */
function dispatchHostMessage(data: unknown, source: Window | null): void {
  const event = new MessageEvent('message', {
    data,
    source,
    origin: 'null',
  })
  window.dispatchEvent(event)
}

describe('createWidgetClient', () => {
  let host: ReturnType<typeof setupHostParent>

  beforeEach(() => {
    host = setupHostParent()
  })

  afterEach(() => {
    host.restore()
    vi.useRealTimers()
  })

  it('构造时立即发送 app.ready postMessage', () => {
    const client = createWidgetClient()
    expect(host.hostPostMessage).toHaveBeenCalledTimes(1)
    const [msg] = host.hostPostMessage.mock.calls[0]!
    expect(msg).toMatchObject({
      jsonrpc: '2.0',
      method: WIDGET_METHOD.AppReady,
    })
    expect((msg as { id?: string }).id).toBeUndefined()
    client.destroy()
  })

  it('callServerTool happy path：host 回 result → resolve', async () => {
    const client = createWidgetClient()
    host.hostPostMessage.mockClear()

    const promise = client.callServerTool<{ ok: true }>('refresh', { force: true })

    // 抓出站请求里的 id
    expect(host.hostPostMessage).toHaveBeenCalledTimes(1)
    const sent = host.hostPostMessage.mock.calls[0]![0] as {
      id: string
      method: string
      params: { name: string; args: unknown }
    }
    expect(sent.method).toBe(WIDGET_METHOD.AppCallServerTool)
    expect(sent.params).toEqual({ name: 'refresh', args: { force: true } })
    expect(typeof sent.id).toBe('string')

    // host 回 toolResult
    dispatchHostMessage(
      {
        jsonrpc: '2.0',
        id: sent.id,
        method: WIDGET_METHOD.AppToolResult,
        result: { ok: true },
      },
      window.parent,
    )

    await expect(promise).resolves.toEqual({ ok: true })
    client.destroy()
  })

  it('callServerTool 失败：host 回 error → reject 含 code: message 文案', async () => {
    const client = createWidgetClient()
    host.hostPostMessage.mockClear()

    const promise = client.callServerTool('refresh')
    const sent = host.hostPostMessage.mock.calls[0]![0] as { id: string }

    dispatchHostMessage(
      {
        jsonrpc: '2.0',
        id: sent.id,
        method: WIDGET_METHOD.AppToolError,
        error: { code: 'TOOL_NOT_FOUND', message: 'refresh not registered' },
      },
      window.parent,
    )

    await expect(promise).rejects.toThrow('TOOL_NOT_FOUND: refresh not registered')
    client.destroy()
  })

  it('callServerTool 30s 超时：advance 30001ms → reject "timeout"', async () => {
    vi.useFakeTimers()
    const client = createWidgetClient()
    host.hostPostMessage.mockClear()

    const promise = client.callServerTool('slow')
    // 先附上 catch handler 防止 unhandled rejection 在 fake-timer advance 触发的微任务里冒出
    const settled = promise.catch((e) => e)

    await vi.advanceTimersByTimeAsync(30_001)

    const err = (await settled) as Error
    expect(err).toBeInstanceOf(Error)
    expect(err.message).toMatch(/timeout/i)
    expect(err.message).toContain('slow')

    client.destroy()
  })

  it('resize 100ms 节流：5 次连发 → 2 次 postMessage（首发 + trailing）', () => {
    vi.useFakeTimers()
    // vi.useFakeTimers 同时 fake 了 Date 和 setTimeout，nowMs (Date.now) 与定时器一起前进
    const client = createWidgetClient()
    host.hostPostMessage.mockClear()

    // 5 次连发
    client.resize(100)
    client.resize(110)
    client.resize(120)
    client.resize(130)
    client.resize(140)

    // 此时应该只有第 1 次 immediate 发出
    const resizeCalls = host.hostPostMessage.mock.calls
      .map((c) => c[0])
      .filter((m): m is { method: string; params: { height: number } } => {
        return typeof m === 'object' && m !== null && (m as { method?: string }).method === WIDGET_METHOD.AppResize
      })
    expect(resizeCalls).toHaveLength(1)
    expect(resizeCalls[0]!.params.height).toBe(100)

    // advance 到 trailing 触发后
    vi.advanceTimersByTime(150)

    const finalCalls = host.hostPostMessage.mock.calls
      .map((c) => c[0])
      .filter((m): m is { method: string; params: { height: number } } => {
        return typeof m === 'object' && m !== null && (m as { method?: string }).method === WIDGET_METHOD.AppResize
      })
    expect(finalCalls).toHaveLength(2)
    expect(finalCalls[0]!.params.height).toBe(100)
    expect(finalCalls[1]!.params.height).toBe(140)

    // 100ms 窗口已过，下一次 resize 应该立即触发
    vi.advanceTimersByTime(200)
    client.resize(200)
    const afterCalls = host.hostPostMessage.mock.calls
      .map((c) => c[0])
      .filter((m): m is { method: string; params: { height: number } } => {
        return typeof m === 'object' && m !== null && (m as { method?: string }).method === WIDGET_METHOD.AppResize
      })
    expect(afterCalls).toHaveLength(3)
    expect(afterCalls[2]!.params.height).toBe(200)

    client.destroy()
  })

  it('resize 防御分支：trailing pending 时 Date 跳过 100ms → 进 immediate 清旧 trailing', () => {
    vi.useFakeTimers()
    const client = createWidgetClient()
    host.hostPostMessage.mockClear()

    // 抓 immediate / trailing 两类 resize 调用的 helper
    const collectResize = () =>
      host.hostPostMessage.mock.calls
        .map((c) => c[0])
        .filter((m): m is { method: string; params: { height: number } } => {
          return (
            typeof m === 'object' &&
            m !== null &&
            (m as { method?: string }).method === WIDGET_METHOD.AppResize
          )
        })

    const t0 = Date.now()

    // 1) 首次 resize → immediate fire，lastResizeAt = t0
    client.resize(100)
    expect(collectResize()).toHaveLength(1)
    expect(collectResize()[0]!.params.height).toBe(100)

    // 2) advance 50ms（节流窗口内），第二次 resize → trailing pending，
    //    setTimeout 调度到 t0+100 触发
    vi.advanceTimersByTime(50)
    client.resize(110)
    expect(collectResize()).toHaveLength(1) // 还没 fire trailing

    // 3) 模拟浏览器 background-tab throttle：Date 单独跳到 t0+250，
    //    但 setTimeout 队列没前进 → 旧 trailing 仍 pending（trailingTimer !== null）
    vi.setSystemTime(t0 + 250)

    // 4) 第三次 resize → elapsed = 250 ≥ 100ms → 走 immediate path：
    //    命中 client.ts:191-195 防御分支：clearTimeout + flushResize(120)
    client.resize(120)
    expect(collectResize()).toHaveLength(2)
    expect(collectResize()[1]!.params.height).toBe(120)

    // 5) 推进 setTimeout 队列足够久，旧 trailing 应已被 cancel，不应再 fire
    vi.advanceTimersByTime(500)
    expect(collectResize()).toHaveLength(2) // 仍是 2 次（首发 + 第二 immediate）

    client.destroy()
  })

  it('入站消息 source 不匹配 → 不处理（onData 不被调用）', () => {
    const onData = vi.fn()
    const client = createWidgetClient({ onData })
    host.hostPostMessage.mockClear()

    // 模拟一条来自 window（非 window.parent）的消息
    dispatchHostMessage(
      {
        jsonrpc: '2.0',
        method: WIDGET_METHOD.AppData,
        params: { data: { foo: 1 }, context: {} },
      },
      window,
    )

    expect(onData).not.toHaveBeenCalled()

    // 校验 dispatch 来自正确 source 时 onData 会被调用（正反对照）
    dispatchHostMessage(
      {
        jsonrpc: '2.0',
        method: WIDGET_METHOD.AppData,
        params: { data: { foo: 2 }, context: { sessionId: 's1' } },
      },
      window.parent,
    )
    expect(onData).toHaveBeenCalledTimes(1)
    expect(onData).toHaveBeenCalledWith({ foo: 2 }, { sessionId: 's1' })

    client.destroy()
  })

  it('destroy 后 reject 所有 pending + listener 移除', async () => {
    const onData = vi.fn()
    const client = createWidgetClient({ onData })
    host.hostPostMessage.mockClear()

    const pending = client.callServerTool('slow')
    const settled = pending.catch((e) => e)

    client.destroy()

    const err = (await settled) as Error
    expect(err).toBeInstanceOf(Error)
    expect(err.message).toMatch(/destroyed/i)

    // destroy 后再发 host 消息，onData 不应被触发
    dispatchHostMessage(
      {
        jsonrpc: '2.0',
        method: WIDGET_METHOD.AppData,
        params: { data: { foo: 3 }, context: {} },
      },
      window.parent,
    )
    expect(onData).not.toHaveBeenCalled()

    // destroy 后 callServerTool 也应直接 reject
    await expect(client.callServerTool('again')).rejects.toThrow(/destroyed/)
  })

  it('destroy 时取消未触发的 trailing resize timer（不再 postMessage）', () => {
    vi.useFakeTimers()
    const client = createWidgetClient()
    host.hostPostMessage.mockClear()

    client.resize(100) // immediate 发
    client.resize(110) // 进入 trailing pending
    expect(host.hostPostMessage).toHaveBeenCalledTimes(1)

    // destroy 应该清掉 trailing timer
    client.destroy()
    vi.advanceTimersByTime(500)

    // trailing 不应被触发
    expect(host.hostPostMessage).toHaveBeenCalledTimes(1)
  })

  it('入站非 JSON-RPC 消息直接 ignore（jsonrpc 字段缺失）', () => {
    const onData = vi.fn()
    const client = createWidgetClient({ onData })

    dispatchHostMessage({ method: WIDGET_METHOD.AppData, params: { data: 1, context: {} } }, window.parent)
    dispatchHostMessage('plain string', window.parent)
    dispatchHostMessage(null, window.parent)
    dispatchHostMessage({ jsonrpc: '1.0', method: WIDGET_METHOD.AppData }, window.parent)

    expect(onData).not.toHaveBeenCalled()
    client.destroy()
  })

  it('close / notify / updateModelContext / openLink 直发 postMessage', () => {
    const client = createWidgetClient()
    host.hostPostMessage.mockClear()

    client.close()
    client.notify('warning', '存档失败')
    client.updateModelContext('用户在 widget 里把 draft 42 改名为 "5月营销月报 v2"')
    client.openLink('https://example.com', true)

    const sent = host.hostPostMessage.mock.calls.map((c) => c[0])
    expect(sent).toHaveLength(4)
    expect(sent[0]).toMatchObject({ jsonrpc: '2.0', method: WIDGET_METHOD.AppClose })
    expect(sent[1]).toMatchObject({
      jsonrpc: '2.0',
      method: WIDGET_METHOD.AppNotify,
      params: { level: 'warning', message: '存档失败' },
    })
    expect(sent[2]).toMatchObject({
      jsonrpc: '2.0',
      method: WIDGET_METHOD.AppUpdateModelContext,
      params: { note: '用户在 widget 里把 draft 42 改名为 "5月营销月报 v2"' },
    })
    expect(sent[3]).toMatchObject({
      jsonrpc: '2.0',
      method: WIDGET_METHOD.AppOpenLink,
      params: { url: 'https://example.com', external: true },
    })
    client.destroy()
  })
})
