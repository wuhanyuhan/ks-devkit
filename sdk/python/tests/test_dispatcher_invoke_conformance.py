"""跨语言 wire fixture：DispatcherClient.invoke 的 on_behalf_of_user_id payload。"""
import json
from pathlib import Path

import httpx
import pytest

from ks_app.keystone_client.dispatcher_client import DispatcherClient

SHARED_FIXTURES = Path(__file__).parent.parent.parent / "shared-fixtures"


def load_fixture(name: str) -> dict:
    return json.loads((SHARED_FIXTURES / name).read_text())


@pytest.mark.asyncio
async def test_dispatcher_invoke_on_behalf_of_fixture(monkeypatch):
    fixture = load_fixture("dispatcher_invoke_on_behalf_of.json")
    captured: dict = {}

    async def handler(req: httpx.Request) -> httpx.Response:
        captured["method"] = req.method
        captured["path"] = req.url.path
        captured["body"] = json.loads(req.read())
        return httpx.Response(200, json=fixture["response"])

    real_async_client = httpx.AsyncClient
    monkeypatch.setattr(
        httpx, "AsyncClient",
        lambda *a, **k: real_async_client(transport=httpx.MockTransport(handler)),
    )

    body = fixture["request"]["body"]
    client = DispatcherClient(gateway_url="http://gw", app_token="tk")
    await client.invoke(
        capability=body["capability"], args=body["args"], mode=body["mode"],
        on_behalf_of_user_id=body["on_behalf_of_user_id"],
    )

    assert captured["method"] == fixture["request"]["method"]
    assert captured["path"] == fixture["request"]["path"]
    assert captured["body"] == fixture["request"]["body"]
