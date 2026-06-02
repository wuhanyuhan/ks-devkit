import type { Hono } from "hono";
import {
  AuthMode,
  type ToolInfo,
  type ConfigUIInfo,
  type MetaResponse,
  type MetaNavDecl,
  type MetaPermissionDecl,
  type MetaConfigMode,
  type MetaConfigStatus,
} from "../types";

export interface MetaRouteConfig {
  name: string;
  version: string;
  effectiveAuthMode: AuthMode;
  getToolInfos: () => ToolInfo[];
  configUI?: ConfigUIInfo | null;
  // v0.2.0 新增（对齐 ks-types v0.5.0 MetaResponse 5 字段）
  getNav?: () => MetaNavDecl | undefined;
  getPermissions?: () => MetaPermissionDecl[];
  getConfigMode?: () => MetaConfigMode | undefined;
  getProtocolVersion?: () => string | undefined;
  getConfigStatus?: () => MetaConfigStatus | undefined;
}

/**
 * 注册 /meta 路由（conformance §4 MetaResponse schema）。
 *
 * 字段装配规则：
 *   - name / version：必填
 *   - auth_mode：mode=none 时省略（空值等价于 none，conformance §4 允许）
 *   - tools：空时省略
 *   - config_ui：null/undefined 时省略（不得是 {} ）
 *
 * v0.2.0 新增 5 字段（对齐 ks-types v0.5.0 + Python SDK v0.4.0 declare 风格）：
 *   - nav / permissions / config_mode / protocol_version / config_status：未声明时全部 omitempty
 *   - permissions 空数组也省略（与 Python omitempty 语义对齐）
 */
export function registerMetaRoute(app: Hono, cfg: MetaRouteConfig): void {
  app.get("/meta", (c) => {
    const body: MetaResponse = {
      name: cfg.name,
      version: cfg.version,
    };
    if (cfg.effectiveAuthMode !== AuthMode.None) {
      body.auth_mode = cfg.effectiveAuthMode;
    }
    const tools = cfg.getToolInfos();
    if (tools.length > 0) {
      body.tools = tools;
    }
    if (cfg.configUI) {
      body.config_ui = cfg.configUI;
    }
    const nav = cfg.getNav?.();
    if (nav !== undefined) {
      body.nav = nav;
    }
    const permissions = cfg.getPermissions?.();
    if (permissions && permissions.length > 0) {
      body.permissions = permissions;
    }
    const configMode = cfg.getConfigMode?.();
    if (configMode !== undefined) {
      body.config_mode = configMode;
    }
    const protocolVersion = cfg.getProtocolVersion?.();
    if (protocolVersion !== undefined) {
      body.protocol_version = protocolVersion;
    }
    const configStatus = cfg.getConfigStatus?.();
    if (configStatus !== undefined) {
      body.config_status = configStatus;
    }
    return c.json(body);
  });
}
