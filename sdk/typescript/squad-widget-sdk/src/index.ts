/**
 * @wuhanyuhan/squad-widget-sdk — public entry。
 *
 * Squad 开发者只需 `import { createWidgetClient } from '@wuhanyuhan/squad-widget-sdk'`
 * 即可在自定义 widget iframe 内与 keystone host 通过 widgets-protocol-v1 通讯。
 */

export { createWidgetClient } from './client'
export type { CreateWidgetClientOptions, WidgetClient } from './client'

export { WIDGET_METHOD } from './protocol'
export type {
  HostToIframeMessage,
  IframeToHostMessage,
  NotifyLevel,
  NotifyParams,
  OpenLinkParams,
  ResizeParams,
  UpdateModelContextParams,
  WidgetContext,
  WidgetDataParams,
  WidgetError,
  WidgetMethod,
} from './protocol'
