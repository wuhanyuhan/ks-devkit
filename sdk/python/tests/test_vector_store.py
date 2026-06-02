import httpx
import pytest

from ks_app.embedding import EmbeddingClient
from ks_app.vector_store import VectorStoreClient


@pytest.mark.asyncio
async def test_vector_store_search_text(monkeypatch):
    monkeypatch.setenv("KS_GATEWAY_URL", "http://gw")
    monkeypatch.setenv("KS_RELAY_TOKEN", "tk")
    monkeypatch.setenv("KS_EMBEDDING_MODEL", "bge-m3")
    captured = {}

    async def handler(req: httpx.Request) -> httpx.Response:
        captured["path"] = req.url.path
        captured["body"] = req.read()
        return httpx.Response(200, json={
            "results": [{"id": "doc1", "score": 0.92, "payload": {"doc_id": "doc1"}}]
        })

    real_async_client = httpx.AsyncClient
    monkeypatch.setattr(httpx, "AsyncClient", lambda *a, **k: real_async_client(transport=httpx.MockTransport(handler)))
    store = VectorStoreClient(EmbeddingClient(), "documents")
    got = await store.search_text("hello", top_k=5)
    assert captured["path"] == "/v1/mcp/relay/vector_store/search_text"
    assert got[0].id == "doc1"
