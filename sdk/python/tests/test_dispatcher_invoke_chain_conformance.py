"""跨语言 wire fixture：DispatcherClient.invoke 的 chain header + body。"""
import json
from pathlib import Path

import httpx
import pytest

from ks_app.keystone_client.dispatcher_client import DispatcherClient

SHARED = Path(__file__).parent.parent.parent / "shared-fixtures"


@pytest.mark.asyncio
async def test_dispatcher_invoke_with_chain_fixture(monkeypatch):
    fixture = json.loads((SHARED / "dispatcher_invoke_with_chain.json").read_text())
    captured: dict = {}

    async def handler(req: httpx.Request) -> httpx.Response:
        captured["method"] = req.method
        captured["path"] = req.url.path
        captured["body"] = json.loads(req.read())
        captured["headers"] = {k: req.headers.get(k) for k in fixture["request"]["headers"]}
        return httpx.Response(200, json=fixture["response"])

    real_async_client = httpx.AsyncClient
    monkeypatch.setattr(
        httpx, "AsyncClient",
        lambda *a, **k: real_async_client(transport=httpx.MockTransport(handler)),
    )

    body = fixture["request"]["body"]
    hdr = fixture["request"]["headers"]
    client = DispatcherClient(gateway_url="http://gw", app_token="tk")
    await client.invoke(
        capability=body["capability"], args=body["args"], mode=body["mode"],
        on_behalf_of_user_id=body["on_behalf_of_user_id"],
        chain_id=hdr["X-Keystone-Chain-Id"], chain_header=hdr["X-Keystone-Call-Chain"],
    )

    assert captured["method"] == fixture["request"]["method"]
    assert captured["path"] == fixture["request"]["path"]
    assert captured["body"] == fixture["request"]["body"]
    assert captured["headers"] == fixture["request"]["headers"]
