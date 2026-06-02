# @wuhanyuhan/ks-app

Keystone MCP service SDK for TypeScript / Bun / Node.

Claims compliance with: **conformance-v1.0.0**

## 安装

```bash
bun add @wuhanyuhan/ks-app zod @modelcontextprotocol/sdk
# 或
npm install @wuhanyuhan/ks-app zod @modelcontextprotocol/sdk
```

## 最小示例

```typescript
import { createApp } from "@wuhanyuhan/ks-app";
import { z } from "zod";

const app = createApp({
  id: "my-service",
  version: "1.0.0",
  auth: "keystone_jwks",
});

app.tool(
  "echo",
  {
    description: "Echo the message back",
    inputSchema: { message: z.string() },
  },
  async ({ message }) => ({ echoed: message })
);

await app.run();
```

## AppConfig

| 字段 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `id` | `string` | 必填 | 服务唯一标识 |
| `version` | `string` | `"0.1.0"` | 版本号，反映到 `/meta` |
| `auth` | `"none" \| "keystone_jwks" \| "static_bearer"` | `"none"` | 鉴权模式 |
| `manifestPath` | `string` | `"manifest.yaml"` | manifest 路径（fallback 源）|
| `port` | `number` | env `PORT` 或 `8080` | 监听端口 |
| `host` | `string` | `"0.0.0.0"` | 监听地址 |
| `mcpPoolSize` | `number` | `5` | MCP server pool 大小 |
| `logger` | `Logger` | 内置 JSON logger | 自定义 logger |

## 环境变量

| 变量 | 说明 |
|------|------|
| `KEYSTONE_JWKS_URL` | `auth=keystone_jwks` 时必填 |
| `KS_APP_AUTH_MODE` | 覆盖 auth mode；特殊值 `insecure` 降级为 none |
| `PORT` | 默认端口（被 AppConfig.port 覆盖）|
| `KS_GATEWAY_URL` | Keystone relay 网关地址，默认 `http://localhost:9988` |
| `KS_RELAY_URL` / `KS_RELAY_TOKEN` | LLM client 用 |
| `KEYSTONE_RELAY_TOKEN` | `KS_RELAY_TOKEN` 的兼容别名 |
| `KS_EMBEDDING_MODEL` | 托管 embedding 模型 ID，由 Keystone 注入 |
| `KS_EMBEDDING_DIM` | 托管 embedding 向量维度，由 Keystone 注入 |

## Strict-by-default

`auth: "keystone_jwks"` 且 `KEYSTONE_JWKS_URL` 为空且 `KS_APP_AUTH_MODE !== "insecure"` → 启动抛错。
本地开发用 `KS_APP_AUTH_MODE=insecure` 降级。

## App 方法

```typescript
app.tool(name, { description, inputSchema }, handler)   // 注册 MCP 工具
app.handle(method, path, handler)                       // 自定义 HTTP 路由
app.use(middleware)                                     // Hono middleware
app.healthCheck(name, fn)                               // 自定义 health check
app.onStartup(fn)                                       // 启动钩子
app.onShutdown(fn)                                      // 关闭钩子
app.llm()                                               // LLM Relay client
app.run()                                               // blocking 启动

// v0.2.0 declare 风格 meta 字段（对齐 ks-types v0.5.0）
app.declareNav({ label, category, open_mode, ... })     // 后台菜单导航项
app.declarePermission({ code, label, default_roles })   // 权限码目录条目（可多次调用累加）
app.setConfigMode("schema" | "iframe" | "none")         // 配置模式分类
app.setProtocolVersion("1.0")                           // MCP 协议版本（SemVer 'MAJOR.MINOR'）
app.setConfigStatus("unconfigured" | "via_frontend" | "via_cli" | "mixed")
app.embedding          // Keystone 托管 embedding client
app.vectorStore(name)  // Keystone 托管向量集合 client

// Escape hatches（承诺稳定）
app.mcpServer          // 官方 MCP SDK 实例
app.fetch              // Hono fetch handler
```

### 托管 Embedding 与 Vector Store

manifest 声明 `platform_services.embedding.required: true` 和 `managed_resources.vector_store` 后，
Keystone 会注入 relay token、embedding model/dim，并通过 `/v1/mcp/relay/*` 代理 embedding 与向量集合操作（`app.embedding` / `app.vectorStore(name)` / `searchText`）。

embed / upsert / `searchText`（走托管 dense+sparse RRF hybrid，选项只有 `{ topK, filter }`）的完整三语言示例见 [SDK API 参考 ·「Embedding 与 Vector Store」](../../docs/sdk-api-reference.md)。

### declare 示例

```typescript
const app = createApp({ id: "image-gen", version: "1.0.0", auth: "keystone_jwks" });

app.declareNav({
  label: "文生图",
  category: "应用",
  open_mode: "fullpage",
  icon: "image",
  required_perms: ["mcp.image-gen.use"],
});
app.declarePermission({ code: "mcp.image-gen.use", label: "使用文生图", default_roles: ["admin"] });
app.setConfigMode("iframe");
app.setProtocolVersion("1.0");
```

## 运行时支持

- **Bun** ≥ 1.2（一等公民，内置 `bun build --compile`）
- **Node** ≥ 20（通过 `@hono/node-server`）

## 路由

SDK 自动注册：`GET /healthz` · `GET /readyz` · `GET /meta` · `POST /mcp`

## 与 ks-types 的关系

手写 TS 镜像（`@wuhanyuhan/ks-app/types` subpath），仅覆盖 SDK 运行时需要的：
`AuthMode`、`MetaResponse`、`ToolInfo`、`ConfigUIInfo`、`MetaNavDecl`、`MetaPermissionDecl`、
`MetaConfigMode`、`MetaConfigStatus`。当前对齐 `ks-types` v0.5.0。Wire-level 漂移由 conformance 兜底。

## Conformance

SDK 通过 `ks-devkit/conformance/auth/` v1.0.0 的 22/22 case。
claimant 实现见 `sdk/typescript/conformance-claimant/`。

## License

MIT
