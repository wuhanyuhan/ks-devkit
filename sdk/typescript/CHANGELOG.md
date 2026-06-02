# Changelog

## [0.5.0] - 2026-05-31

### Added

- `app.embedding` / `EmbeddingClient`：通过 Keystone relay 调 `/v1/mcp/relay/embeddings`。
- `app.vectorStore(collection)` / `VectorStoreClient`：封装 `upsert` / `search` / `searchText` / `delete` / `deleteByFilter` / `count`。
- **Capability Mesh**：`registerCapability`（裸名 + canonical 派生）/ `callCapability`（全名，引用他人能力维持全名，故不对称）/ `DispatcherClient` / `CapabilityCall` / `Task` / `EventsClient`（polling + WS）/ `ScopedJWTVerifier`（`kx_` claims）/ 错误类层级 + `mapHttpError`。
- mcp_tool 复用四象限；`input_schema` 透传到生成的 tool；`CapabilityContext`（`AbortSignal` 适配 ctx）。
- 三语言 wire-compat conformance（与 Go/Python 字节级一致）。

### Changed / BREAKING

- **orphan → error**：mcp_tool capability 声明但既无 handler 也无同名 tool → 启动期报错（替代旧 warn）。
- capability 命名去前缀（裸名 + 派生 canonical）；`CapabilityContext` caller 身份用 `callerId`。

## [0.4.0] - 2026-05-18

### Added

- `SelfClient` class + `fetchKeystoneManagedEnv` 函数（`@wuhanyuhan/ks-app` 主入口
  export）：应用启动期通过 `KS_APP_TOKEN` 调 keystone `GET /v1/apps/self/resources`
  自取本安装实例被分配的托管资源凭证（`DB_HOST` / `DB_PASSWORD` / `CACHE_URL` /
  `KS_APP_FILES_DIR` 等）。
- App 类新增 5 个 declare API（镜像 `ks-types` v0.5.0 `MetaResponse`）：
  `declareNav` / `declarePermission` / `setConfigMode` / `setProtocolVersion` /
  `setConfigStatus`。`/meta` 路由按 omitempty 装配新字段。
- `ks-types` v0.6.0 mirror：4 个新类型。
- LLM relay 客户端：chat 非流式 + `streamChat` 流式（基于 SSE，TextDecoder 尾部
  flush + reader 释放保护）。
- 配置公钥信任类型、应用包签名摘要必填。

### Fixed

- `streamChat` TextDecoder 尾部 flush + reader 释放保护。
- `happy-dom` 15 → 20.9 修复 CVE-2025-61927 + CVE-2026-34226。

### Rationale

托管资源自查机制的 TS 端实现。生产容器化路径不受影响——当应用由 keystone
自身的 lifecycle 启动、未注入 `KS_APP_TOKEN` 时 SDK 自动跳过，继续走
`runtime.env` 挂载路径。

### 关于版本号跳跃

npm registry 上 0.1.0 之后未发布过 0.2/0.3——本地 source 已包含但未发版。
0.4.0 一次性合并所有 0.2/0.3 + self-fetch 改动。未来从 0.4.0 起按
SemVer 严格记录每版。

## [0.2.0] - 2026-04-18

### Added

- 镜像 `ks-types` v0.5.0 `MetaResponse` 新增 5 个声明式字段：
  - `MetaNavDecl`：MCP 在 keystone 后台左侧菜单的导航声明（label / icon / category / order / open_mode / entry_path / required_perms）
  - `MetaPermissionDecl`：权限码目录条目（code / label / default_roles）
  - `MetaConfigMode`：配置模式分类枚举（`schema` / `iframe` / `none`）
  - `MetaResponse.protocol_version`：MCP 协议版本（SemVer 'MAJOR.MINOR'）
  - `MetaResponse.config_status`：配置状态枚举（`unconfigured` / `via_frontend` / `via_cli` / `mixed`）
- App 类对齐 Python SDK v0.4.0 的 declare 风格 API：`declareNav` / `declarePermission` / `setConfigMode` / `setProtocolVersion` / `setConfigStatus`
- `setConfigMode` / `setConfigStatus` 接收非法值时抛 Error
- `/meta` 路由按 omitempty 装配新字段：未声明时全部不出现在响应中（与 Python SDK + Go 端 wire 行为一致）

### Changed

- `MetaResponse` interface 注释明确：`config_mode` 与 `config_ui` 在 v0.x.0 共存

### Migration

新字段全部为 additive optional，不破坏 v0.1.0 调用方。已使用 `MetaResponse` interface 的代码无需改动。

## [0.1.0] - 2026-04-17

### Added

- 初始版本。`createApp` factory + App class。
- MCP 工具注册（`app.tool`）基于 `@modelcontextprotocol/sdk` 包装。
- Auth middleware（keystone_jwks）对齐 conformance auth 套件。
- `/healthz` / `/readyz` / `/meta` 自动路由（对齐 conformance 套件）。
- 手写 TS 镜像（`@wuhanyuhan/ks-app/types`）：AuthMode、MetaResponse、ToolInfo、ConfigUIInfo。
- Dual runtime 支持：Bun ≥ 1.2（一等公民）+ Node ≥ 20（@hono/node-server）。
- LLM Relay 极简 client。
- Lifecycle hooks：onStartup / onShutdown。
- Escape hatches：`app.mcpServer` / `app.fetch`。

### Conformance

Claims compliance with: **conformance-v1.0.0**（22/22）
