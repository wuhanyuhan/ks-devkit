"""A5 config_ui 形状归一测试（A6 对等矩阵 + shared-fixtures 测试见同文件后续追加）。"""
from ks_app.app import App


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
