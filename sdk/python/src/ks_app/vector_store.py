from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from .embedding import EmbeddingClient


Filter = dict[str, Any]


@dataclass
class Point:
    id: str
    dense: list[float]
    # bge-m3 sparse：token-id 串 → 权重；可直接取自 EmbeddingResult.sparse
    sparse: dict[str, float] | None = None
    payload: dict[str, Any] | None = None


@dataclass
class SearchResult:
    id: str
    score: float
    payload: dict[str, Any]


class VectorStoreClient:
    def __init__(self, embedding: EmbeddingClient, collection: str) -> None:
        self._embedding = embedding
        self._collection = collection

    async def upsert(self, points: list[Point]) -> None:
        await self._embedding._post_json(
            "/v1/mcp/relay/vector_store/upsert",
            {
                "collection": self._collection,
                "points": [
                    {
                        "id": p.id,
                        "dense": p.dense,
                        **({"sparse": p.sparse} if p.sparse else {}),
                        **({"payload": p.payload} if p.payload else {}),
                    }
                    for p in points
                ],
            },
        )

    async def search_text(self, text: str, *, top_k: int = 5, filter: Filter | None = None) -> list[SearchResult]:
        """服务端 embed dense+sparse 后做 RRF hybrid 检索（托管向量链唯一检索路径）。"""
        body: dict[str, Any] = {"collection": self._collection, "text": text, "top_k": top_k}
        if filter:
            body["filter"] = filter
        return await self._search("/v1/mcp/relay/vector_store/search_text", body)

    async def delete(self, ids: list[str]) -> None:
        await self._embedding._post_json(
            "/v1/mcp/relay/vector_store/delete",
            {"collection": self._collection, "ids": ids},
        )

    async def delete_by_filter(self, filter: Filter) -> None:
        await self._embedding._post_json(
            "/v1/mcp/relay/vector_store/delete",
            {"collection": self._collection, "filter": filter},
        )

    async def count(self, filter: Filter | None = None) -> int:
        body: dict[str, Any] = {"collection": self._collection}
        if filter:
            body["filter"] = filter
        data = await self._embedding._post_json("/v1/mcp/relay/vector_store/count", body)
        return int(data.get("count") or 0)

    async def _search(self, path: str, body: dict[str, Any]) -> list[SearchResult]:
        data = await self._embedding._post_json(path, body)
        return [
            SearchResult(id=str(item.get("id")), score=float(item.get("score") or 0), payload=item.get("payload") or {})
            for item in data.get("results") or []
        ]
