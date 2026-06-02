"""ksapp CLI 测试（镜像 Go cli_test.go）。

覆盖 config / pubkey 全子命令。用 monkeypatch.chdir(tmp_path) 隔离
副作用，用 KSAPP_MCP_PRIVKEY_B64="" 避免主机 env 污染测试。
"""
from __future__ import annotations

import io
import json

import pytest

from ks_app.cli import config_cmds, pubkey_cmds
from ks_app.cli.__main__ import build_parser, main
from ks_app.cli.config_cmds import (
    CONFIG_DEK_PATH,
    CONFIG_ENC_PATH,
    CONFIG_STATUS_PATH,
    STATUS_UNCONFIGURED,
    STATUS_VIA_CLI,
    do_config_set_from_file,
    do_config_set_kv,
    is_sensitive_key,
    load_current_config_map,
    render_config,
    tail_n,
    write_config_status,
)
from ks_app.cli.pubkey_cmds import render_keystore, render_rotate_result
from ks_app.keystore import (
    RotateOptions,
    RotateResult,
    load,
    prune_old,
    rotate,
)


# ---- 公共 fixture ----


@pytest.fixture
def workdir(tmp_path, monkeypatch):
    """切换到隔离的临时工作目录，并清理可能影响 keystore 加载的 env。

    等价 Go 的 chdirTempDir + t.Setenv(KSAPP_MCP_PRIVKEY_B64, "")。
    """
    monkeypatch.chdir(tmp_path)
    monkeypatch.setenv("KSAPP_MCP_PRIVKEY_B64", "")
    monkeypatch.setenv("KSAPP_MCP_PRIVKEY_OLD_B64", "")
    return tmp_path


# ========================================================================
# 可测 helper：config_cmds
# ========================================================================


def test_config_set_and_show(workdir):
    """set KV → 读回 + 状态文件写入 via_cli。"""
    do_config_set_kv("api_key", "sk-xxx")

    cfg = load_current_config_map()
    assert cfg is not None
    assert cfg["api_key"] == "sk-xxx"

    status_path = workdir / "config" / ".status"
    assert status_path.read_bytes() == STATUS_VIA_CLI.encode("utf-8")


def test_config_reset(workdir):
    """预置 enc → reset → 文件不在 + 状态 unconfigured。"""
    config_dir = workdir / "config"
    config_dir.mkdir(mode=0o700)
    (config_dir / "mcp-config.enc").write_bytes(b"x")

    # 直接调 helper（不走 cmd 入口避免 SystemExit）
    args = build_parser().parse_args(["config", "reset"])
    # cmd_config_reset 无任何异常路径 → 正常返回
    config_cmds.cmd_config_reset(args)

    assert not (config_dir / "mcp-config.enc").exists()
    status = (config_dir / ".status").read_bytes()
    assert status == STATUS_UNCONFIGURED.encode("utf-8")


def test_config_reset_not_exist_silent(workdir):
    """reset 在 enc 不存在时仍成功（I-2 镜像：幂等）。"""
    args = build_parser().parse_args(["config", "reset"])
    # 不应抛 SystemExit
    config_cmds.cmd_config_reset(args)

    status = (workdir / "config" / ".status").read_bytes()
    assert status == STATUS_UNCONFIGURED.encode("utf-8")


def test_config_set_from_json_file(workdir):
    """从 JSON 文件批量导入。"""
    src = workdir / "cfg.json"
    src.write_text(
        json.dumps({"api_key": "sk-json", "endpoint": "https://example.com"}),
        encoding="utf-8",
    )

    do_config_set_from_file(str(src))

    cfg = load_current_config_map()
    assert cfg["api_key"] == "sk-json"
    assert cfg["endpoint"] == "https://example.com"


def test_config_set_from_yaml_file(workdir):
    """从 YAML 文件批量导入。数字字段经 JSON roundtrip 后保持数值等价。"""
    src = workdir / "cfg.yaml"
    src.write_text(
        "api_key: sk-yaml\nendpoint: https://yaml.example\ntimeout_ms: 3000\n",
        encoding="utf-8",
    )

    do_config_set_from_file(str(src))

    cfg = load_current_config_map()
    assert cfg["api_key"] == "sk-yaml"
    assert cfg["endpoint"] == "https://yaml.example"
    # Python YAML 原生解析成 int；JSON roundtrip 保持 int
    assert cfg["timeout_ms"] == 3000


def test_config_set_from_file_bad_format(workdir):
    """YAML / JSON 解析错误应抛异常（CLI 入口会转 exit_err → sys.exit(1)）。"""
    src = workdir / "bad.json"
    src.write_text("{not valid json", encoding="utf-8")

    with pytest.raises(ValueError, match="JSON 解析失败"):
        do_config_set_from_file(str(src))


def test_config_set_from_yaml_bad_format(workdir):
    """YAML 解析错误单独覆盖（expand I-3 — yaml.YAMLError → ValueError 包装）。"""
    src = workdir / "bad.yaml"
    # YAML 不支持的结构（tab 开头的缩进）
    src.write_text("foo:\n\tbar: baz\n", encoding="utf-8")

    with pytest.raises(ValueError, match="YAML 解析失败"):
        do_config_set_from_file(str(src))


def test_render_config_sensitive_redaction():
    """key/secret/token/password/api 关键字的字段必须脱敏。"""
    cfg = {
        "api_key": "sk-1234567890",
        "secret": "super-secret-value",
        "token": "tk-abcdef",
        "password": "hunter2",
        "endpoint": "https://example.com",
    }
    buf = io.StringIO()
    render_config(buf, cfg)
    out = buf.getvalue()

    # 敏感字段原文不应出现
    for original in ("sk-1234567890", "super-secret-value", "tk-abcdef", "hunter2"):
        assert original not in out, f"敏感原文 {original!r} 泄露到输出: {out}"
    # 脱敏标记应出现
    for needle in ("api_key", "secret", "token", "password", "***", "已脱敏"):
        assert needle in out, f"输出应含 {needle!r}: {out}"
    # 非敏感字段原文应出现
    assert "https://example.com" in out


def test_render_config_sorted_keys():
    """输出行的 key 必须按字母序排列（I-5 镜像：跨语言 conformance 可对齐）。"""
    cfg = {"zebra": "z", "alpha": "a", "middle": "m"}
    buf = io.StringIO()
    render_config(buf, cfg)
    lines = [line for line in buf.getvalue().splitlines() if line.strip()]

    # 行首 key 按字母序
    keys_in_order = [line.split(":")[0].strip() for line in lines]
    assert keys_in_order == ["alpha", "middle", "zebra"]


def test_render_config_no_config():
    """cfg is None → '(未配置)'。"""
    buf = io.StringIO()
    render_config(buf, None)
    assert "(未配置)" in buf.getvalue()


def test_is_sensitive_key():
    """5 关键字 + 大小写混合命中；非敏感 key 不命中。"""
    cases = {
        "api_key": True,
        "API_KEY": True,
        "MySecret": True,
        "auth_token": True,
        "password": True,
        "api_endpoint": True,
        "endpoint": False,
        "timeout_ms": False,
        "name": False,
    }
    for k, want in cases.items():
        got = is_sensitive_key(k)
        assert got == want, f"is_sensitive_key({k!r}) = {got}, want {want}"


def test_tail_n():
    """末尾字符截取。"""
    cases = [
        ("sk-1234567890", 4, "7890"),
        ("abc", 4, "abc"),
        ("", 4, ""),
        ("hunter2", 4, "ter2"),
    ]
    for s, n, want in cases:
        got = tail_n(s, n)
        assert got == want, f"tail_n({s!r}, {n}) = {got!r}, want {want!r}"


def test_load_current_config_map_not_exist(workdir):
    """enc 文件不存在时返回 None（show / set 首次调用场景）。"""
    cfg = load_current_config_map()
    assert cfg is None


def test_write_config_status_creates_dir(workdir):
    """writeConfigStatus 自动创建 config 目录。"""
    assert not (workdir / "config").exists()

    write_config_status("test_status")

    data = (workdir / "config" / ".status").read_bytes()
    assert data == b"test_status"


# ========================================================================
# 可测 helper：pubkey_cmds
# ========================================================================


def test_pubkey_show_renders_keystore(workdir):
    """预置 fallback → render 输出含 source / fingerprint / pubkey。"""
    ks = load()
    assert ks.primary is not None

    buf = io.StringIO()
    render_keystore(buf, ks)
    out = buf.getvalue()

    assert "source:" in out
    assert "fingerprint:" in out
    assert "pubkey:" in out
    assert ks.primary.fingerprint in out


def test_pubkey_rotate_print_only(workdir):
    """print-only 不落盘；fallback 文件内容不变；输出含 KSAPP_MCP_PRIVKEY_B64=。"""
    ks_before = load()
    fp_before = ks_before.primary.fingerprint

    before_data = (workdir / "config" / ".mcp-key").read_bytes()

    r = rotate(RotateOptions(print_only=True))
    assert r.new_privkey_b64
    assert r.new_pubkey_b64
    assert r.fingerprint
    assert r.files_written == []
    assert r.fingerprint != fp_before, "新指纹不应与旧指纹相等"

    after_data = (workdir / "config" / ".mcp-key").read_bytes()
    assert before_data == after_data, "print-only 后 fallback 文件不应被改写"

    # 渲染
    buf = io.StringIO()
    render_rotate_result(buf, r, True)
    out = buf.getvalue()
    assert "KSAPP_MCP_PRIVKEY_B64=" in out
    assert r.fingerprint in out


def test_pubkey_rotate_file_mode(workdir):
    """文件模式：旧 primary 搬到 .old，新对写 primary，FilesWritten 2 项。"""
    ks1 = load()
    old_fp = ks1.primary.fingerprint

    r = rotate()
    assert len(r.files_written) == 2

    assert (workdir / "config" / ".mcp-key.old").exists()

    ks2 = load()
    assert ks2.primary.fingerprint != old_fp
    assert ks2.old is not None
    assert ks2.old.fingerprint == old_fp


def test_pubkey_prune_old(workdir):
    """预埋 .old → PruneOld → 不存在。"""
    config_dir = workdir / "config"
    config_dir.mkdir(mode=0o700)
    (config_dir / ".mcp-key.old").write_bytes(b"{}")

    prune_old("")

    assert not (config_dir / ".mcp-key.old").exists()


def test_render_rotate_result_file_mode():
    """文件模式输出字节级等价 Go：files_written 必须按 Go `%v` 格式化为 `[a b]`（空格分隔无引号）。"""
    r = RotateResult(
        new_privkey_b64="priv-b64-dummy",
        new_pubkey_b64="pub-b64-dummy",
        fingerprint="fp:sha256:dummy",
        files_written=["config/.mcp-key.old", "config/.mcp-key"],
    )
    buf = io.StringIO()
    render_rotate_result(buf, r, False)
    out = buf.getvalue()
    # 精确字节级断言：对齐 Go fmt.Sprintf("%v", []string{"config/.mcp-key.old", "config/.mcp-key"}) = "[config/.mcp-key.old config/.mcp-key]"
    assert "已写入: [config/.mcp-key.old config/.mcp-key]\n" in out
    # 负断言：不应是 Python str(list) 默认的 "['a', 'b']" 格式
    assert "['config/.mcp-key.old'" not in out


def test_render_rotate_result_file_mode_empty_slice():
    """空 files_written 应输出 `[]`（对齐 Go %v on empty slice）。"""
    r = RotateResult(
        new_privkey_b64="p",
        new_pubkey_b64="p",
        fingerprint="f",
        files_written=[],
    )
    buf = io.StringIO()
    render_rotate_result(buf, r, False)
    assert "已写入: []\n" in buf.getvalue()


def test_config_reset_os_remove_permission_error(workdir, monkeypatch, capsys):
    """configReset 遇到非 FileNotFoundError 的 OSError（如 PermissionError）应退出码 1（镜像 Go I-2 修复）。"""
    import os as _os
    from ks_app.cli import config_cmds as _cc

    (workdir / "config").mkdir(mode=0o700)
    (workdir / "config" / "mcp-config.enc").write_bytes(b"dummy")

    def fake_remove(path):
        raise PermissionError(13, "Permission denied", path)

    monkeypatch.setattr(_os, "remove", fake_remove)
    with pytest.raises(SystemExit) as exc:
        _cc.cmd_config_reset(None)
    assert exc.value.code == 1
    err = capsys.readouterr().err
    assert "删除加密配置失败" in err


# ========================================================================
# argparse 分派 + 入口
# ========================================================================


def test_config_cmd_dispatch_set_show_reset(workdir, capsys):
    """走 __main__.main 的 dispatch：set → show → reset。"""
    main(["config", "set", "--key=endpoint", "--value=https://dispatch.example"])
    cfg = load_current_config_map()
    assert cfg["endpoint"] == "https://dispatch.example"

    main(["config", "show"])
    captured = capsys.readouterr().out
    assert "endpoint" in captured
    assert "https://dispatch.example" in captured

    main(["config", "reset"])
    captured = capsys.readouterr().out
    assert "unconfigured" in captured


def test_config_show_no_config(workdir, capsys):
    """未配置时 CLI show 输出 '(未配置)'。"""
    main(["config", "show"])
    out = capsys.readouterr().out
    assert "(未配置)" in out


def test_config_show_with_config(workdir, capsys):
    """set → show 打印配置项。"""
    do_config_set_kv("endpoint", "https://show.example")
    main(["config", "show"])
    out = capsys.readouterr().out
    assert "endpoint" in out
    assert "https://show.example" in out


def test_config_set_file_flag(workdir, capsys):
    """走 cmd_config_set 的 --file 分支。"""
    src = workdir / "cfg.json"
    src.write_text(
        json.dumps({"endpoint": "https://file-flag.example"}),
        encoding="utf-8",
    )

    main(["config", "set", "--file", str(src)])
    out = capsys.readouterr().out
    assert "配置已从文件导入" in out

    cfg = load_current_config_map()
    assert cfg["endpoint"] == "https://file-flag.example"


def test_config_set_missing_key_value_exits_1(workdir, capsys):
    """config set 不带 --key/--value/--file → sys.exit(1)。"""
    with pytest.raises(SystemExit) as exc:
        main(["config", "set"])
    assert exc.value.code == 1
    err = capsys.readouterr().err
    assert "必填其一" in err


def test_pubkey_cmd_dispatch_show(workdir, capsys):
    """无参数走 pubkey show 分支。"""
    main(["pubkey"])
    out = capsys.readouterr().out
    assert "fingerprint:" in out


def test_pubkey_cmd_dispatch_rotate(workdir, capsys):
    """走 rotate --print-only（避免写文件副作用扩散）。"""
    main(["pubkey", "rotate", "--print-only"])
    out = capsys.readouterr().out
    assert "KSAPP_MCP_PRIVKEY_B64=" in out


def test_pubkey_cmd_dispatch_prune_old(workdir, capsys):
    """走 prune-old 子命令。"""
    config_dir = workdir / "config"
    config_dir.mkdir(mode=0o700)
    (config_dir / ".mcp-key.old").write_bytes(b"{}")

    main(["pubkey", "prune-old"])
    out = capsys.readouterr().out
    assert "已清除" in out
    assert not (config_dir / ".mcp-key.old").exists()


# ========================================================================
# argparse 退出码（与 Go 等价：未知子命令 / 缺 flag → exit 2）
# ========================================================================


def test_unknown_subcommand_exits_2(workdir, capsys):
    """未知子命令 → argparse 报错 → sys.exit(2)。"""
    with pytest.raises(SystemExit) as exc:
        main(["config", "unknown-subcmd"])
    assert exc.value.code == 2


def test_unknown_top_command_exits_2(workdir, capsys):
    """未知顶层命令 → argparse 报错 → sys.exit(2)。"""
    with pytest.raises(SystemExit) as exc:
        main(["totally-unknown"])
    assert exc.value.code == 2


def test_config_set_invalid_flag_exits_2(workdir, capsys):
    """argparse 无法识别的 flag → sys.exit(2)。"""
    with pytest.raises(SystemExit) as exc:
        main(["config", "set", "--unknown=x"])
    assert exc.value.code == 2


# ========================================================================
# 跨语言文件格式自证据（Python CLI 写 → Python keystore 读回）
# 真正的 Go↔Python 互通由 conformance 套件覆盖
# ========================================================================


def test_cross_language_file_format_roundtrip(workdir):
    """Python CLI set → 直接用 Python keystore 解密 → json 还原。

    这是"三语言互通"在 Python 侧的自证据：只要文件布局遵守约定，
    其它语言 SDK 读同一份 enc + dek 应该得到同样字典。
    """
    from ks_app.keystore import decrypt_config_from_file, load_or_generate_dek

    do_config_set_kv("api_key", "sk-roundtrip-xxxx")
    do_config_set_kv("region", "cn-north-1")

    dek = load_or_generate_dek(CONFIG_DEK_PATH)
    data = decrypt_config_from_file(CONFIG_ENC_PATH, dek)
    obj = json.loads(data.decode("utf-8"))
    assert obj == {"api_key": "sk-roundtrip-xxxx", "region": "cn-north-1"}


# ========================================================================
# 字节级等价性 smoke（Python f"{:<20}" 对 ASCII key 与 Go %-20s 一致）
# ========================================================================


def test_render_config_byte_equivalent_ascii_padding():
    """ASCII key 下 Python 的 f"{k:<20}: {v}" 与 Go fmt.Sprintf("%-20s: %v", k, v)
    产物字节等价。用固定样本对比预期字符串。
    """
    cfg = {"api_key": "sk-abc1234", "region": "cn"}
    buf = io.StringIO()
    render_config(buf, cfg)
    out = buf.getvalue()

    # 预期字节序列（硬编码自 Go 跑同样输入的输出；Python 必须严格等价）
    # - api_key 脱敏为末 4 字符 "1234"
    # - region 原文展示
    # - key 按字母序：api_key 在前，region 在后
    expected = (
        "api_key             : ***1234（已脱敏）\n"
        "region              : cn\n"
    )
    assert out == expected, f"字节级不等价:\nwant={expected!r}\n got={out!r}"
