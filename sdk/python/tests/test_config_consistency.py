"""A5 config_ui 形状归一 + A6 对等矩阵 + shared-fixtures 跨语言契约测试。"""
import json
import os

import pytest

from ks_app.app import App
from ks_app.config_consistency import check_nav_config_consistency

_FIXTURES = os.path.join(
    os.path.dirname(__file__), "..", "..", "shared-fixtures", "nav_config_consistency.json"
)


def test_resolve_config_ui_code_source_normalizes_to_enabled_url(tmp_path):
    """代码 mount_config_ui 来源 → {enabled,url}（A5），不再是 {path}。"""
    app = App("demo")
    ui_dir = tmp_path / "config-ui"
    ui_dir.mkdir()
    app.set_config_mode("iframe")
    app.mount_config_ui(str(ui_dir))
    cu = app._resolve_config_ui()
    assert cu == {"enabled": True, "url": "/config-ui/"}, cu
    assert "path" not in cu


def test_resolve_config_ui_none_when_no_source():
    app = App("demo")
    assert app._resolve_config_ui() is None


def test_matrix_matches_shared_fixtures():
    """Python 对等矩阵与 ks-types 权威（shared-fixtures）逐例一致。"""
    with open(_FIXTURES, encoding="utf-8") as f:
        cases = json.load(f)["cases"]
    assert len(cases) == 15
    for c in cases:
        reason, ok = check_nav_config_consistency(
            c["nav_state"], c["open_mode"], c["config_mode"], c["has_config_ui"]
        )
        assert ok == c["ok"], f'{c["name"]}: ok={ok} want {c["ok"]} (reason={reason})'
        assert (reason == "") == ok, f'{c["name"]}: reason/ok 不自洽'


def test_create_app_raises_on_inconsistent_combo():
    """A6：不一致组合启动（create_app）raise。"""
    app = App("demo")
    app.declare_nav(label="X", category="应用", open_mode="fullpage")
    app.set_config_mode("iframe")  # fullpage+iframe 非法
    with pytest.raises(RuntimeError, match="非法"):
        app.create_app()


def test_create_app_ok_on_legal_combo():
    app = App("demo")
    app.declare_nav(label="X", category="应用", open_mode="fullpage")
    app.set_config_mode("none")
    app.create_app()  # 不 raise
