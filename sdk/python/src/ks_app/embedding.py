from __future__ import annotations

import os
from dataclasses import dataclass, field

import httpx

from .llm import LLMRateLimitedError, LLMUnauthorizedError, LLMUpstreamError


class EmbeddingNotConfiguredError(RuntimeError):
    pass


@dataclass
class EmbeddingResult:
    dense: list[float]
    tokens: int
    # bge-m3 sparse 向量：token-id 串 → 权重；upsert 时可直接塞进 Point.sparse
    sparse: dict[str, float] = field(default_factory=dict)


class EmbeddingClient:
    def __init__(self) -> None:
        self._gateway_url = (os.environ.get("KS_GATEWAY_URL") or "http://localhost:9988").rstrip("/")
        self._relay_token = os.environ.get("KS_RELAY_TOKEN") or os.environ.get("KEYSTONE_RELAY_TOKEN") or ""
        self._model = os.environ.get("KS_EMBEDDING_MODEL") or ""
        self._dim = int(os.environ.get("KS_EMBEDDING_DIM") or "0")

    @property
    def model(self) -> str:
        return self._model

    @property
    def dim(self) -> int:
        return self._dim

    async def embed(self, text: str) -> EmbeddingResult:
        results = await self.embed_many([text])
        if not results:
            raise LLMUpstreamError("embedding response is empty")
        return results[0]

    async def embed_many(self, texts: list[str]) -> list[EmbeddingResult]:
        if not self._relay_token:
            raise EmbeddingNotConfiguredError("KS_RELAY_TOKEN 未设置")
        if not self._model:
            raise EmbeddingNotConfiguredError("KS_EMBEDDING_MODEL 未设置")
        body = {"model": self._model, "input": texts, "encoding_format": "dense+sparse"}
        data = await self._post_json("/v1/mcp/relay/embeddings", body)
        total_tokens = int((data.get("usage") or {}).get("total_tokens") or 0)
        tokens = total_tokens // len(texts) if texts else 0
        results: list[EmbeddingResult | None] = [None] * len(texts)
        for item in data.get("data") or []:
            idx = int(item.get("index"))
            if idx < 0 or idx >= len(texts):
                raise LLMUpstreamError(f"embedding index out of range: {idx}")
            sparse = {str(k): float(v) for k, v in (item.get("sparse_embedding") or {}).items()}
            results[idx] = EmbeddingResult(dense=list(item.get("embedding") or []), tokens=tokens, sparse=sparse)
        return [r or EmbeddingResult(dense=[], tokens=0) for r in results]

    async def _post_json(self, path: str, body: dict) -> dict:
        if not self._relay_token:
            raise EmbeddingNotConfiguredError("KS_RELAY_TOKEN 未设置")
        headers = {
            "Content-Type": "application/json",
            "Authorization": f"Bearer {self._relay_token}",
        }
        async with httpx.AsyncClient(timeout=60.0) as client:
            resp = await client.post(f"{self._gateway_url}{path}", json=body, headers=headers)
        if resp.status_code < 200 or resp.status_code >= 300:
            _raise_http_error(resp.status_code, resp.text)
        try:
            return resp.json()
        except ValueError as e:
            raise LLMUpstreamError(f"解析响应失败: {e}") from e


def _raise_http_error(status_code: int, body: str) -> None:
    msg = f"status={status_code} body={body[:500]}"
    if status_code in (401, 403):
        raise LLMUnauthorizedError(msg)
    if status_code == 429:
        raise LLMRateLimitedError(msg)
    raise LLMUpstreamError(msg)
