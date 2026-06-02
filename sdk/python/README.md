# ks-app-sdk (Python)

Keystone 平台 MCP Service SDK（Python 版）。

## 安装

```bash
pip install ks-app-sdk>=0.9.0
```

## 快速上手

```python
from ks_app import App

app = App(
    "my-app",
    keystone_auth=True,           # 启用 JWKS strict-by-default
    version="0.1.0",
    manifest_path="manifest.yaml",
)


@app.tool("greet", "打招呼")
async def greet(name: str = "world"):
    return {"message": f"Hello, {name}!"}


# 可选：挂 /config-ui/ 静态前端
# app.mount_config_ui("./web/dist")

# 可选：扩展健康检查
# app.health_check("db", check_db_connection)

if __name__ == "__main__":
    app.run()
```

## 鉴权

启用 `keystone_auth=True` 后，对 `/mcp` 前缀的请求会强制 JWKS RS256 JWT 验签。

环境变量：

| 变量 | 说明 |
|------|------|
| `KEYSTONE_JWKS_URL` | 生产必须设置，指向 Keystone 的 `.well-known/jwks.json` |
| `KS_APP_AUTH_MODE=insecure` | 本地开发降级为裸跑（跳过所有鉴权） |
| `KS_APP_PORT` / `KS_APP_HOST` | 进程监听配置 |

**strict-by-default**：若 `keystone_auth=True` 且 `KEYSTONE_JWKS_URL` 未配置，
`app.create_app()` 会抛 `AuthResolveError`。生产意外漏配时立即暴露，不会静默裸跑。

在工具 handler 中读取 JWT claims：

```python
from ks_app import get_claims


@app.tool("whoami", "查当前用户")
async def whoami():
    claims = get_claims()
    return {"sub": claims.get("sub") if claims else None}
```

## 托管 Embedding 与 Vector Store

应用 manifest 声明 `platform_services.embedding.required: true` 和 `managed_resources.vector_store` 后，
Keystone 会注入 relay token、embedding model/dim，并通过 `/v1/mcp/relay/*` 代理 embedding 与向量集合操作（`app.embedding.embed` / `app.vector_store(name)` / `search_text`）。

embed / upsert / `search_text`（服务端 embed→dense+sparse RRF hybrid，向量检索唯一路径）的完整三语言示例见 [SDK API 参考 ·「Embedding 与 Vector Store」](../../docs/sdk-api-reference.md)。

## 调用其他应用的 Capability（caller-side）

应用可作为 caller 调用 Capability Mesh 中其他应用暴露的 capability（需 manifest 声明 `requires.capabilities[]`）。`on_behalf_of_user_id` 用于多跳调用链穿透发起人 user_id（仅 >0 时透传，与 Go SDK `CapabilityCall.WithOnBehalfOfUser` 对齐）。

`invoke` / `submit` / `on_behalf_of_user_id` 的完整用法见 [SDK API 参考 ·「Capability Mesh Caller」](../../docs/sdk-api-reference.md)。

## 运行时上下文（ToolContext）

`tools/call` 调用期间，Keystone 通过 MCP `_meta` 字段透传运行时元信息；
handler 通过 `get_context()` 非侵入式获取，函数签名无需变更。

| 字段 | 来源 _meta 键 | 用途 |
|------|--------------|------|
| `resource_scope` | `ks_resource_scope` | 多租户隔离作用域（dict 形式，业务侧 `json.loads` 还原） |
| `execution_id` | `ks_execution_id` | 当前执行 ID |
| `task_id` | `ks_task_id` | 当前任务 ID |
| `task_name` | `ks_task_name` | 当前任务名称 |
| `trigger_type` | `ks_trigger_type` | 触发类型（`manual` / `cron` / `webhook` / `event`） |
| `agent_id` | `ks_agent_id` | **v0.4.0 新增**：当前 keystone agent ID（审计） |
| `user_id` | `ks_user_id` | **v0.4.0 新增**：当前 keystone user ID（审计） |
| `request_id` | `ks_request_id` | **v0.4.0 新增**：当前 keystone 请求 ID（审计 / 链路追踪） |

未注入时所有字段返回空字符串（`""`）而非 `None`。

```python
from ks_app import get_context


@app.tool("read_doc", "读文档")
async def read_doc(path: str):
    ctx = get_context()
    # v0.4.0：写审计日志使用 agent_id / user_id / request_id
    audit_log.info(
        "read_doc",
        agent_id=ctx.agent_id,
        user_id=ctx.user_id,
        request_id=ctx.request_id,
        path=path,
    )
    return {"content": ...}
```

## 端点

| 路径 | 方法 | 说明 |
|------|------|------|
| `/healthz` | GET | 存活探针（聚合自定义 `health_check`） |
| `/readyz` | GET | 就绪探针 |
| `/meta` | GET | 应用元信息（对齐 `ks-types.MetaResponse`） |
| `/mcp` | POST | MCP Streamable HTTP，JSON-RPC 2.0 |
| `/mcp/tools/list` | GET | 工具列表（legacy） |
| `/mcp/tools/call` | POST | 调用工具（legacy） |
| `/config-ui/*` | GET | 前端静态资源（调用 `mount_config_ui` 后挂载） |

## 配置

| 环境变量 | 默认值 | 说明 |
|----------|--------|------|
| `KS_APP_PORT` | 8080 | 监听端口 |
| `KS_APP_HOST` | 0.0.0.0 | 监听地址 |
| `KEYSTONE_JWKS_URL` | — | JWKS 端点；`keystone_auth=True` 必配 |
| `KS_APP_AUTH_MODE` | — | `insecure` 时强制跳过鉴权 |
| `KS_GATEWAY_URL` | `http://localhost:9988` | Keystone relay 网关地址 |
| `KS_RELAY_TOKEN` / `KEYSTONE_RELAY_TOKEN` | — | Keystone relay token |
| `KS_EMBEDDING_MODEL` | — | 托管 embedding 模型 ID |
| `KS_EMBEDDING_DIM` | `0` | 托管 embedding 向量维度 |

## 依赖

- `uvicorn>=0.30`
- `starlette>=0.37`
- `pyyaml>=6.0`
- `PyJWT[crypto]>=2.8.0`
