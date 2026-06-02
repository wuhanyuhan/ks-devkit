# @wuhanyuhan/squad-widget-sdk

Squad 自定义 widget 的 iframe 客户端 SDK，实现 keystone `widgets-protocol-v1`（postMessage / JSON-RPC 2.0）。在 squad widget 的 iframe 内引入本 SDK 后，即可与 keystone host 双向通讯：接收 host 推送的 `app.data`、反向调用 server-side tool、上报 resize / notify / model-context note 等。

体积承诺：minified gzip ≤ 5KB，无运行时依赖。

## 安装

```bash
npm install @wuhanyuhan/squad-widget-sdk
# 或
bun add @wuhanyuhan/squad-widget-sdk
```

## Quickstart

```typescript
import { createWidgetClient } from '@wuhanyuhan/squad-widget-sdk'

interface MyData {
  items: Array<{ id: string; title: string }>
}

const client = createWidgetClient<MyData>({
  onData: (data, context) => {
    console.log('received data:', data, 'context:', context)
    renderUI(data)
  },
  timeoutMs: 30_000, // 可选，默认 30s
})

// 用户点击按钮 → 反向调用 server-side tool
document.getElementById('refresh')!.addEventListener('click', async () => {
  try {
    const result = await client.callServerTool<{ ok: boolean }>('refresh', { force: true })
    console.log('tool result:', result)
  } catch (err) {
    console.error('tool failed:', err)
  }
})

// iframe 高度变化时通知 host（自动节流到 100ms）
const ro = new ResizeObserver(([entry]) => {
  if (entry) client.resize(entry.contentRect.height)
})
ro.observe(document.body)

// 用户主动关闭 widget
document.getElementById('close')!.addEventListener('click', () => {
  client.close()
})
```

构造瞬间 SDK 会立刻发送 `app.ready` 给 host；host 收到后会推 `app.data`（首帧 + 后续更新）触发你的 `onData` 回调。

## API

### `createWidgetClient<TData>(opts?): WidgetClient`

**Options**：

| 字段 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `onData` | `(data: TData, ctx: WidgetContext) => void` | — | host 推送 `app.data` 时回调（首帧 + 更新） |
| `timeoutMs` | `number` | `30000` | `callServerTool` 超时（ms），默认 30s |

**返回的 `WidgetClient` 句柄**：

| 方法 | 用途 |
|------|------|
| `callServerTool<TResult>(name, args?)` | 反向调用 server-side tool，30s 超时；host 回 `app.toolResult` 时 resolve，回 `app.toolError` 时 reject（错误文案 `code: message`） |
| `resize(height)` | 通知 host iframe 高度变化；100ms 节流（连续 N 次 → 首发 + 末尾 trailing 两次 postMessage） |
| `close()` | 请求 host 关闭当前 widget |
| `notify(level, message)` | 通知 host 弹出全局通知；level 取值 `info / success / warning / error` |
| `updateModelContext(note)` | 上报一段自然语言 note，host 合入下轮模型上下文 |
| `openLink(url, external?)` | 请求 host 打开链接，`external=true` 时在新标签页 |
| `destroy()` | 销毁 client：移除 listener、reject 所有 pending、停掉 trailing timer |

### `WidgetContext`

host 注入的 widget 运行时上下文（首帧 / 数据更新随 `app.data` 一起带下来）：

| 字段 | 类型 | 说明 |
|------|------|------|
| `sessionId` | `string?` | 当前 chat session 的 ID |
| `requestId` | `string?` | 触发本 widget 的 tool call 请求 ID |
| `serverId` | `string?` | MCP server ID |
| `toolName` | `string?` | 当前调用的 tool 名 |
| `toolCallId` | `string?` | parent tool call ID（用于 follow-up 关联） |
| `messageId` | `string?` | widget 所属 message id |
| `bindingFingerprint` | `string?` | host 端 ToolUIBinding 标识 |
| `locale` | `string?` | 用户偏好语言（如 `zh-CN`） |
| `darkMode` | `boolean?` | 是否暗色主题 |

`WidgetContext` 允许扩展额外字段（host 后续追加新键不破坏旧 SDK）。

> **wire 字段命名约定（强制）**：host 通过 postMessage 下发 context 字段时 **必须使用 camelCase**
> （即上表第一列写的形式），与 SDK 暴露给 squad 开发者的字段一一对应。host 代码示例
> 里的 `sessionID / requestID / serverID / toolCallID` 是 Go runtime 内部命名，落到 wire / SDK
> 层一律 camelCase。

## 协议参考

- widgets postMessage contract
- ks-types `widget_postmessage.go` → `dist/widgets.d.ts`：10 个 `PMMethodApp*` 常量是本 SDK `WIDGET_METHOD` 的真实来源

## 不变量

- 入站消息严格 `event.source === window.parent` 校验，不匹配直接 ignore
- 入站消息必须 `data.jsonrpc === '2.0'` 才进入分发
- `callServerTool` 超过 `timeoutMs` reject `Error('callServerTool timeout: <name>')`
- `destroy()` 后所有 pending reject `Error('widget client destroyed')`，listener 移除
