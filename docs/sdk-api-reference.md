# SDK API 参考

## 能力 × 语言矩阵

三语言 SDK 并非完全对齐。下表是诚实的能力覆盖（✅ 有 / ⚠️ 部分或仅内部 / ❌ 无）：

| 能力 | Go (`ksapp`) | Python (`ks_app`) | TypeScript (`@wuhanyuhan/ks-app`) |
|---|:---:|:---:|:---:|
| App 核心（创建 / 运行） | ✅ | ✅ | ✅ |
| LLM relay（`app.LLM` / `app.llm`） | ✅ | ✅ | ✅ |
| Embedding / VectorStore | ✅ | ✅ | ✅ |
| Capability Mesh — provider | ✅ | ✅ | ✅ |
| Capability Mesh — caller | ✅ | ✅ | ✅ |
| 事件订阅（events，ws / polling） | ✅ | ✅ | ✅ |
| scoped JWT 中间件 | ✅ | ✅ | ✅ |
| 类型化 config-schema | ✅ | ✅ | ❌ |
| keystore / crypto | ✅ | ✅ | ❌ |
| Tool UI widgets | ✅ | ✅ | ❌ |
| 幂等 key helper | ✅ | ✅ | ⚠️ |
| keystone-self 托管 env | ✅ | ✅ | ✅ |

脚注：

- **Tool UI widgets**：Go 经 `ToolBuilder.WithToolUI(kstypes.ToolUIBinding{...})` 绑定；Python 另带富 widget 构造类（`WidgetCardGridV1` / `WidgetTimelineV1` 等）；TS 无。见 [squad-tool-ui-quickstart.md](squad-tool-ui-quickstart.md)。
- **幂等 key helper**：Go / Python 暴露 `IsValidIdempotencyKey` / `is_valid_idempotency_key`；TS 仅 dispatcher 内部使用，无公开 helper。
- **keystone-self 托管 env**：Go / Python 启动期自动注入（读 env 即可）；TS 另有显式 `fetchKeystoneManagedEnv` / `SelfClient`。
- **config-schema / keystore / crypto**：TS SDK 无对应实现，配置加密由 Keystone Web 前端承担。见 [config-schema.md](config-schema.md)、[keystore-and-crypto.md](keystore-and-crypto.md)。

## HTTP 端点契约

Go SDK 和 Python SDK 均实现以下端点，行为一致：

### GET /healthz

存活探针。聚合所有通过 `HealthCheck()` 注册的自定义检查。

全部通过: `{"status": "ok"}` (200)

任一失败: `{"status": "unhealthy", "checks": {"db": "connection refused"}}` (503)

### GET /readyz

就绪探针。

响应: `{"status": "ok"}`

### GET /meta

应用元信息。

响应:
    {
      "app_id": "my-app",
      "tools": [
        {"name": "greet", "description": "打招呼"}
      ]
    }

### POST /mcp

MCP Streamable HTTP 端点（JSON-RPC 2.0，遵循 MCP 2025-03-26 规范）。

支持的 method：`tools/list`、`tools/call`、`initialize`。

### GET /mcp/tools/list

已注册工具列表（legacy 端点）。

响应:
    {
      "tools": [
        {"name": "greet", "description": "打招呼"}
      ]
    }

### POST /mcp/tools/call

调用工具（legacy 端点）。

请求:
    {"name": "greet", "params": {"name": "world"}}

成功响应 (200):
    {"result": {"message": "Hello, world!"}}

工具不存在 (404):
    {"error": "tool not found"}

JSON 解析失败 (400):
    {"error": "invalid json: ..."}

Handler 异常 (500):
    {"error": "工具执行失败"}

> 注意：Handler 的原始错误信息只记录到服务端日志，不会返回给客户端（防止泄露内部信息）。

## 扩展 API

### Handle / HandleFunc

注册自定义 HTTP 路由，用于添加 REST API、静态文件服务等。

Go: `app.Handle("GET /api/items", handler)` / `app.HandleFunc("GET /api/items", fn)`

Python: `app.handle("/api/items", endpoint, methods=["GET"])`

### Use

注册全局 HTTP 中间件。

Go: `app.Use(func(next http.Handler) http.Handler { ... })`

Python: `app.use(MiddlewareClass, **kwargs)`

### HealthCheck

注册自定义健康检查，聚合到 `/healthz` 端点。

Go: `app.HealthCheck("db", func() error { return db.Ping() })`

Python: `app.health_check("db", check_fn)`

### LLM Relay Client（`app.LLM()` / `app.llm()`）

SDK 提供了打 Keystone LLM relay 端点的客户端。**仅封装 relay 模式**；若需直连 LLM 提供商，请用 openai-python / anthropic / litellm 等现有库。

#### 先决条件

- manifest 声明 `permissions.llm: { level: host_proxy }`（走 Keystone relay 代理大模型）
- Keystone 安装应用时会自动注入 `KS_RELAY_TOKEN` 环境变量
- 本地开发时需手动 `export KS_RELAY_TOKEN=<token>` 和 `export KS_GATEWAY_URL=<url>`

#### 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `KS_GATEWAY_URL` | `http://localhost:9988` | Keystone 地址 |
| `KS_RELAY_TOKEN` | （无，必须由 Keystone 注入） | Relay 鉴权 token（兼容别名 `KEYSTONE_RELAY_TOKEN`） |

#### Go API

```go
llm := app.LLM()
resp, err := llm.Chat(ctx, ksapp.ChatRequest{
    Model: "gpt-4o-mini",
    Messages: []ksapp.Message{{Role: "user", Content: "你好"}},
})
err = llm.StreamChat(ctx, req, func(chunk ksapp.Chunk) { ... })
```

#### Python API

```python
llm = app.llm()
resp = await llm.chat(ChatRequest(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "你好"}],
))
async for chunk in llm.stream_chat(req):
    ...
```

#### TypeScript API

```typescript
const llm = app.llm();
const resp = await llm.chat({ model: "gpt-4o-mini", messages: [...] });
for await (const chunk of llm.streamChat(req)) { ... }
```

#### 错误类型

| 场景 | Go | Python | TS |
|---|---|---|---|
| 缺 token | `ErrNotConfigured`（通过 `NewErrNotConfigured("llm-relay", ...)` 构造，`errors.Is` 断言） | `LLMNotConfiguredError` | `LLMNotConfiguredError` |
| 401/403 | `ErrUnauthorized` | `LLMUnauthorizedError` | `LLMUnauthorizedError` |
| 429 | `ErrRateLimited` | `LLMRateLimitedError` | `LLMRateLimitedError` |
| 5xx | `ErrUpstream` | `LLMUpstreamError` | `LLMUpstreamError` |

> Go 侧 `ErrNotConfigured` 是跨模块共享的哨兵（定义于 `ksapp/errors.go`），调用方用 `errors.Is(err, ksapp.ErrNotConfigured)` 断言；Python / TS 侧是继承 `RuntimeError` / `Error` 的具名异常类。

#### 厂商私有参数透传

需要调用 Qwen 思考模式、GLM thinking、Claude extended thinking 等非 OpenAI 标准字段，用 `request_options.openai_extra_body`：

**Go**:

```go
req.RequestOptions = map[string]interface{}{
    "openai_extra_body": map[string]interface{}{"enable_thinking": true},
}
```

**Python**:

```python
req.request_options = {"openai_extra_body": {"enable_thinking": True}}
```

**TypeScript**:

```typescript
req.request_options = { openai_extra_body: { enable_thinking: true } };
```

这些字段由 Keystone 透传到上游，SDK 不理解具体语义。

### Capability Mesh Provider（`RegisterCapability` / `@app.capability` / `registerCapability`）

对外暴露 capability（进能力网格，可被 dispatcher 路由 / 跨 app 调用 / long_running）。作者注册【裸名】，全局 `canonical = <app_id>.<name>` 由 SDK/keystone 派生（作者不写前缀）。handler 收 `CapabilityContext`（透传发起方 user/caller/chain）+ args。

run()/finalize 时按 manifest `provides` 做**复用四象限**（`backend.kind: mcp_tool`）：有独立 handler × 命中同名 `app.Tool` → 报错（真冲突）；有 handler 无同名 tool → 生成 MCP tool；无 handler 命中同名 tool → 复用；**无 handler 也无同名 tool → 启动期报错（orphan→error，clean-break BREAKING）**。详见 [decision-guide.md](decision-guide.md#三复用四象限mcp_tool-backend)。

**命名两层**：wire（manifest / dispatcher payload）一律 snake_case；SDK API 面用各语言惯用——`CapabilityContext` 字段 Go `ctx.UserID()/CallerID()/ChainID()/Context()`、Python `ctx.user_id/caller_id/chain_id`、TS `ctx.userId/callerId/chainId`。

#### Go API

```go
app.RegisterCapability("hello", func(ctx ksapp.CapabilityContext, args map[string]any) (any, error) {
    name, _ := args["name"].(string)
    return map[string]any{"message": "Hello, " + name}, nil
})
```

#### Python API

```python
@app.capability("hello")
async def hello(ctx: CapabilityContext, args: dict) -> dict:
    return {"message": f"Hello, {args.get('name')}"}
```

#### TypeScript API

```typescript
app.registerCapability("hello", async (ctx, args) => {
  return { message: `Hello, ${args.name as string}` };
});
```

### Capability Mesh Caller（`app.CallCapability()` / `app.call_capability()`）

作为 caller 调用 Capability Mesh 中其他应用暴露的 capability（需 manifest 声明 `requires.capabilities[]`）。`WithOnBehalfOfUser` / `on_behalf_of_user_id` 用于多跳调用链穿透发起人 user_id：仅当 `> 0` 时写入 dispatcher payload 的 `on_behalf_of_user_id` 字段（Go / Python 双端语义一致，由 `shared-fixtures/dispatcher_invoke_on_behalf_of.json` 锁定）。

#### Go API

```go
res, err := app.CallCapability("demo-writer.publish").
    WithOnBehalfOfUser(12345).
    Invoke(ctx, map[string]any{"title": "..."})
task, err := app.CallCapability("image-gen.generate").
    Submit(ctx, map[string]any{"prompt": "..."})
```

#### Python API

```python
result = await app.call_capability("demo-writer.publish").invoke(
    on_behalf_of_user_id=12345,
    title="...",
)
task = await app.call_capability("image-gen.generate").submit(prompt="...")
out = await task.result(timeout=60)
```

### Embedding 与 Vector Store（`app.Embedding()` / `app.embedding`）

托管 embedding + 向量集合（需 manifest 声明 `platform_services.embedding.required: true` 与 `managed_resources.vector_store`）。向量检索唯一路径是 `SearchText` / `search_text`：传原始查询文本，服务端 embed 后做 dense+sparse RRF hybrid 融合（无 dense-only / mode 选项）。

#### Go API

```go
vec, err := app.Embedding().Embed(ctx, "要写入的文本")  // vec.Dense / vec.Sparse
store := app.VectorStore("knowledge")
err = store.Upsert(ctx, []ksapp.Point{{ID: "d1", Dense: vec.Dense, Sparse: vec.Sparse}})
hits, err := store.SearchText(ctx, "查询文本", ksapp.SearchOptions{TopK: 5})
```

#### Python API

```python
emb = await app.embedding.embed("要写入的文本")          # emb.dense / emb.sparse / emb.tokens
store = app.vector_store("knowledge")
await store.upsert([Point(id="d1", dense=emb.dense, sparse=emb.sparse)])
hits = await store.search_text("查询文本", top_k=5)
```

#### TypeScript API

```typescript
const vector = await app.embedding.embed("要写入的文本");      // vector.dense / vector.sparse
const store = app.vectorStore("knowledge");
await store.upsert([{ id: "d1", dense: vector.dense, sparse: vector.sparse }]);
// searchText 走托管 dense+sparse RRF hybrid，选项只有 { topK, filter }
const hits = await store.searchText("查询文本", { topK: 5 });
```

### Mux / create_app

获取底层 HTTP handler/app 实例，用于高级定制。

Go: `handler := app.Mux()`

Python: `starlette_app = app.create_app()`

### 事件订阅（Events）

订阅 task 事件流（`long_running` 能力的进度 / 中间产物推送）。两种传输：`ws`（默认，长连 `/v1/apps/self/events`）或 `polling`（轮询 `?since=<cursor>`）。

- Go：`ksapp.NewEventsClient(gatewayURL, appToken string, mode ksapp.EventsMode) *EventsClient`（`EventsModeWS` / `EventsModePolling`）
- Python：`EventsClient`（`event_mode="ws"|"polling"`）；通常经 `await task.events()` 异步迭代消费
- TypeScript：`EventsClient`、`type EventsMode = "ws" | "polling"`

**何时用**：`Submit` 了一个 long_running 能力调用，想实时拿进度而非阻塞等 `result()`。

### Dispatcher 客户端（底层传输）

`DispatcherClient` 是与 keystone capability dispatcher 通讯的底层 HTTP 客户端。**绝大多数情况不直接用**——上层 ergonomic API 是上文的 Capability Mesh Caller（`app.CallCapability()`）。

- Go：`ksapp.NewDispatcherClient(gatewayURL, appToken string)` + `(*DispatcherClient).Invoke(ctx, InvokeOptions) (*InvokeResult, error)`
- Python：`DispatcherClient`（`ks_app.keystone_client.dispatcher_client`）
- TypeScript：`DispatcherClient`

### 幂等（Idempotency）

能力调用 / 配置保存用 uuid-v4 幂等 key 去重重试。公开面是 key 校验器：

- Go：`ksapp.IsValidIdempotencyKey(s string) bool`
- Python：`is_valid_idempotency_key(s) -> bool`

SDK 内部对 `/ks-config/save` 按 per-handle LRU（容量 64 / TTL 10min）去重；TS SDK 暂无对应公开 helper。

### 运行时上下文（Context）

能力 handler 收 `CapabilityContext`（见上文 Capability Mesh Provider 节）。**非能力 handler**（`app.Tool` / 自定义 HTTP 路由）则从标准 context 读同一份发起方身份：

- Go 自由函数：`ksapp.UserID(ctx)` / `ksapp.CallerID(ctx)` / `ksapp.ChainID(ctx)` / `ksapp.RequestID(ctx)`（均 `(context.Context) string`）
- Python：`get_context() -> ToolContext`（字段 `user_id` / `caller_id` / `chain_id` …，见 SDK README 的 ToolContext 表）；能力侧用 `CapabilityContext`
- TypeScript：`CapabilityContext`（`userId` / `callerId` / `chainId`）

**何时用**：普通 tool / HTTP handler 里需要发起 user 或调用链做审计 / 鉴权。

### Tool UI Widgets

squad / agent 的工具可返回结构化 UI widget（卡片、列表、时间线、diff 审阅等），由 Keystone 前端渲染。完整清单与用法见 [squad-tool-ui-quickstart.md](squad-tool-ui-quickstart.md)。

## 配置

| 环境变量 | Go 默认值 | Python 默认值 | 说明 |
|----------|-----------|---------------|------|
| KS_APP_PORT | 8080 | 8080 | 监听端口 |
| KS_APP_HOST | (不适用) | 0.0.0.0 | 监听地址（仅 Python） |
| KS_GATEWAY_URL | http://localhost:9988 | http://localhost:9988 | LLM Relay 网关地址 |
| KS_RELAY_TOKEN | (无) | (无) | LLM Relay 令牌 |

## 类型化配置（config-schema）

`install.yaml` 退役后的官方替代：作者用代码声明配置结构，SDK 反射出 JSON Schema + UI Schema，平台安装向导渲染表单、信封加密下发，SDK 解密 / 校验 / 热切换 / 落盘。完整文法、敏感字段、加密落盘与端点见 [config-schema.md](config-schema.md)。

| 语言 | 注册入口 | 读取当前快照 |
|---|---|---|
| Go | `ksapp.NewConfigOn(app, ksapp.ConfigSpec[T]{OnValidate, OnApply})` | `cfg.Get() *T`（未配置为 nil） |
| Python | `new_config(app, ModelCls, ConfigSpec(on_validate, on_apply))` | `cfg.get() -> T \| None` |
| TypeScript | ❌ 暂无类型化 config-schema | 走 managed_resources / platform_services 注入或自管 env |
