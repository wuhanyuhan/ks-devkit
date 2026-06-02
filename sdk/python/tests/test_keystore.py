"""keystore 模块测试。

覆盖：
  - Load 类：env / secret-file / fallback-file 三来源 + 优先级 + 自愈
  - Rotate 类：print_only 模式 + 文件模式
  - DEK 类：load_or_generate_dek + encrypt/decrypt_config_to/from_file

每个测试用 pytest tmp_path fixture 保证独立目录，env 变量通过
monkeypatch fixture 隔离。
"""
from __future__ import annotations

import base64
import json
import os
from pathlib import Path

import pytest

from ks_app.crypto import fingerprint, generate_x25519
from ks_app.keystore import (
    CONFIG_FILE_MIN_SIZE,
    CONFIG_FILE_VERSION,
    DEK_LEN,
    ENV_PRIVKEY_B64,
    ENV_PRIVKEY_FILE,
    ENV_PRIVKEY_OLD_B64,
    ENV_PRIVKEY_OLD_FILE,
    LoadOptions,
    RotateOptions,
    Source,
    decrypt_config_from_file,
    encrypt_config_to_file,
    load,
    load_or_generate_dek,
    rotate,
)


# ---- Fixtures ----


@pytest.fixture(autouse=True)
def _clear_env(monkeypatch):
    """每个测试前清理 KSAPP_MCP_* env 变量，避免串台。"""
    for k in (
        ENV_PRIVKEY_B64,
        ENV_PRIVKEY_OLD_B64,
        ENV_PRIVKEY_FILE,
        ENV_PRIVKEY_OLD_FILE,
    ):
        monkeypatch.delenv(k, raising=False)


def _make_opts(tmp_path: Path) -> LoadOptions:
    """在 tmp_path 下构造一组完全隔离的 LoadOptions。"""
    return LoadOptions(
        secret_file=str(tmp_path / "secret" / "mcp-key"),
        secret_file_old=str(tmp_path / "secret" / "mcp-key-old"),
        fallback_file=str(tmp_path / "fallback" / ".mcp-key"),
        fallback_old=str(tmp_path / "fallback" / ".mcp-key.old"),
    )


# ---- Load 类（7 条）----


def test_load_from_env_primary(monkeypatch, tmp_path):
    """KSAPP_MCP_PRIVKEY_B64 设置 → load 返 Source.ENV。"""
    priv, _ = generate_x25519()
    monkeypatch.setenv(ENV_PRIVKEY_B64, base64.b64encode(priv).decode("ascii"))
    ks = load(_make_opts(tmp_path))
    assert ks.source == Source.ENV
    assert ks.primary.privkey == priv
    assert ks.old is None
    assert len(ks.primary.pubkey) == 32
    # 指纹应该能自洽计算
    assert ks.primary.fingerprint == fingerprint(ks.primary.pubkey)


def test_load_from_env_with_old(monkeypatch, tmp_path):
    """env 里同时设 primary + old → 两个都有。"""
    priv1, _ = generate_x25519()
    priv2, _ = generate_x25519()
    monkeypatch.setenv(ENV_PRIVKEY_B64, base64.b64encode(priv1).decode("ascii"))
    monkeypatch.setenv(
        ENV_PRIVKEY_OLD_B64, base64.b64encode(priv2).decode("ascii")
    )
    ks = load(_make_opts(tmp_path))
    assert ks.source == Source.ENV
    assert ks.primary.privkey == priv1
    assert ks.old is not None
    assert ks.old.privkey == priv2


def test_load_from_secret_file(tmp_path):
    """把 base64 私钥写入 secret_file → load 返 Source.SECRET_FILE。"""
    priv, _ = generate_x25519()
    opts = _make_opts(tmp_path)
    Path(opts.secret_file).parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    Path(opts.secret_file).write_text(
        base64.b64encode(priv).decode("ascii") + "\n"
    )
    ks = load(opts)
    assert ks.source == Source.SECRET_FILE
    assert ks.primary.privkey == priv
    assert ks.old is None


def test_load_fallback_auto_generate(tmp_path):
    """空目录 → 首次 load 生成 .mcp-key 文件。"""
    opts = _make_opts(tmp_path)
    ks = load(opts)
    assert ks.source == Source.FALLBACK_FILE
    assert len(ks.primary.privkey) == 32
    assert len(ks.primary.pubkey) == 32
    # 文件已写
    assert Path(opts.fallback_file).exists()
    # 再次 load 应该复用已有文件（私钥一致）
    ks2 = load(opts)
    assert ks2.primary.privkey == ks.primary.privkey
    # fingerprint 内容与 JSON 对齐
    payload = json.loads(Path(opts.fallback_file).read_text())
    assert payload["version"] == 1
    assert payload["fingerprint"] == ks.primary.fingerprint


def test_load_priority_env_over_file(monkeypatch, tmp_path):
    """env 存在时忽略 secret / fallback 文件。"""
    # 在 secret_file / fallback_file 各写一把无关的密钥
    other_priv, _ = generate_x25519()
    opts = _make_opts(tmp_path)
    Path(opts.secret_file).parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    Path(opts.secret_file).write_text(
        base64.b64encode(other_priv).decode("ascii")
    )
    env_priv, _ = generate_x25519()
    monkeypatch.setenv(
        ENV_PRIVKEY_B64, base64.b64encode(env_priv).decode("ascii")
    )
    ks = load(opts)
    assert ks.source == Source.ENV
    assert ks.primary.privkey == env_priv
    assert ks.primary.privkey != other_priv


def test_load_invalid_length_b64_raises(monkeypatch, tmp_path):
    """env 里写 16 字节 base64 → ValueError（长度不符）。"""
    short = b"\x00" * 16  # 16 字节，不是 32
    monkeypatch.setenv(
        ENV_PRIVKEY_B64, base64.b64encode(short).decode("ascii")
    )
    with pytest.raises(ValueError, match="长度"):
        load(_make_opts(tmp_path))


def test_load_fallback_corrupted_self_heals(tmp_path):
    """故意损坏 .mcp-key → 备份 .corrupted 再生。"""
    opts = _make_opts(tmp_path)
    # 首次生成
    ks1 = load(opts)
    # 损坏
    Path(opts.fallback_file).write_text("NOT JSON AT ALL")
    # 再次 load 应该自愈生成新密钥对
    ks2 = load(opts)
    assert ks2.source == Source.FALLBACK_FILE
    assert ks2.primary.privkey != ks1.primary.privkey
    # .corrupted 备份存在
    assert Path(opts.fallback_file + ".corrupted").exists()


# ---- Rotate 类（2 条）----


def test_rotate_print_only(tmp_path):
    """print_only=True → 不写文件，返回 base64 + fingerprint。"""
    opts = RotateOptions(
        fallback_file=str(tmp_path / ".mcp-key"),
        fallback_old=str(tmp_path / ".mcp-key.old"),
        print_only=True,
    )
    res = rotate(opts)
    # base64 解码应为 32 字节
    assert len(base64.b64decode(res.new_privkey_b64)) == 32
    assert len(base64.b64decode(res.new_pubkey_b64)) == 32
    # fingerprint 与 pubkey 自洽
    pub = base64.b64decode(res.new_pubkey_b64)
    assert res.fingerprint == fingerprint(pub)
    # 不写文件
    assert res.files_written == []
    assert not Path(opts.fallback_file).exists()


def test_rotate_file_mode_moves_old_then_writes_new(tmp_path):
    """文件模式：primary 搬到 old，新密钥写 primary。"""
    # 先 load 一次产生 primary
    load_opts = _make_opts(tmp_path)
    ks1 = load(load_opts)
    original_priv = ks1.primary.privkey

    # rotate
    rot_opts = RotateOptions(
        fallback_file=load_opts.fallback_file,
        fallback_old=load_opts.fallback_old,
        print_only=False,
    )
    res = rotate(rot_opts)
    assert len(res.files_written) == 2
    # 两个文件都存在
    assert Path(load_opts.fallback_file).exists()
    assert Path(load_opts.fallback_old).exists()
    # primary 换新
    new_payload = json.loads(Path(load_opts.fallback_file).read_text())
    new_priv = base64.b64decode(new_payload["privkey"])
    assert new_priv != original_priv
    # old 是原来的 primary
    old_payload = json.loads(Path(load_opts.fallback_old).read_text())
    old_priv = base64.b64decode(old_payload["privkey"])
    assert old_priv == original_priv


# ---- DEK 类（6 条）----


def test_load_or_generate_dek_first_time(tmp_path):
    """空目录 → 生成并写 .local-dek。"""
    path = str(tmp_path / "subdir" / ".local-dek")
    dek = load_or_generate_dek(path)
    assert len(dek) == DEK_LEN == 32
    assert Path(path).exists()
    # 再次调用应读出相同 dek
    dek2 = load_or_generate_dek(path)
    assert dek == dek2


def test_load_or_generate_dek_existing(tmp_path):
    """预写 32 字节 → 原样读出。"""
    path = str(tmp_path / ".local-dek")
    original = os.urandom(32)
    Path(path).write_bytes(original)
    dek = load_or_generate_dek(path)
    assert dek == original


def test_load_or_generate_dek_invalid_length_raises(tmp_path):
    """预写 16 字节 → ValueError。"""
    path = str(tmp_path / ".local-dek")
    Path(path).write_bytes(b"\x00" * 16)
    with pytest.raises(ValueError, match="DEK 文件长度"):
        load_or_generate_dek(path)


def test_encrypt_decrypt_config_file_roundtrip(tmp_path):
    """encrypt_config_to_file + decrypt_config_from_file → 明文相等。"""
    dek = os.urandom(32)
    cfg_path = str(tmp_path / ".mcp-config.enc")
    plaintext = b'{"model": "gpt-4", "temperature": 0.7}'
    encrypt_config_to_file(cfg_path, dek, plaintext)
    # 文件格式合法
    data = Path(cfg_path).read_bytes()
    assert data[0] == CONFIG_FILE_VERSION
    assert len(data) >= CONFIG_FILE_MIN_SIZE
    # 解密
    got = decrypt_config_from_file(cfg_path, dek)
    assert got == plaintext


def test_decrypt_config_corrupted_backs_up(tmp_path):
    """预写破损文件（长度 < MIN_SIZE）→ decrypt 抛 + 生成 .corrupted。"""
    cfg_path = str(tmp_path / ".mcp-config.enc")
    # 只写 5 字节（小于 MIN_SIZE = 29）
    Path(cfg_path).write_bytes(b"\x01xxxx")
    with pytest.raises(ValueError, match="长度"):
        decrypt_config_from_file(cfg_path, os.urandom(32))
    # .corrupted 存在（原文件已被 rename）
    assert Path(cfg_path + ".corrupted").exists()
    assert not Path(cfg_path).exists()


def test_decrypt_config_wrong_version_no_backup(tmp_path):
    """version byte != 1 → 抛 + 不备份。"""
    cfg_path = str(tmp_path / ".mcp-config.enc")
    # 合法长度但 version 错误
    Path(cfg_path).write_bytes(b"\x99" + b"\x00" * (CONFIG_FILE_MIN_SIZE - 1))
    with pytest.raises(ValueError, match="version"):
        decrypt_config_from_file(cfg_path, os.urandom(32))
    # 文件仍在原位（未备份）
    assert Path(cfg_path).exists()
    assert not Path(cfg_path + ".corrupted").exists()
