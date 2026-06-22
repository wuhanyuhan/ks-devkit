# ksapp SDK Changelog

## [sdk/go/v0.18.0] - 2026-06-22

### Added

- `ConfigSpec` 新增 `OnTest` 与 `OnSaveValidate` 回调：`/ks-config/validate` 在本地 `OnValidate` 通过后才运行显式测试，`/ks-config/save` 优先使用 `OnSaveValidate` 做保存前校验并且不会触发 `OnTest`。未设置新回调时保持旧版 `OnValidate` 兼容语义。

## [sdk/go/v0.17.0] - 2026-06-18

### Added

- `/ks-readiness` + `/ks-readiness/init` 端点脚手架：`App.RegisterInitTask(gateID, handler)` 注册 init_task 门初始化逻辑（progress 上报、幂等重触发），SDK 管理 pending/running/ready/failed 内存态。消费 ks-types v0.43.0 wire 类型；端点无鉴权（同 /healthz、/meta 通路）。
- `go.mod`：`github.com/wuhanyuhan/ks-types` 升至 v0.43.0。

## [sdk/go/v0.15.0] - 2026-06-02

### Added

- mcpproto / ksapp 新增 `ks_conversation_id` caller-context 字段 + `ConversationID(ctx)` helper：keystone 调工具时经 `_meta` 透传 keystone 会话 ID，被调服务据此把决策门 / 交付物回流到正确会话。与 `ks_user_id` 等同款。

## [sdk/go/v0.13.0] - 2026-05-31

> capability mesh 去前缀 + mcp_tool 复用 + 形态对齐 + Go↔Python wire-compat。含 BREAKING，迁移见下。

### Changed

- **BREAKING**：`App.RegisterCapability` 改收**裸名** `name`（去前缀），全局 `CanonicalName` 由 SDK 内部 `kstypes.Canonical(appID, name)` 派生 `<app_id>.<name>`；manifest `provides.capabilities[].name` 同步写裸名。caller 侧 `App.CallCapability(<full_name>)` 与 `requires.capabilities[].canonical_name` **维持全名**（引用他人能力无法本地派生，故不对称）。
  - 迁移：把 `RegisterCapability("acme-app.foo", ...)` 改 `RegisterCapability("foo", ...)`，manifest 的 `canonical_name: acme-app.foo` 改 `name: foo`；caller 侧调用不变。
- **BREAKING**：`CapabilityContext.AppID()` 正名 `CallerID()`（承载 wire `ks_caller_id` 语义）。迁移：handler 里 `ctx.AppID()` 改 `ctx.CallerID()`。
- **BREAKING**：`mcp_tool` capability 在 manifest 声明、但既无 `RegisterCapability` handler 也无同名 `App.Tool` 时，finalize 改为返回错误（无承载），替代旧的静默跳过。

### Added

- `mcp_tool` 复用四象限：`backend.tool_name` 命中已有 `App.Tool` 且无独立 handler 时**复用**该 tool（join，不再撞名 hard fail）；有 handler 且撞名报真冲突；有 handler 无撞名生成新 tool。
- mcp_tool capability 路径注入 `CapabilityContext.ChainHeader`：从 `_meta.ks_chain_snapshot` 读调用链快照（`mcpproto.ChainSnapshot` / `ksapp.ChainSnapshot` helper）。⚠️ 完整生效需 keystone mcptool executor 配套透传 `ks_chain_snapshot`；未透传时为空但不回归。
- bump `ks-types` v0.21.0 → v0.29.0（去前缀契约 + `Canonical` helper；`input_schema` 透传到生成的 MCP tool）。

## [sdk/go/v0.12.0] - 2026-05-31

### Added

- `ChatRequest` 新增 LLM intent 一等字段 `Tier` / `Reasoning` / `RequireCapabilities`；自定义 `MarshalJSON` 降解进 `request_options` 约定 key（`tier` / `reasoning_mode` / `<cap>_required`），覆盖 `Chat` 与 `StreamChat`，对齐 Python `to_dict`；relay 顶层 schema 零改、不传 intent 时行为不变。
- `VisionChat` / `TextPart` / `ImagePart`：补齐多模态内容基元与便利方法（自动声明 `vision` 能力路由），对齐 Python vision 链路。
- `LLMCapabilityUnavailableError` + `ErrLLMCapabilityUnavailable` 哨兵：网关 422 `capability_unavailable` 映射为可判别错误（支持 `errors.As` / `errors.Is`），携带 `Missing` 列表；与 capability-mesh 的 `CapabilityUnavailableErr`（dispatch 域）区分。
- 跨语言 wire fixture `shared-fixtures/relay_chat_intent.json` + conformance，锁定 Go/Python intent `request_options` 字节等价。

### Changed

- `KS_RELAY_TOKEN` 为空时回落 `KEYSTONE_RELAY_TOKEN`（keystone 平台安装时注入名），LLM 与 embedding 客户端一致，对齐 Python。

## [sdk/go/v0.11.2] - 2026-05-26

### Changed

- **BREAKING**：向量检索收敛为 hybrid 唯一路径（dense+sparse RRF），三端一致 clean-break；`ksapp` SDK 统一通过服务端 hybrid 检索。

## [sdk/go/v0.11.1] - 2026-05-20

### Fixed

- 透传 Capability Mesh 调用链上下文（caller 身份多跳穿透）。
- 校验 config UI token server 绑定。

### Added

- embedding / vector store 客户端的跨语言 fixture（与 Python/TS 对齐）。

## [sdk/go/v0.11.0] - 2026-05-20

### Fixed

- `go.mod` require `ks-devkit/conformance` sub-module 替代 root `ks-devkit`，根因消除模块依赖问题。

## [sdk/go/v0.10.0] - 2026-05-20

### Added

- `CapabilityCall.WithOnBehalfOfUser(userID int64)` 链式方法：透传调用链发起人 user_id 到 dispatcher 的 `on_behalf_of_user_id` payload 字段（仅 `>0` 时发送），用于多跳 capability mesh「代表用户」调用。

## [sdk/go/v0.9.0] - 2026-05-19

### Added

- `App.Embedding()` / `EmbeddingClient`：通过 Keystone relay 调 `/v1/mcp/relay/embeddings`，读取 `KS_GATEWAY_URL` / `KS_RELAY_TOKEN` / `KS_EMBEDDING_MODEL` / `KS_EMBEDDING_DIM`。
- `App.VectorStore(collection)` / `VectorStoreClient`：封装 `upsert` / `search` / `search_text` / `delete` / `count`。

## [sdk/go/v0.8.0] - 2026-05-19

### Added

- **Capability Mesh 支持**：应用作者用 `app.RegisterCapability(canonical_name, handler)` 注册 capability handler；SDK 启动期按 manifest `provides.capabilities[]` 自动接入两条 backend 路径：
  - `backend.kind=mcp_tool`：capability 自动注册成同名 MCP tool（`tool_name` 由 manifest 指定），handler 从 `_meta.ks_*` 还原 `CapabilityContext`
  - `backend.kind=http_endpoint`：capability 注册成 net/http route，挂 `ScopedJWTMiddleware` 校验 `aud == canonical_name`
- **Caller-side**：`app.CallCapability(name).Invoke(ctx, args)` / `.Submit(ctx, args)` 寻址调用其他应用 capability；`.Submit` 返 `*Task`，支持 `task.Result(ctx, pollInterval)` / `task.Events(ctx)` / `task.Cancel(ctx)` / `task.Refresh(ctx)`。
- **CapabilityContext interface**：`UserID()` / `AppID()` / `CallerKind()` / `ChainID()` / `TaskID()` / `RequestID()` / `CanonicalName()` / `Progress(ctx, stage, percent)` / `Deadline()` / `Cancelled()`。
- **DispatcherClient**：`ksapp.NewDispatcherClient(gateway, token)` 提供 `Invoke` / `ReportProgress` / `GetTask` / `CancelTask`；HTTP 错误映射到 ksapp 错误层级；`GetTask` / `CancelTask` 在 caller 端把 404 重映射为 `TaskNotFound`（避免在 mapper 内嗅探 message 字段）。
- **EventsClient**：inbound WS 连 `/v1/apps/self/events`（lazy，首次 `CallCapability` 时启动）+ polling fallback（`KS_EVENTS_MODE=polling`）；WS 模式指数退避重连；`task_id → chan map[string]any` 路由，未注册的事件静默丢弃。
- **错误层级**：25 个 sentinel（`ErrKeystoneError` 为根，6 大类：Auth / Permission / NotFound / Validation / Dependency / Execution / RateLimit）+ 携带上下文的具体错误结构体（`*CapabilityNotFoundErr` / `*TokenAudienceMismatchErr` / `*ManifestMismatchErr` / `*CapabilityUnavailableErr` / `*RateLimitErr` / `*TimeoutErr` / `*TaskNotFoundErr`）。判定大类用 `errors.Is`；取上下文（canonical_name / retry_after_ms / ...）用 `errors.As`。
- **ScopedJWTVerifier + ScopedJWTMiddleware**：RS256 scoped JWT 验签 + aud 校验（保留在 ksapp 主包，未拆 auth 子包，避免循环 import）；`SetScopedJWTTestKey(kid, pem)` 注入静态公钥跳过 JWKS 拉取。
- **mcpproto context 字段补齐**：`WithMeta` 透传补 6 个键（`ks_agent_id` / `ks_user_id` / `ks_request_id` + capability mesh 的 `ks_caller_id` / `ks_caller_kind` / `ks_chain_id`），与 Python `ks_app/context.py` 对齐；ksapp 包同步导出 `AgentID` / `UserID` / `RequestID` / `CallerID` / `CallerKind` / `ChainID` helper。
- **示例**：`examples/capability-writer-demo/` — mcp_tool + http_endpoint 双路径完整示例（对齐 Python `sdk/python/examples/capability_writer_demo/`）。

### Changed

- `go.mod`: ks-types `v0.15.0` → `v0.19.0`（消费 `AppSpec.Provides.Capabilities[]` 新 schema）。
- `go.mod`: 新增 `github.com/gorilla/websocket v1.5.1` 运行时依赖（EventsClient WS 模式用）。
- `app.go`: `Mux()` 入口自动 finalize + wire capabilities（幂等保护），先于 `mcpToolDefs` 调；新增 `CallCapability(name)` / `SetScopedJWTTestKey` / `SetDispatcherClient` / `SetEventsClient` 入口方法。
- `auth_resolver.go`: ks-types v0.19.0 已删 `MountSpec.Service` / `MountSpec.Extension`；manifest auth_mode fallback 改用低层 yaml dict 解析（与 Python `ks_app/manifest.py` 对齐），保留 backward-compat 同时维持 `AuthMode.Valid()` 校验。

### 实现说明

- caller-side 入口命名为 `App.CallCapability(name)`（`Capability` 已是 `ToolDef.Capability` 字段名）；Python SDK 对齐为 `app.call_capability()`。
- 自动指数退避当前不在 SDK 内做（`DispatcherClient.Invoke` 直接返错），由 caller 自行管理。
- `DispatcherClient` 与 `ScopedJWTMiddleware` 都放 ksapp 主包：构造 ksapp 错误层级 / 避免 auth 子包反向 import 主包形成的循环依赖。

## [sdk/go/v0.7.1] - 2026-05-19

### Added

- `ToolBuilder.WithAnnotations(map[string]any)` 新增方法，设置 MCP 2025-03-26
  tool annotations（`readOnlyHint` / `destructiveHint` / `idempotentHint` /
  `openWorldHint`）。
- `mcpproto.ToolDef.Annotations` 字段，透传到 `tools/list` 响应（streamable +
  legacy 两条 list 路径）。
- 让 LLM 客户端能在 tools/list 阶段读到工具的安全调用提示。

## [sdk/go/v0.7.0] - 2026-05-18

### Added

- `keystoneclient.SelfClient`：应用启动时通过 `KS_APP_TOKEN` 调
  keystone `GET /v1/apps/self/resources` 自取本安装实例被分配的托管资源
  凭证（`DB_HOST` / `DB_PASSWORD` / `HMAC_SECRET` 等）。失败统一用
  `errors.Is(err, keystoneclient.ErrFetchFailed)` 断言。
- `ksapp.App.New` 集成 `maybeFetchKeystoneManagedEnv`：探测
  `KS_APP_TOKEN` + `KS_GATEWAY_URL` 同时存在则拉取并 `os.Setenv` 注入
  （仅当 key 不在 `os.Environ` 时写入；等价 Python `setdefault`）。
  失败 `slog.Warn` 不 panic，让 application 层在校验必填字段时报更具体的错。
- `ksapp fetch-env --gateway ... --token ... [--format dotenv|json|shell]`
  CLI 子命令，供非 SDK 进程拉取相同凭证。dotenv 输出用
  `# ─── BEGIN/END KEYSTONE MANAGED ───` marker 包夹，与 Python SDK
  字节级一致（跨语言 .env.local 互相兼容）。

### Rationale

托管资源自查机制的 Go 端实现。生产容器化路径不受影响——当应用由 keystone
自身的 lifecycle 启动、未注入 `KS_APP_TOKEN` 时 SDK 自动跳过，继续走
`runtime.env` 挂载路径。

## [sdk/go/v0.6.0] - 2026-04-20

### Fixed (ksconfig)

- `ksconfig.ReflectConfigSchema` 的 `reflect.Slice`（elem struct）/ `reflect.Struct` 分支
  修复 MVP 限制：现会把 sub-struct 内部字段的 `show_when` 产出的 allOf 片段向上透传
  （slice → `items.allOf`；struct → 父字段 `allOf`），UI Schema sub-tree 同步透传
  （slice → `ui[field].items`；struct → 合并到 `ui[field]`）。
- 修复点：`reflect.go` 内 `buildFieldSchema` 的 `subProps, _, subReq, _, err` → `subProps, subUI, subReq, subAllOf, err`
- 解除此前 sub-struct/slice-of-struct 内部字段 `show_when` 不向上透传的 MVP 限制。

### Changed (ksconfig)

- 更新 `ReflectConfigSchema` / `reflectStructFields` 函数头部注释，移除“MVP 限制”字样，
  改为明确透传行为；`reflectStructFields` 返回签名说明统一为“5 元组”。

## [sdk/go/v0.5.0] - 2026-04-20

### Added (ksconfig)

- `ksconfig.TagSpec.ShowWhen string` 字段
- `ksconfig.ParseTag` 支持 `show_when` key（show_when DSL）
- `ksconfig.ReflectConfigSchema` 自动把字段上的 `show_when` DSL 同时编译到：
  - JSON Schema 侧：`schema["allOf"]` 追加 `{if, then, else}` 片段
  - UI Schema 侧：`ui[fieldName]["ui:hidden_when"]` 注入 rjsf widget 消费的 AST

### Added (ksapp)

- `ksapp.ErrNotConfigured` 哨兵 error（可 `errors.Is` 断言）
- `ksapp.NewErrNotConfigured(scope, format string, args ...any) error` 构造器
  （字符串前缀 `"ERR_NOT_CONFIGURED: <scope> <detail>: ERR_NOT_CONFIGURED"`，与
  历史实现黑盒等价）

### Known Limits (ksconfig)

- `show_when` 表达式在 ksconfig tag 内禁用 `,` 和 `:`（tag 分隔符冲突），`in [a,b,c]`
  多元素列表只能通过编程式调用 `CompileShowWhen` 使用；tag 场景改用 `op1 || op2`
  表达等价逻辑
- sub-struct / slice-of-struct 内部字段的 `show_when` allOf 暂不向上透传（顶层字段
  生效）

## [sdk/go/v0.4.1] - 2026-04-17

### Added
- `resolveAuth()` 支持 `mount.extension.auth_mode` fallback（对齐 service mount 语义）
- 依赖 `ks-types@v0.4.1`

### Rationale
支持 type=extension 服务通过 manifest 声明 auth_mode。

## [sdk/go/v0.4.0] - 2026-04-17

### Added

- `ksapp/auth/` 新子包：JWKSVerifier + RequireJWT net/http middleware
- Option 模式：`WithKeystoneAuth` / `WithoutAuth` / `WithVersion` / `WithManifest`
- `App.authMode` / `App.version` / `App.jwksURL` / `App.manifestPath` 字段
- `resolveAuth()` 三层优先级（代码 Option > manifest > 默认）+ KS_APP_AUTH_MODE=insecure 逃生 + strict-by-default
- service-go 模板默认开启 `WithKeystoneAuth`

### Changed

- **Breaking**: `/meta` 端点响应从 `{app_id, tools}` 改为 `ks-types.MetaResponse` 格式 `{name, version, auth_mode, tools, config_ui?}`
- **Breaking**: `New(id)` → `New(id, opts ...Option)`；无 Option 行为与旧版一致（默认 authMode=none）

### Depends

- `github.com/wuhanyuhan/ks-types@v0.4.0`
- `github.com/golang-jwt/jwt/v5@v5.3.1`
- `gopkg.in/yaml.v3@v3.0.1`

### Rationale

对接 Keystone 生态 MCP 服务鉴权统一协议，实现 strict-by-default
安全姿态；auth_mode 声明和 keystone 侧动态签 JWT 端到端闭环。
