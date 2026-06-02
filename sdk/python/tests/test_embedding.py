import httpx
import pytest

from ks_app.embedding import EmbeddingClient


@pytest.mark.asyncio
async def test_embedding_client_posts_openai_request(monkeypatch):
    monkeypatch.setenv("KS_GATEWAY_URL", "http://gw")
    monkeypatch.setenv("KS_RELAY_TOKEN", "tk")
    monkeypatch.setenv("KS_EMBEDDING_MODEL", "bge-m3")
    monkeypatch.setenv("KS_EMBEDDING_DIM", "1024")
    captured = {}

    async def handler(req: httpx.Request) -> httpx.Response:
        captured["path"] = req.url.path
        captured["auth"] = req.headers.get("authorization")
        captured["body"] = req.read()
        return httpx.Response(200, json={
            "object": "list",
            "model": "bge-m3",
            "data": [{"object": "embedding", "index": 0, "embedding": [0.1, 0.2]}],
            "usage": {"prompt_tokens": 2, "total_tokens": 2},
        })

    real_async_client = httpx.AsyncClient
    monkeypatch.setattr(httpx, "AsyncClient", lambda *a, **k: real_async_client(transport=httpx.MockTransport(handler)))
    client = EmbeddingClient()
    got = await client.embed("hello")
    assert captured["path"] == "/v1/mcp/relay/embeddings"
    assert captured["auth"] == "Bearer tk"
    assert got.dense == [0.1, 0.2]
    assert got.tokens == 2
