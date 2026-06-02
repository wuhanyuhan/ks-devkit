# ks-devkit Go SDK

Keystone MCP Service SDK for Go.

## 安装

```bash
go get github.com/wuhanyuhan/ks-devkit/sdk/go@v0.13.0
```

## 使用

```go
package main

import (
    "context"

    ksapp "github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp"
)

func main() {
    app := ksapp.New("my-app")

    app.Tool("greet", "打招呼", func(ctx context.Context, params map[string]any) (any, error) {
        name, _ := params["name"].(string)
        return map[string]string{"message": "Hello, " + name}, nil
    })

    app.Run() // 监听 :8080，收到 SIGINT/SIGTERM 优雅关闭
}
```

## API

### ksapp.New(id string) *App

创建应用实例。

### (*App).Tool(name, description string, handler ToolHandler) *App

注册工具。支持链式调用。同名工具重复注册会 panic。

### (*App).ToolWithSchema(name, description string, schema map[string]any, handler ToolHandler) *App

注册工具并指定显式 JSON Schema。

### (*App).Handle(pattern string, handler http.Handler) *App

注册自定义 HTTP 路由（支持 Go 1.22+ ServeMux 模式，如 `"GET /api/items"`）。

### (*App).HandleFunc(pattern string, handler http.HandlerFunc) *App

`Handle` 的函数版便捷方法。

### (*App).Use(middleware Middleware) *App

注册全局 HTTP 中间件，应用到所有路由（含内置端点）。按注册顺序从外到内包装。

### (*App).HealthCheck(name string, check func() error) *App

注册自定义健康检查。`/healthz` 端点会聚合所有检查结果，任一失败返回 503。

### (*App).LLM() *LLMClient

返回 Keystone LLM Relay 客户端（需要 manifest 声明 `permissions.llm: { level: host_proxy }`）。`Chat` / `StreamChat` / 错误类型 / 厂商私有参数透传用法见 [SDK API 参考 ·「LLM Relay Client」](../../docs/sdk-api-reference.md)。

### (*App).Embedding() *EmbeddingClient

返回 Keystone 托管 embedding 客户端（需要 manifest 声明 `platform_services.embedding.required: true`）。`Embed` 用法见 [SDK API 参考 ·「Embedding 与 Vector Store」](../../docs/sdk-api-reference.md)。

### (*App).VectorStore(collection string) *VectorStoreClient

返回 Keystone 托管向量集合客户端。集合名必须使用 manifest 中声明的托管集合名。`Upsert` / `SearchText`（服务端 embed→dense+sparse RRF hybrid，向量检索唯一路径）用法见 [SDK API 参考 ·「Embedding 与 Vector Store」](../../docs/sdk-api-reference.md)。

### (*App).CallCapability(name string) *CapabilityCall

作为 caller 调用 Capability Mesh 中其他应用暴露的 capability（需 manifest 声明 `requires.capabilities[]`）。链式可选 `WithOnBehalfOfUser` 透传调用链发起人 user_id（仅 >0 时透传）。`Invoke` / `Submit` / `WithOnBehalfOfUser` 用法见 [SDK API 参考 ·「Capability Mesh Caller」](../../docs/sdk-api-reference.md)。

### (*App).Mux() http.Handler

返回已配置的 HTTP Handler（含中间件包装），用于高级场景（如自定义 Server）。

### (*App).Run()

启动 HTTP 服务器，阻塞直到收到终止信号。

### (*App).RunWithContext(ctx context.Context)

同 `Run()`，但接受外部 context 用于协调优雅关闭。

## 端点

| 路径 | 方法 | 说明 |
|------|------|------|
| /healthz | GET | 存活探针（聚合自定义健康检查） |
| /readyz | GET | 就绪探针 |
| /meta | GET | 应用元信息 + 工具列表 |
| /mcp | POST | MCP Streamable HTTP（JSON-RPC 2.0） |
| /mcp/tools/list | GET | 已注册工具列表（legacy） |
| /mcp/tools/call | POST | 调用工具（legacy，错误已脱敏） |

## 配置

| 环境变量 | 默认值 | 说明 |
|----------|--------|------|
| KS_APP_PORT | 8080 | 监听端口 |
| KS_GATEWAY_URL | http://localhost:9988 | LLM Relay 网关地址 |
| KS_RELAY_TOKEN | (无) | LLM Relay 令牌 |
| KS_EMBEDDING_MODEL | (无) | 托管 embedding 模型 ID，由 Keystone 注入 |
| KS_EMBEDDING_DIM | 0 | 托管 embedding 向量维度，由 Keystone 注入 |
