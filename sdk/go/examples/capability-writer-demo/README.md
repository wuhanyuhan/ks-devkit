# capability-writer-demo (Go)

ksapp v0.8.0 capability mesh 端到端示例。与 `sdk/python/examples/capability_writer_demo/` 对应。

演示两条 backend 路径：

- `writer-demo-go.list_articles` — `mcp_tool` backend：SDK 自动把 capability 包装成同名 MCP tool（`list_articles`），通过 `POST /mcp` (tools/call) 调用。
- `writer-demo-go.create_article` — `http_endpoint` backend：SDK 把 capability 暴露为 `POST /capabilities/create_article`，套 `ScopedJWTMiddleware` 校验 `aud == writer-demo-go.create_article`。

## 跑

```bash
cd ks-devkit/sdk/go/examples/capability-writer-demo
# caller-side（CallCapability）才需要下面两个 env；纯 callee 跑可省
export KS_APP_TOKEN=fake-token
export KS_GATEWAY_URL=http://localhost:8080

go run .
```

服务起在 `:8000`：

- `POST /mcp`（tools/call name=`list_articles`）— mcp_tool backend
- `POST /capabilities/create_article`（Bearer scoped JWT，`aud=writer-demo-go.create_article`）— http_endpoint backend

## 设计要点

- manifest 顶层使用 ks-types v0.19.0 flat schema（`id` / `name` / `version` / `type` / `runtime` / `provides`）。
- 启动期 `app.Mux()` 自动调 `finalizeCapabilities` → 校验已注册 capability 必须出现在 `manifest.provides.capabilities[]`，违反返 `ErrManifestMismatch`。
- mcp_tool wiring 与 `App.Tool` 同名冲突会启动期报错，禁止覆盖用户显式注册。
- http_endpoint handler 从 `ScopedClaims` 直接构造 `CapabilityContext`（`UserID=sub`，`AppID=kx_caller_id`，`ChainID=kx_chain_id`，...）。
- `CapabilityContext.Progress(stage, percent)` 在 sync 路径下自然 no-op；long_running 任务路径下走 `DispatcherClient.ReportProgress`（依赖 `KS_APP_TOKEN` + `KS_GATEWAY_URL` env）。
