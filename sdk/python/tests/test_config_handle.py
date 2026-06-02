"""config_handle.py + new_config 测试（镜像 Go config_handle_test.go）。"""
from __future__ import annotations

import pytest
from pydantic import BaseModel, Field

from ks_app import App, ConfigSpec, new_config
from ks_app.ksconfig import reflect_config_schema


class SampleCfg(BaseModel):
    api_key: str = Field(..., title="API Key")


def test_new_config_registers():
    app = App("test-app-register")
    cfg = new_config(app, SampleCfg, ConfigSpec())
    assert cfg.get() is None
    # 内部记账字段（包私有 convention）：handle 已登记
    assert cfg in app._config_handles
    assert SampleCfg.__qualname__ in app._config_handle_types


def test_new_config_duplicate_raises():
    app = App("test-app-dup")
    new_config(app, SampleCfg, ConfigSpec())
    with pytest.raises(ValueError, match="已注册"):
        new_config(app, SampleCfg, ConfigSpec())


def test_new_config_nil_app_raises():
    with pytest.raises(TypeError, match="app"):
        new_config(None, SampleCfg, ConfigSpec())  # type: ignore[arg-type]


def test_new_config_not_basemodel_raises():
    class NotAModel:
        pass
    app = App("test-app-notmodel")
    with pytest.raises(TypeError):
        new_config(app, NotAModel, ConfigSpec())  # type: ignore[arg-type]


@pytest.mark.asyncio
async def test_handle_validate_calls_on_validate():
    app = App("test-app-validate")
    called = []

    async def on_validate(cfg: SampleCfg) -> None:
        called.append(cfg.api_key)

    cfg = new_config(app, SampleCfg, ConfigSpec(on_validate=on_validate))
    await cfg.handle_validate(SampleCfg(api_key="sk-xxx"))
    assert called == ["sk-xxx"]


@pytest.mark.asyncio
async def test_handle_validate_propagates_error():
    app = App("test-app-validate-fail")

    async def on_validate(cfg: SampleCfg) -> None:
        raise ValueError("validate failed")

    cfg = new_config(app, SampleCfg, ConfigSpec(on_validate=on_validate))
    with pytest.raises(ValueError, match="validate failed"):
        await cfg.handle_validate(SampleCfg(api_key="sk-bad"))


@pytest.mark.asyncio
async def test_validate_from_bytes_ok():
    app = App("test-app-vfb-ok")
    cfg = new_config(app, SampleCfg, ConfigSpec())
    code, msg = await cfg.validate_from_bytes(b'{"api_key": "sk-xxx"}')
    assert code == ""
    assert msg == ""


@pytest.mark.asyncio
async def test_validate_from_bytes_bad_json():
    app = App("test-app-vfb-bad")
    cfg = new_config(app, SampleCfg, ConfigSpec())
    code, msg = await cfg.validate_from_bytes(b"not-json{")
    assert code == "ERR_SCHEMA"
    assert msg  # 非空错误描述


@pytest.mark.asyncio
async def test_validate_from_bytes_on_validate_fails():
    app = App("test-app-vfb-vfail")

    async def on_validate(cfg: SampleCfg) -> None:
        raise ValueError("biz rule violated")

    cfg = new_config(app, SampleCfg, ConfigSpec(on_validate=on_validate))
    code, msg = await cfg.validate_from_bytes(b'{"api_key": "sk-xxx"}')
    assert code == "ERR_VALIDATE"
    assert "biz rule" in msg


@pytest.mark.asyncio
async def test_validate_from_bytes_schema_violation():
    """pydantic 校验失败（missing required field）也归 ERR_SCHEMA。"""
    app = App("test-app-vfb-schema")
    cfg = new_config(app, SampleCfg, ConfigSpec())
    code, msg = await cfg.validate_from_bytes(b'{}')  # 缺 api_key
    assert code == "ERR_SCHEMA"
    assert msg


def test_has_dek_false_before_bootstrap():
    app = App("test-app-dek-false")
    cfg = new_config(app, SampleCfg, ConfigSpec())
    assert cfg.has_dek() is False


def test_bootstrap_persistence_injects_fields():
    app = App("test-app-bootstrap")
    cfg = new_config(app, SampleCfg, ConfigSpec())
    dek = b"\x00" * 32
    cfg.bootstrap_persistence(
        persist_path="/tmp/mcp-config.enc",
        dek_path="/tmp/.local-dek",
        dek=dek,
    )
    assert cfg.has_dek() is True
    assert cfg._persist_path == "/tmp/mcp-config.enc"
    assert cfg._dek_path == "/tmp/.local-dek"
    assert cfg._dek == dek


def test_type_name_returns_qualname():
    app = App("test-app-typename")
    cfg = new_config(app, SampleCfg, ConfigSpec())
    assert cfg.type_name() == SampleCfg.__qualname__


def test_schema_json_matches_reflect():
    app = App("test-app-schemajson")
    cfg = new_config(app, SampleCfg, ConfigSpec())
    schema1, ui1 = cfg.schema_json()
    schema2, ui2 = reflect_config_schema(SampleCfg)
    assert schema1 == schema2
    assert ui1 == ui2


@pytest.mark.asyncio
async def test_handle_save_without_dek_raises():
    """对齐 Go handleSave dek==nil panic：Python 用 RuntimeError fail-fast。"""
    app = App("test-app-save-nodek")
    cfg = new_config(app, SampleCfg, ConfigSpec())
    with pytest.raises(RuntimeError, match="dek"):
        await cfg.handle_save(SampleCfg(api_key="sk-xxx"))


@pytest.mark.asyncio
async def test_apply_save_from_bytes_bad_json():
    """JSON 反序列化失败 → (0, 422, ERR_SCHEMA, msg)。"""
    app = App("test-app-asfb-bad")
    cfg = new_config(app, SampleCfg, ConfigSpec())
    # 不注入 dek，走前置反序列化失败分支
    ver, status, code, msg = await cfg.apply_save_from_bytes(
        b"not-json{", aad_fields={"config_version": 1.0}
    )
    assert ver == 0
    assert status == 422
    assert code == "ERR_SCHEMA"
    assert msg


@pytest.mark.asyncio
async def test_apply_save_from_bytes_on_validate_fails():
    """on_validate 抛异常 → (0, 422, ERR_VALIDATE, msg) —— 而非 ERR_INTERNAL。

    覆盖 Spec 契约：同一业务异常在 /validate 和 /save 路径必须归同一错误码。
    """
    app = App("test-app-asfb-vfail")

    async def on_validate(cfg: SampleCfg) -> None:
        raise ValueError("biz rule violated")

    cfg = new_config(app, SampleCfg, ConfigSpec(on_validate=on_validate))
    # 注入假 dek 让 handle_save 通过前置 fail-fast；on_validate 阶段就会抛错，
    # 不会触发 _persist_encrypted，无需 stub keystore。
    cfg.bootstrap_persistence(persist_path="/tmp/unused.enc", dek_path="/tmp/unused.dek", dek=b"\x00" * 32)

    ver, status, code, msg = await cfg.apply_save_from_bytes(
        b'{"api_key": "sk-xxx"}', aad_fields={"config_version": 1}
    )
    assert ver == 0
    assert status == 422
    assert code == "ERR_VALIDATE"
    assert "biz rule" in msg


@pytest.mark.asyncio
async def test_apply_save_from_bytes_on_apply_fails():
    """on_apply 抛异常 → (0, 500, ERR_APPLY, msg)。

    需要先走通 _persist_encrypted；用 monkeypatch 把实例方法
    stub 成 no-op（_rollback_persisted 同样 stub 避免回滚时 late import ImportError）。
    """
    app = App("test-app-asfb-afail")

    async def on_apply(cfg: SampleCfg) -> None:
        raise ValueError("apply failed")

    cfg = new_config(app, SampleCfg, ConfigSpec(on_apply=on_apply))
    cfg.bootstrap_persistence(persist_path="/tmp/unused.enc", dek_path="/tmp/unused.dek", dek=b"\x00" * 32)

    # stub keystore 相关写盘 —— 让 handle_save 能走到 on_apply 阶段
    def _noop_persist(self, _new_cfg):  # type: ignore[no-untyped-def]
        return None

    def _noop_rollback(self, _old_cfg):  # type: ignore[no-untyped-def]
        return None

    cfg._persist_encrypted = _noop_persist.__get__(cfg, type(cfg))  # type: ignore[method-assign]
    cfg._rollback_persisted = _noop_rollback.__get__(cfg, type(cfg))  # type: ignore[method-assign]

    ver, status, code, msg = await cfg.apply_save_from_bytes(
        b'{"api_key": "sk-xxx"}', aad_fields={"config_version": 7}
    )
    assert ver == 0
    assert status == 500
    assert code == "ERR_APPLY"
    assert "apply failed" in msg
    # 回滚后内存快照应回到 None（old_cfg 初始就是 None）
    assert cfg.get() is None
