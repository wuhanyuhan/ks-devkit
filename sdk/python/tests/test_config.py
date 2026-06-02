import pytest
from ks_app.config import load_config


def test_load_config_defaults(monkeypatch):
    monkeypatch.delenv("KS_APP_PORT", raising=False)
    monkeypatch.delenv("KS_APP_HOST", raising=False)
    cfg = load_config()
    assert cfg["port"] == 8080
    assert cfg["host"] == "0.0.0.0"


def test_load_config_from_env(monkeypatch):
    monkeypatch.setenv("KS_APP_PORT", "9090")
    monkeypatch.setenv("KS_APP_HOST", "127.0.0.1")
    cfg = load_config()
    assert cfg["port"] == 9090
    assert cfg["host"] == "127.0.0.1"


def test_load_config_invalid_port(monkeypatch):
    monkeypatch.setenv("KS_APP_PORT", "not-a-number")
    with pytest.raises(ValueError):
        load_config()
