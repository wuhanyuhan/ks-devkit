# ks-app-sdk Changelog

## [Unreleased]

### Added

- `ks_app.context` 新增 `ks_conversation_id` 字段（`ToolContext.conversation_id` + `_set_meta` 读取 + `_reset_meta` 清理）：keystone 调工具时经 `_meta` 透传 keystone 会话 ID，被调服务据此把决策门 / 交付物回流到正确会话。与 `ks_user_id` 等同款，与 Go SDK 对齐。

## [sdk/python/v0.9.0] - 2026-05-31

> capability mesh 去前缀 + mcp_tool 复用 + 形态对齐 + Go↔Python wire-compat。含 BREAKING，迁移见下。

### Changed

- **BREAKING**：`@app.capability` 改收**裸名** `name`（去前缀），全局 `canonical_name` 由 SDK 内部派生 `<app_id>.<name>`；manifest `provides.capabilities[].name` 同步写裸名。caller 侧 `call_capability(<full_name>)` 与 `requires.capabilities[].canonical_name` **维持全名**（引用他人能力无法本地派生，故不对称）。
  - 迁移：把 `@app.capability("acme-app.foo")` 改 `@app.capability("foo")`，manifest 的 `canonical_name: acme-app.foo` 改 `name: foo`；caller 侧调用不变。
- **BREAKING**：`CapabilityContext.app_id` 正名 `caller_id`（承载 wire `ks_caller_id` 语义）。迁移：handler 里 `ctx.app_id` 改 `ctx.caller_id`（`App.app_id` 应用 id 不受影响）。
- **BREAKING**：`mcp_tool` capability 在 manifest 声明、但既无 `@app.capability` handler 也无同名 `@app.tool` 时，`create_app()` 改为抛 `ValueError`（无承载），替代旧的 warn-not-error。多实例拆分部署需各实例用 per-instance 裁剪 manifest，只声明本实例实际承载的 capability。

### Added

- `mcp_tool` 复用四象限：`backend.tool_name` 命中已有 `@app.tool` 且无独立 handler 时**复用**该 tool（join，不再 hard fail）；有 handler 且撞名报真冲突；有 handler 无撞名生成新 tool。
- `CapabilityCall.invoke/submit` 支持 `chain_id` / `chain_header` 透传（多跳 capability mesh 调用链穿透，对齐 Go `WithChainContext` wire；走 `X-Keystone-Chain-Id` / `X-Keystone-Call-Chain` header）。
- `CapabilityContext.chain_header` 字段；mcp_tool 路径从 `_meta.ks_chain_snapshot` 读调用链快照注入。⚠️ 完整生效需 keystone mcptool executor 配套透传 `ks_chain_snapshot`；未透传时为空但不回归。
- `ToolContext` 暴露 `caller_id` / `caller_kind` / `chain_id`（复用降级路径：复用普通 `@app.tool` 时 handler 经 `get_context()` 取 caller 上下文）。
- manifest `provides.capabilities[]` 解析对齐 ks-types v0.29.0：裸名 `name` + `input_schema`，并把 `input_schema` 透传到生成的 MCP tool。

## [sdk/python/v0.8.0] - 2026-05-31

### Added

- `ChatRequest` 新增 LLM intent 一等字段 `tier`（economy/standard/flagship）与 `reasoning`（on/off/auto）；`to_dict()` 降解进 `request_options` 约定 key（`tier` / `reasoning_mode`），与既有 `require_capabilities`（`<cap>_required`）同模式；relay 顶层 schema 零改、不传 intent 时行为不变。
- `LLMCapabilityUnavailableError`：网关 422 `capability_unavailable`（现场无满足所需能力如 vision 的模型）映射为可判别异常，携带 `missing` 列表供调用方自行降级；与 capability-mesh 的 `CapabilityUnavailable`（dispatch 域）区分。
- 跨语言 wire fixture `shared-fixtures/relay_chat_intent.json` + conformance，锁定 Go/Python intent `request_options` 字节等价。

## [sdk/python/v0.7.0] - 2026-05-29

### Added

- `app.embedding` / `EmbeddingClient`：通过 Keystone relay 调 `/v1/mcp/relay/embeddings`。
- `app.vector_store(collection)` / `VectorStoreClient`：封装 `upsert` / `search_text` / `delete` / `delete_by_filter` / `count`。
- **caller-side「代表用户调用」**：`call_capability(name).invoke(on_behalf_of_user_id=...)` / `.submit(on_behalf_of_user_id=...)` 新增 keyword-only 参数 `on_behalf_of_user_id`（int），透传到 dispatcher 的 `on_behalf_of_user_id` payload 字段（仅 `>0` 时发送），与 Go SDK `CapabilityCall.WithOnBehalfOfUser` 语义对齐。新增跨语言 wire fixture `shared-fixtures/dispatcher_invoke_on_behalf_of.json` 锁定 Go/Python 双端 payload 一致。

### Changed

- **BREAKING**：向量检索收敛为 hybrid 唯一路径（dense+sparse RRF），三端一致 clean-break。移除 dense-only `search(query_vector=...)` 入口与 `mode` 选项，统一通过 `search_text(text=...)` 走服务端 embed→hybrid（RRF）检索。
  - **迁移**：`vs.search(query_vector=emb.dense, top_k=5)` → `vs.search_text("query text", top_k=5)`（改传原始查询文本，dense+sparse 嵌入与 RRF 融合由服务端完成）。

### Fixed

- capability handler 的 `arguments` 支持内嵌 `_meta`（与 MCP tool 路径一致）。

## [sdk/python/v0.6.0] - 2026-05-19

### Added

- **Capability Mesh 支持**：应用作者可用 `@app.capability(canonical_name)` decorator 注册 capability handler，SDK 启动期按 manifest `provides.capabilities[]` 自动接入两条 backend 路径：
  - `backend.kind=mcp_tool`：capability 自动注册成同名 MCP tool（`tool_name` 由 manifest 指定），handler 从 `_meta.ks_*` 还原 `CapabilityContext`
  - `backend.kind=http_endpoint`：capability 注册成 Starlette POST route，挂 `ScopedJWTMiddleware` 校验 `aud == canonical_name`
- **Caller-side**：`app.call_capability(canonical_name).invoke(**args)` / `.submit(**args)` 寻址调用其他应用 capability；返 `Task` 对象，支持 `await task.result()` / `async for ev in task.events()` / `await task.cancel()` / `await task.refresh()`
- **CapabilityContext**：与现有 `ToolContext` 并列；提供 `user_id` / `app_id` / `caller_kind` / `chain_id` / `task_id` / `request_id` + `ctx.progress(stage, percent)` / `ctx.deadline()` / `ctx.cancelled()`
- **DispatcherClient**：调 keystone `POST /v1/apps/self/invoke` / `POST /v1/user-tasks/:id/progress` / `GET /v1/user-tasks/:id` / `POST /v1/user-tasks/:id/cancel`；HTTP 错误按错误层级映射
- **EventsClient**：inbound WS 连 `/v1/apps/self/events`（lazy，首次 `submit` 时启动）+ polling fallback (`event_mode='polling'`)；指数退避重连；`task_id → asyncio.Queue` 路由到 `Task.events()`
- **错误层级**：25 个错误类，从 `KeystoneError` 基类下 8 子树（Auth/Permission/NotFound/Validation/Dependency/Execution/RateLimit）
- **ScopedJWTVerifier**：RS256/EdDSA scoped JWT 验签 + aud 校验 + claims schema；从 JWKS 取 key（与现有 `JWKSVerifier` 同源），测试可注入静态 key
- **示例**：`examples/capability_writer_demo/` —— 同时演示 `mcp_tool` + `http_endpoint` 两条 backend 路径 + caller-side 跨应用调用

### Changed

- `manifest.py` 扩展：新增 `load_manifest_capabilities()` / `load_manifest_requires()` 解析 `provides.capabilities[]` / `requires.capabilities[]`（对齐 ks-types v0.19.0 capability mesh schema）
- `context.py` 扩展：新增 `_ks_caller_id` / `_ks_caller_kind` / `_ks_chain_id` ContextVar，`_set_meta` / `_reset_meta` 处理这三个新字段（dispatcher 调 mcp_tool backend 时透传 caller 身份）
- `keystone_client/` 子包新增 `DispatcherClient` export，原 `SelfClient` 行为不变

### Dependencies

- 新增 `websockets>=12.0` 运行时依赖（caller-side EventsClient WS 模式用）

### Rationale

Capability Mesh：让应用作者用一致的 decorator / caller API 接入，框架自动处理 scoped JWT 验签 / chain 注入 / progress 上报 / inbound 事件流。caller-side 入口命名为 `app.call_capability(name)`（`app.capability` 已被 decorator 工厂占用）；Go SDK 采用对齐命名 `App.CallCapability(name)`。

## [sdk/python/v0.5.0] - 2026-05-18

### Added

- `ks_app.keystone_client.SelfClient`：应用启动时通过 `KS_APP_TOKEN`
  调 keystone `GET /v1/apps/self/resources` 自取本安装实例被分配的
  托管资源凭证（DB_HOST / DB_PASSWORD / HMAC_SECRET 等）。失败抛
  `KeystoneSelfFetchError`。
- `App.__init__` 集成 `_maybe_fetch_keystone_managed_env`：探测
  `KS_APP_TOKEN` + `KS_GATEWAY_URL` 同时存在则拉取并注入 `os.environ`
  （等价 `setdefault`，不覆盖已存在键）。失败 `warn` 不 `raise`，让应用层
  pydantic-settings 在校验必填字段时报更具体的错。
- `ksapp fetch-env --gateway ... --token ... [--format dotenv|json|shell]`
  CLI 子命令，供非 SDK 进程（脚本 / Go 二进制）拉取相同凭证。dotenv
  输出用 `# ─── BEGIN/END KEYSTONE MANAGED ───` marker 包夹，支持脚本幂等替换。

### Rationale

托管资源自查机制的 SDK 端实现。当应用由 keystone 自身的 lifecycle
启动、未注入 `KS_APP_TOKEN` 时 SDK 自动跳过，继续走 `runtime.env`
挂载路径。配套 `ksapp fetch-env` CLI 供非 SDK 进程拉取相同凭证。

## [sdk/python/v0.4.1] - 2026-05-10

### Fixed

- `tools/list` MCP 协议响应现在优先使用 `App.tool(input_schema=...)` 显式传入
  的 schema；之前硬编码走 `schema_from_func` 自动推导，导致用户传入的
  `input_schema` 参数被静默丢弃——这是 v0.4.0 起暗藏的协议 bug，会让下游
  LLM 拿到残缺类型信息（无 description / enum / array.items）。

### Added

- `schema_from_func` 支持泛型注解：
  - `list[T]` / `List[T]` → `{"type": "array", "items": <T 的 schema>}`
  - `Optional[T]` / `T | None` → 等价于 T 的 schema，且参数自动从 required
    移除（None 是合法值）
  - `Literal["a", "b"]` → `{"type": "string", "enum": ["a", "b"]}`，
    字面量类型混合时只输出 enum
  - `dict[str, V]` → `{"type": "object"}`（不展开 V，避免无界递归）
- 不识别的高阶类型（如 `int | str` 多分支 Union、自定义类）继续按
  best-effort 输出无 type 的属性，保持向后兼容。

### Rationale

曾发现：编排官 LLM 看到 `search_news` 工具的
`topics: list[str]` 参数 schema 是空对象 `{}`，无法判断它是 array 还是 string，
结果把 `"AI,人工智能"` 当字符串传 → 工具按字符遍历 topics → 全空。本版本根治：
- 服务可以用 `App.tool(input_schema=...)` 显式声明完整 schema（含 description /
  enum / 多层 properties），SDK 原样透传
- 即使不显式声明，`list[str]` / `Optional[str]` / `Literal[...]` 这类常规
  注解也能被 schema_from_func 正确识别成对应 JSON Schema 形态

## [sdk/python/v0.4.0] - 2026-04-18

### Added

- `App.declare_nav(*, label, category, open_mode, icon?, order?, entry_path?, required_perms?)`：
  声明 MCP 在 Keystone 后台左侧菜单的导航项
- `App.declare_permission(*, code, label, default_roles?)`：
  声明 MCP 的权限码目录条目（可调多次）
- `App.set_config_mode(mode)`：设置配置模式（`schema` / `iframe` / `none`），含枚举校验
- `App.set_protocol_version(version)`：设置 MCP 协议版本（SemVer `MAJOR.MINOR`）
- `App.set_config_status(status)`：设置 MCP 配置状态
  （`unconfigured` / `via_frontend` / `via_cli` / `mixed`），含枚举校验
- `/meta` 响应按 omitempty 反射 5 个新字段：
  `nav` / `permissions` / `config_mode` / `protocol_version` / `config_status`
- `health_routes()` 签名扩展：新增 5 个 keyword-only 参数承接上述字段

### Changed

- `/meta` 响应对齐 `ks-types` v0.5.0 `MetaResponse`（新增 5 字段，向后兼容）

### Rationale

Keystone 生态前端统一要求 MCP 在 `/meta` 上以声明式方式上报导航、权限、
配置模式、协议版本与配置状态，由 Keystone 后端聚合渲染左侧菜单与权限校验。
本版本在不引入新依赖（无 pydantic）、保持 dict 风格的前提下，
为 `App` 类提供 5 个声明式方法，作为 ks-types v0.5.0 在 Python SDK 的镜像。

与 `mount_config_ui()` 的共存约定：

- `set_config_mode('iframe')` → 必须配套 `mount_config_ui(...)`
- `set_config_mode('schema' | 'none')` → 不应调 `mount_config_ui`
- 不调 `set_config_mode()` → 走老语义（看 `mount_config_ui` 是否调过）

## [sdk/python/v0.3.2] - 2026-04-17

### Fixed

- `context._set_meta` 对 dict / list 类型使用 `json.dumps(ensure_ascii=False)` 替代 `str()`：
  修复业务侧无法用 `json.loads(get_context().resource_scope)` 还原 Keystone 下发的 dict 结构
  的问题。原 `str({...})` 产出 Python repr（如 `"{'template_ids': ['a']}"`）不是合法 JSON。

### Rationale

某 MCP 应用迁移中发现 Keystone 通过 `_meta.ks_resource_scope` 下发的
`json.RawMessage` 经 Python SDK 的 `str()` coerce 后变成 repr 字符串，业务无法反序列化。
int / bool / None 等标量仍走 `str()`，向后兼容；dict / list 改走 `json.dumps`。

## [sdk/python/v0.3.1] - 2026-04-17

### Added

- `App.on_startup(fn)` / `App.on_shutdown(fn)` 装饰器：注册 async lifespan 钩子
- Starlette lifespan 协议驱动：startup 按注册顺序执行；shutdown 按注册反序执行（LIFO 资源释放语义）

### Rationale

某 MCP 应用迁移中发现需要 DB 连接池的启动/关闭协调，v0.3.0 无此能力。补齐框架级生命周期钩子。

## [sdk/python/v0.3.0] - 2026-04-17

### Added

- `ks_app.auth/` 子包：`JWKSVerifier` + `JWKSAuthMiddleware`（对齐 Go SDK auth/）
- `ks_app.auth_resolver`：三层优先级解析 effective auth mode + strict-by-default + `KS_APP_AUTH_MODE=insecure` 逃生
- `ks_app.manifest`：manifest.yaml 解析（支持 service & extension mount 的 auth_mode 与 config_ui）
- `App(id, *, keystone_auth=False, version=..., manifest_path=...)` keyword args 风格
- `app.mount_config_ui(directory, path="/config-ui")` 静态前端挂载
- `@app.tool(name, description, input_schema=None)` 支持可选 JSON Schema
- `ks_app.get_claims()` 在工具 handler 内读取当前请求 JWT claims
- SSE + JWKS 共存契约测试（`tests/test_sse_with_jwks.py`）

### Changed

- **Breaking**: `/meta` 响应从 `{app_id, tools}` 改为 `{name, version, auth_mode, tools: [{name, description, input_schema?}], config_ui?}`，对齐 `ks-types.MetaResponse`
- **Breaking**: `health_routes(app_id, tools, health_checks)` 签名变为 `health_routes(app_id, version, auth_mode, tools, health_checks, config_ui=None)`

### Dependencies

- 新增 `PyJWT[crypto]>=2.8.0`（引入 `cryptography` 作为可选依赖）

### Rationale

对齐 Go SDK v0.4.0 鉴权能力面，使 Python MCP 服务具备迁移到 ks_app 的合格承接底座。

## [sdk/python/v0.2.0] - 2026 之前

- 初版 MCP Service SDK，支持 `@app.tool` 装饰器、Streamable HTTP `/mcp`、legacy 端点、`get_context()` 读 MCP `_meta`。
