/**
 * ks-types-ts: 手写 TS 镜像最小 schema 集合，对齐 github.com/wuhanyuhan/ks-types v0.5.0
 *
 * 漂移守护：ks-types bump 时手动 diff 本文件；conformance 套件兜 wire-level 漂移
 *
 * 字段命名约定：所有 wire 字段使用 snake_case，与 Go ks-types JSON 标签 + Python SDK
 * + JSON wire format 保持一致。TS interface 字段名直接采用 snake_case（不做 camelCase 桥接）。
 */

export const AuthMode = {
  None: "none",
  KeystoneJWKS: "keystone_jwks",
  StaticBearer: "static_bearer",
} as const;

export type AuthMode = typeof AuthMode[keyof typeof AuthMode];

export function isValidAuthMode(v: string): v is AuthMode {
  return v === "none" || v === "keystone_jwks" || v === "static_bearer";
}

/** 空字符串归一为 none，其余返回自身 */
export function defaultAuthMode(v: string | undefined | null): AuthMode {
  if (!v) return AuthMode.None;
  if (isValidAuthMode(v)) return v;
  throw new Error(`invalid auth_mode: ${v}`);
}

export interface ToolInfo {
  name: string;
  description?: string;
}

export interface ConfigUIInfo {
  enabled: boolean;
  url?: string;
}

/**
 * MCP 在 keystone 后台左侧菜单的导航声明（v0.5.0 新增）。
 *
 * - label：菜单显示名（<= 12 字符，中文）
 * - icon：lucide-react 图标名
 * - category：'应用' / '工具' / '配置' / '集成'
 * - order：排序权重，默认 99
 * - open_mode：'dialog' / 'fullpage'
 * - entry_path：入口路径，默认 '/'
 * - required_perms：进入页面所需权限码（AND 语义；空数组 = admin 直通）
 */
export interface MetaNavDecl {
  label: string;
  icon?: string;
  category: "应用" | "工具" | "配置" | "集成";
  order?: number;
  open_mode: "dialog" | "fullpage";
  entry_path?: string;
  required_perms?: string[];
}

/**
 * MCP 权限码目录条目（v0.5.0 新增）。
 *
 * - code：权限码，格式 `mcp.{mcp_id}.{action}`
 * - label：中文显示名
 * - default_roles：默认角色列表（MVP 只 ['admin']）
 */
export interface MetaPermissionDecl {
  code: string;
  label: string;
  default_roles?: string[];
}

/** 配置模式分类（v0.5.0 新增）。 */
export type MetaConfigMode = "schema" | "iframe" | "none";

/** 配置状态（v0.5.0 新增，Spec A §6.4 CLI 离线通道）。 */
export type MetaConfigStatus =
  | "unconfigured"
  | "via_frontend"
  | "via_cli"
  | "mixed";

export interface MetaResponse {
  name: string;
  version: string;
  auth_mode?: AuthMode;
  config_ui?: ConfigUIInfo | null;
  tools?: ToolInfo[];
  // v0.5.0 新增 5 字段（对齐 ks-types v0.5.0 MetaResponse + Python SDK v0.4.0 declare API）
  nav?: MetaNavDecl;
  permissions?: MetaPermissionDecl[];
  // config_mode 与 config_ui 的语义关系（v0.x.0 共存约定）：
  //   config_mode 是"配置模式分类"（schema / iframe / none / undefined）
  //   config_ui   是"iframe 模式的接入信息"（URL 等）
  // 共存规则同 ks-types Go 端 meta.go 的 ConfigMode 注释，详见 keystone Spec B Q2 决策。
  config_mode?: MetaConfigMode;
  protocol_version?: string;
  config_status?: MetaConfigStatus;
}

// Spec A v0.6.0 mirrors（对应 ks-types config_schema.go）

export interface ConfigSchemaResponse {
  schema: Record<string, unknown>;
  ui_schema: Record<string, unknown>;
  version: string;
}

export interface ConfigPubkeyResponse {
  pubkey: string;       // base64-std 32 bytes
  fingerprint: string;  // spec-v1 §4.2 格式
  algorithm: string;    // "x25519-ecdh-aes256gcm-v1"
  created_at: string;
  trust?: ConfigPubkeyTrust;
}

export interface ConfigPubkeyTrust {
  status: "system_verified" | "tofu_known" | "user_confirmation_required" | "changed" | "blocked" | string;
  reason?: string;
  app_id?: string;
  app_version?: string;
  source?: string;
  signer_kid?: string;
  package_sha256?: string;
  manifest_sha256?: string;
  verified_at?: string;
  requires_user_confirmation: boolean;
}

export interface AppPackageSignature {
  signature_b64: string;
  kid: string;
  public_key_url: string;
  domain: string;
  signed_payload_b64: string;
  package_sha256: string;
  manifest_sha256: string;
  signed_at: string;
  verification_state?: string;
}

export interface EncryptedConfigPayload {
  algorithm: string;
  ephemeral_pubkey: string;
  nonce: string;
  aad_fields: Record<string, unknown>;
  aad_canonical: string;
  ciphertext: string;
  idempotency_key: string;
}

export interface ConfigApplyResult {
  applied_at: string;
  version: number; // TS 用 number；Spec A v1 约定 version < 2^53
}

// AAD + Fingerprint helper 镜像在前端处（crypto-e2e.ts / fingerprint.ts）；
// TS SDK 层面仅提供类型定义，不重复实现（避免双源）。

// ── Capability Mesh wire 镜像（对齐 ks-types v0.29.0 去前缀语义；snake_case，遵本文件约定）──

/** capability 后端路由声明，镜像 ks-types v0.29.0 BackendSpec。 */
export interface BackendSpec {
  kind: string; // mcp_tool | http_endpoint
  tool_name?: string;
  path?: string;
  method?: string;
}

/**
 * provides.capabilities[i]，镜像 ks-types v0.29.0 CapabilitySpec 必读字段。
 * 去前缀：作者写裸名 name；canonical_name 由 SDK 派生 <app_id>.<name>（见 canonical.ts）。
 */
export interface CapabilitySpec {
  name: string;
  canonical_name?: string;
  execution_mode?: string;
  backend: BackendSpec;
  timeout_ms?: number;
  input_schema?: Record<string, unknown>;
  concurrency_limit?: number;
  resumable?: boolean;
}

/** requires.capabilities[i]：引用他人能力，写全名（不对称，不派生）。 */
export interface RequiresCapability {
  canonical_name: string;
}
