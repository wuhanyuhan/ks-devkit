import json
from pathlib import Path

import httpx
import pytest

from ks_app.embedding import EmbeddingClient
from ks_app.vector_store import VectorStoreClient


SHARED_FIXTURES = Path(__file__).parent.parent.parent / "shared-fixtures"


def load_fixture(name: str) -> dict:
    return json.loads((SHARED_FIXTURES / name).read_text())


def patch_httpx(monkeypatch, fixture: dict, captured: dict) -> None:
    async def handler(req: httpx.Request) -> httpx.Response:
        captured["method"] = req.method
        captured["path"] = req.url.path
        captured["body"] = json.loads(req.read())
        return httpx.Response(200, json=fixture["response"])

    real_async_client = httpx.AsyncClient
    monkeypatch.setattr(
        httpx,
        "AsyncClient",
        lambda *a, **k: real_async_client(transport=httpx.MockTransport(handler)),
    )


@pytest.mark.asyncio
async def test_embedding_client_conformance_fixture(monkeypatch):
    fixture = load_fixture("embeddings_v1.json")
    captured = {}
    monkeypatch.setenv("KS_GATEWAY_URL", "http://gw")
    monkeypatch.setenv("KS_RELAY_TOKEN", "tk")
    monkeypatch.setenv("KS_EMBEDDING_MODEL", "bge-m3")
    patch_httpx(monkeypatch, fixture, captured)

    got = await EmbeddingClient().embed("hello")

    assert captured == fixture["request"]
    assert got.dense == [0.1, 0.2]
    assert got.tokens == 2
    assert got.sparse == {"100": 0.5, "7": 0.25}


@pytest.mark.asyncio
async def test_vector_store_client_conformance_fixture(monkeypatch):
    fixture = load_fixture("vector_store_search_text.json")
    captured = {}
    monkeypatch.setenv("KS_GATEWAY_URL", "http://gw")
    monkeypatch.setenv("KS_RELAY_TOKEN", "tk")
    monkeypatch.setenv("KS_EMBEDDING_MODEL", "bge-m3")
    patch_httpx(monkeypatch, fixture, captured)

    got = await VectorStoreClient(EmbeddingClient(), "documents").search_text("hello", top_k=5)

    assert captured == fixture["request"]
    assert len(got) == 1
    assert got[0].id == "doc1"
    assert got[0].payload == {"doc_id": "doc1"}


@pytest.mark.asyncio
async def test_llm_chat_intent_conformance_fixture(monkeypatch):
    from ks_app.llm import ChatRequest, LLMClient

    fixture = load_fixture("relay_chat_intent.json")
    captured = {}
    monkeypatch.setenv("KS_GATEWAY_URL", "http://gw")
    monkeypatch.setenv("KS_RELAY_TOKEN", "tk")
    patch_httpx(monkeypatch, fixture, captured)

    await LLMClient().chat(ChatRequest(
        messages=[{"role": "user", "content": "hi"}],
        tier="flagship",
        require_capabilities=["vision"],
        reasoning="on",
    ))

    assert captured["method"] == fixture["request"]["method"]
    assert captured["path"] == fixture["request"]["path"]
    assert captured["body"] == fixture["request"]["body"]
