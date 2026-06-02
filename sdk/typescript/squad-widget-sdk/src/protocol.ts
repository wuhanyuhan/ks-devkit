/**
 * widgets-protocol-v1 — JSON-RPC 2.0 wire types & method 名常量。
 *
 * 与 ks-types `widget_postmessage.go` 派生的 `dist/widgets.d.ts` 严格对齐：
 * 10 个 PMMethodApp* 常量字面量原样镜像。
 *
 * 仅自定义 widget（ui:// scheme）路径用；共享 widget 走同源 React，不走 postmessage。
 */

export const WIDGET_METHOD = {
  // iframe → host
  AppReady: 'app.ready',
  AppResize: 'app.resize',
  AppCallServerTool: 'app.callServerTool',
  AppUpdateModelContext: 'app.updateModelContext',
  AppClose: 'app.close',
  AppNotify: 'app.notify',
  AppOpenLink: 'app.openLink',
  // host → iframe
  AppData: 'app.data',
  AppToolResult: 'app.toolResult',
  AppToolError: 'app.toolError',
} as const

export type WidgetMethod = (typeof WIDGET_METHOD)[keyof typeof WIDGET_METHOD]

/** iframe → host JSON-RPC 消息（params 由调用方决定结构） */
export interface IframeToHostMessage<T = unknown> {
  jsonrpc: '2.0'
  id?: string
  method: string
  params?: T
}

/** host → iframe JSON-RPC 消息（result/error 由 host 路由决定） */
export interface HostToIframeMessage<T = unknown> {
  jsonrpc: '2.0'
  id?: string
  method: string
  params?: T
  result?: unknown
  error?: WidgetError
}

/** JSON-RPC 错误对象（与 host 端 sendToolError 对齐） */
export interface WidgetError {
  code: string
  message: string
}

/**
 * host 在首帧 / 数据更新时下发的 app.data payload 结构。
 * data 是 squad 自定义 schema（与 ToolResult.data 一致），context 由 keystone host 注入。
 */
export interface WidgetDataParams<TData = unknown> {
  data: TData
  context: WidgetContext
}

/**
 * host 注入的 widget 运行时上下文。
 * 字段与 keystone widget_postmessage 设计稿对齐，未来可能扩展但只追加不删除。
 *
 * **wire 字段命名约定（强制）**：host 通过 postMessage 下发字段时 MUST 使用 camelCase
 * （即 `sessionId / requestId / serverId / toolCallId / toolName`），与本 SDK 暴露给 squad
 * 开发者的字段一一对应。host 代码示例里的 `sessionID / requestID / serverID /
 * toolCallID` 是 Go runtime 内部命名，落到 wire / SDK 层一律 camelCase。host 集成
 * 时按这里的契约对齐；有疑义以本文件为准。
 */
export interface WidgetContext {
  /** 当前 chat session 的 ID（host 必须用 camelCase 发送） */
  sessionId?: string
  /** 触发本 widget 的 tool call 请求 ID（host 必须用 camelCase 发送） */
  requestId?: string
  /** MCP server ID（host 必须用 camelCase 发送） */
  serverId?: string
  /** 当前调用的 tool 名（host 必须用 camelCase 发送） */
  toolName?: string
  /** parent tool call ID（用于 follow-up 关联，host 必须用 camelCase 发送） */
  toolCallId?: string
  /** widget 当前所属的 message id（host 端 message id） */
  messageId?: string
  /** widget binding 的 fingerprint（host 端 ToolUIBinding 标识） */
  bindingFingerprint?: string
  /** 用户偏好语言（如 zh-CN / en-US） */
  locale?: string
  /** 是否暗色主题 */
  darkMode?: boolean
  /** 兼容扩展槽：host 后续追加新字段不破坏旧 SDK */
  [key: string]: unknown
}

/** notify 通知级别 */
export type NotifyLevel = 'info' | 'success' | 'warning' | 'error'

/** app.notify params */
export interface NotifyParams {
  level: NotifyLevel
  message: string
}

/** app.openLink params */
export interface OpenLinkParams {
  url: string
  /** 是否在新标签页打开（host 决定执行策略） */
  external?: boolean
}

/** app.resize params */
export interface ResizeParams {
  height: number
}

/**
 * app.updateModelContext params（squad 上报给 host，host 再合入下轮模型上下文）。
 * 草稿形态：自然语言 note，不是结构化 patch。
 */
export interface UpdateModelContextParams {
  note: string
}
