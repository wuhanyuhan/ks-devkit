"""capability_writer_demo —— ks-app-sdk v0.6.0 capability mesh 端到端示例。

跑示例:
    cd examples/capability_writer_demo
    export KS_APP_TOKEN=fake-token       # caller-side 才需要
    export KS_GATEWAY_URL=http://localhost:8080
    python main.py
"""
import asyncio
from pathlib import Path

from ks_app import App, CapabilityContext

HERE = Path(__file__).parent

app = App(
    id="ks-mcp-writer-demo",
    version="0.1.0",
    manifest_path=str(HERE / "manifest.yaml"),
)


@app.capability("list_articles")
async def list_articles(ctx: CapabilityContext, args: dict) -> dict:
    """mcp_tool backend：sync 列表查询。

    dispatcher 通过 MCP tools/call 调过来；ctx.user_id 从 _meta.ks_user_id 还原。
    """
    page = int(args.get("page", 1))
    return {
        "page": page,
        "items": [
            {"id": i, "title": f"示例文章 #{i}", "owner": ctx.user_id}
            for i in range((page - 1) * 5, page * 5)
        ],
    }


@app.capability("create_article")
async def create_article(ctx: CapabilityContext, args: dict) -> dict:
    """http_endpoint backend：long_running，演示 progress 上报 + caller-side。

    dispatcher 直接 HTTP POST /capabilities/create_article；
    scoped JWT aud == canonical_name；ctx 字段从 scoped claims 还原。
    """
    topic = args.get("topic", "AI")
    generate_cover = args.get("generate_cover", False)

    await ctx.progress("正在搜索热点...", percent=10)
    await asyncio.sleep(0.1)

    await ctx.progress("正在生成正文...", percent=50)
    body = f"关于 {topic} 的一篇示例文章（用户 {ctx.user_id} 触发）"

    cover_url = ""
    if generate_cover:
        await ctx.progress("正在生成封面...", percent=80)
        try:
            task = await app.call_capability("image-gen.generate").submit(
                prompt=topic, style="科技感",
            )
            result = await task.result(timeout=60)
            cover_url = result.get("image_url", "")
        except Exception:
            cover_url = ""

    await ctx.progress("装配中...", percent=95)

    return {
        "topic": topic,
        "body": body,
        "cover_url": cover_url,
        "owner": ctx.user_id,
        "chain_id": ctx.chain_id,
    }


if __name__ == "__main__":
    app.run()
