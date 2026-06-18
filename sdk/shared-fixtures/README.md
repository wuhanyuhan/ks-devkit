# 跨语言共享测试 Fixtures

三端 SDK（Go / Python / TypeScript）读取同一组 fixture 验证 relay 协议行为一致。

## sse/ 目录

每组 fixture 由两个文件组成：

- `NN-<name>.sse` —— 原始 SSE 字节流，模拟 keystone relay 端点的响应
- `NN-expected-chunks.json` —— 预期解析出的 Chunk 数组

## 现有 fixture

| 编号 | 主题 | 覆盖点 |
|---|---|---|
| 01 | 纯文本增量流 | 多个 delta_content + finish_reason=stop |
| 02 | 工具调用流 | tool_calls 增量累积 + finish_reason=tool_calls |
| 03 | 带 usage 的流 | 最后 chunk 的 usage 字段 |

## Chunk 字段约定（JSON 表示）

- `delta_content`: string（仅在有文本增量时存在）
- `finish_reason`: string（仅在 chunk 终止时存在）
- `tool_calls_delta`: array（仅在有工具调用增量时存在）
- `usage`: object `{prompt_tokens, completion_tokens, total_tokens}`（仅最后 chunk 可能有）

解析器在输出 Chunk 时，只序列化非零/非空字段。三端行为必须一致。

## Embedding / Vector Store fixture

根目录下的 JSON fixture 覆盖 Keystone 托管 embedding 与 vector store relay 端点：

| 文件 | 端点 |
|---|---|
| `embeddings_v1.json` | `POST /v1/mcp/relay/embeddings` |
| `vector_store_upsert.json` | `POST /v1/mcp/relay/vector_store/upsert`（命名 dense+sparse 点） |
| `vector_store_search_text.json` | `POST /v1/mcp/relay/vector_store/search_text`（服务端 embed → hybrid RRF，唯一检索路径） |
| `vector_store_delete.json` | `POST /v1/mcp/relay/vector_store/delete` |
| `vector_store_count.json` | `POST /v1/mcp/relay/vector_store/count` |

## Dispatcher / Capability fixture

| 文件 | 端点 / 覆盖点 |
|---|---|
| `dispatcher_invoke_on_behalf_of.json` | `POST /v1/apps/self/invoke`（on_behalf_of_user_id >0 守卫）|
| `dispatcher_invoke_with_chain.json` | `POST /v1/apps/self/invoke`（chain header + 全名 capability）|
| `canonical_derivation.json` | 去前缀 canonical 派生 `<app_id>.<name>` 两语言一致 |

## Readiness fixture

| 文件 | 端点 / 覆盖点 |
|---|---|
| `readiness.json` | `GET /ks-readiness` 响应 + `POST /ks-readiness/init` 请求体（就绪端点 wire 契约） |

## 使用方式

Go: `go:embed sdk/shared-fixtures/sse/*` 或测试时读取相对路径
Python: pytest fixture 用 `pathlib.Path(__file__).parent.parent.parent / "shared-fixtures"`
TS: `import.meta.dir` 拼路径
