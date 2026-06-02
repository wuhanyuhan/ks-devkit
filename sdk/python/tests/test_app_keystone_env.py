"""App.__init__ 集成 _maybe_fetch_keystone_managed_env 的测试。

行为契约：
- KS_APP_TOKEN + KS_GATEWAY_URL 任一缺失 → 跳过，不调 SelfClient
- 都设了 → 调 SelfClient.fetch_env() → os.environ.setdefault 注入
- 已存在的 env 不被覆盖（本地手填优先）
- SelfClient 抛 KeystoneSelfFetchError → warn 但不 raise，App 正常构造
"""
from __future__ import annotations

import logging
import os

import pytest

import ks_app.app as app_mod
from ks_app import App
from ks_app.keystone_client import KeystoneSelfFetchError


class _FakeSelfClient:
    """记录调用 + 返回固定 env 的 SelfClient 替身。

    通过 .calls 暴露 __init__ 参数列表，便于断言 gateway/token 是否正确传入。
    """

    calls: list[tuple[str, str]] = []
    fetch_result: dict[str, str] = {}
    fetch_raises: Exception | None = None

    def __init__(self, gateway_url: str, app_token: str, timeout: float = 5.0) -> None:
        _FakeSelfClient.calls.append((gateway_url, app_token))

    def fetch_env(self) -> dict[str, str]:
        if _FakeSelfClient.fetch_raises is not None:
            raise _FakeSelfClient.fetch_raises
        return _FakeSelfClient.fetch_result


@pytest.fixture(autouse=True)
def _reset_fake(monkeypatch):
    """每个 test 重置 _FakeSelfClient 状态并替换 app 模块的 SelfClient 符号。"""
    _FakeSelfClient.calls = []
    _FakeSelfClient.fetch_result = {}
    _FakeSelfClient.fetch_raises = None
    monkeypatch.setattr(app_mod, "SelfClient", _FakeSelfClient)


@pytest.fixture(autouse=True)
def _clear_ks_env(monkeypatch):
    """每个 test 起步清掉 KS_APP_TOKEN / KS_GATEWAY_URL 以及测试用的注入键。"""
    for k in ("KS_APP_TOKEN", "KS_GATEWAY_URL", "DB_HOST", "DB_PASSWORD", "HMAC_SECRET"):
        monkeypatch.delenv(k, raising=False)


# ── 跳过路径 ────────────────────────────────────────────────────────


def test_skip_when_both_env_missing():
    """KS_APP_TOKEN + KS_GATEWAY_URL 都没设 → 不调 SelfClient。"""
    App("test")
    assert _FakeSelfClient.calls == []


def test_skip_when_only_token_set(monkeypatch):
    """只有 KS_APP_TOKEN，没 KS_GATEWAY_URL → 不调（按 spec：两个都要齐）。"""
    monkeypatch.setenv("KS_APP_TOKEN", "ks-app:1:1:1:abc")
    App("test")
    assert _FakeSelfClient.calls == []


def test_skip_when_only_gateway_set(monkeypatch):
    """只有 KS_GATEWAY_URL，没 KS_APP_TOKEN → 不调。"""
    monkeypatch.setenv("KS_GATEWAY_URL", "http://gw:9988")
    App("test")
    assert _FakeSelfClient.calls == []


def test_skip_when_token_is_empty_string(monkeypatch):
    """KS_APP_TOKEN 是空串视同未设。"""
    monkeypatch.setenv("KS_APP_TOKEN", "")
    monkeypatch.setenv("KS_GATEWAY_URL", "http://gw:9988")
    App("test")
    assert _FakeSelfClient.calls == []


# ── 注入路径 ────────────────────────────────────────────────────────


def test_fetches_and_injects_env(monkeypatch):
    """两个 env 都设 → 调 SelfClient → setdefault 注入到 os.environ。"""
    monkeypatch.setenv("KS_APP_TOKEN", "ks-app:42:1:1:abc")
    monkeypatch.setenv("KS_GATEWAY_URL", "http://gw:9988")
    _FakeSelfClient.fetch_result = {
        "DB_HOST": "keystone-mysql",
        "DB_PASSWORD": "secret",
    }

    App("test")

    assert _FakeSelfClient.calls == [("http://gw:9988", "ks-app:42:1:1:abc")]
    assert os.environ["DB_HOST"] == "keystone-mysql"
    assert os.environ["DB_PASSWORD"] == "secret"


def test_setdefault_does_not_overwrite_existing(monkeypatch):
    """已存在的 env 不被 SelfClient 拉回的值覆盖（本地手填优先）。"""
    monkeypatch.setenv("KS_APP_TOKEN", "tok")
    monkeypatch.setenv("KS_GATEWAY_URL", "http://gw:9988")
    monkeypatch.setenv("DB_HOST", "local-override")
    _FakeSelfClient.fetch_result = {
        "DB_HOST": "keystone-mysql",  # 应被忽略
        "DB_PASSWORD": "from-keystone",  # 应注入
    }

    App("test")

    assert os.environ["DB_HOST"] == "local-override"
    assert os.environ["DB_PASSWORD"] == "from-keystone"


# ── 失败路径 ────────────────────────────────────────────────────────


def test_fetch_failure_does_not_raise(monkeypatch, caplog):
    """SelfClient 抛 KeystoneSelfFetchError → App.__init__ 不 raise；warn 写日志。"""
    monkeypatch.setenv("KS_APP_TOKEN", "tok")
    monkeypatch.setenv("KS_GATEWAY_URL", "http://gw:9988")
    _FakeSelfClient.fetch_raises = KeystoneSelfFetchError("network down")

    with caplog.at_level(logging.WARNING, logger="ks_app.app"):
        App("test")  # 不应抛

    # 没注入 env（DB_HOST 仍未设）
    assert "DB_HOST" not in os.environ
    # warn 写到日志
    assert any("network down" in r.getMessage() or "fetch" in r.getMessage().lower()
               for r in caplog.records)


def test_fetch_unexpected_error_does_not_raise(monkeypatch):
    """SelfClient 抛任意 Exception（非 KeystoneSelfFetchError）也不应让 App 启动崩。

    理由：spec 关键决策"失败 warn 不 raise"——pydantic-settings 校验阶段抛错信
    息更具体，不要在 SDK init 阶段先炸。
    """
    monkeypatch.setenv("KS_APP_TOKEN", "tok")
    monkeypatch.setenv("KS_GATEWAY_URL", "http://gw:9988")
    _FakeSelfClient.fetch_raises = RuntimeError("totally unexpected")

    App("test")  # 不应抛
