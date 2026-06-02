# capability_writer_demo

最小 capability provider 示例：覆盖 `mcp_tool` 与 `http_endpoint` 两条 backend 路径。

## 文件

- `manifest.yaml` —— 声明 2 个 capability（list_articles 走 mcp_tool；create_article 走 http_endpoint）
- `main.py` —— SDK 注册 + handler 实现 + caller-side 调用 image-gen 的 demo

## 跑示例

````bash
cd ks-devkit/sdk/python
uv sync
cd examples/capability_writer_demo

# 配 caller-side env（callee-only 跑可跳过）
export KS_APP_TOKEN=...
export KS_GATEWAY_URL=http://localhost:8080

uv run python main.py
````

服务起在 `0.0.0.0:8000`（默认），dispatcher 走两条路径：

- `POST /mcp` (tools/call name=list_articles) —— mcp_tool backend
- `POST /capabilities/create_article` (Bearer scoped JWT) —— http_endpoint backend

## e2e 测试

跑 `uv run pytest tests/test_capability_writer_demo.py -v` 即可看到本示例的完整链路覆盖（含 mock scoped JWT 签名）。
