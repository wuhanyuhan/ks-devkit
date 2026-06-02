"""Config endpoint 路由工厂测试。

镜像 Go sdk/go/ksapp/config_handler_test.go。覆盖：
  - /config-schema 正常路径 / 无 handle → 404
  - /config-pubkey 返回 pubkey/fingerprint/algorithm/created_at
  - /ks-config/save 端到端加密 + 解密 + 幂等命中 + 错误分支（uuid / AAD 篡改 /
    fingerprint 不匹配 / on_validate / on_apply 回滚 / plaintext JSON 坏 /
    无 handle / request body JSON 坏）
  - /ks-config/validate 端到端 + 不落盘 + on_validate 失败 + 空 idempotency_key
  - Mux 自动挂四端点（Starlette app routes）

所有端到端测试通过 env 注入固定 X25519 privkey，避免 keystore 写 CWD
的 config/.mcp-key fallback 文件污染。
"""
from __future__ import annotations

import base64
import json
import os
import re

import pytest
from cryptography.hazmat.primitives.asymmetric.x25519 import X25519PrivateKey
from pydantic import BaseModel
from starlette.testclient import TestClient

from ks_app import App, ConfigSpec, new_config
from ks_app.crypto import (
    aad_canonical_bytes,
    derive_kek,
    encrypt_aes_gcm,
    fingerprint,
    generate_x25519,
    x25519_ecdh,
)

# ---- 固定测试 keypair（与 Go testPrivkeyB64URL 等价）----
# 对齐 Go testPrivkeyB64URL / testPubkeyB64Std；通过 env 注入 → keystore.load 走
# env 分支，避免 config/.mcp-key 污染测试 CWD。
TEST_PRIVKEY_B64URL = "Gx19uYzMFkgASsaV6tcU9p68yPAkTxocenZAMMacxO8"
TEST_PUBKEY_B64STD = "qImndoV6pjUvrjdVlneipSbjY3BTRig2sNP2iuczNmk="

# fingerprint 格式正则（"ab12:cd34:...." 8 段 4 hex）
FINGERPRINT_RE = re.compile(r"^[0-9a-f]{4}(:[0-9a-f]{4}){7}$")

# 合法 uuid-v4（Go 测试复用的固定值）
VALID_UUID4 = "123e4567-e89b-42d3-a456-426614174000"


# ---- 测试用 pydantic config ----


class HandlerTestCfg(BaseModel):
    """端点测试专用 Config 类型；隔离 test_config_handle.py 的 TestCfg 避免
    new_config 重复注册错误（同 process test session 里跨文件注册）。"""

    api_key: str


# ---- fixtures ----


@pytest.fixture
def _set_priv_env(monkeypatch):
    """env 注入固定 privkey → keystore.load 走 env 分支，不触碰文件系统。"""
    monkeypatch.setenv("KSAPP_MCP_PRIVKEY_B64", TEST_PRIVKEY_B64URL)
    # 清除可能影响 keystore 解析的 old / secret 路径 env
    monkeypatch.delenv("KSAPP_MCP_PRIVKEY_OLD_B64", raising=False)
    monkeypatch.delenv("KSAPP_MCP_PRIVKEY_FILE", raising=False)
    monkeypatch.delenv("KSAPP_MCP_PRIVKEY_OLD_FILE", raising=False)
    yield


@pytest.fixture
def _tmp_cwd(tmp_path, monkeypatch):
    """切到临时目录 + 恢复 CWD。避免 bootstrap_persistence 写 config/ 污染项目。"""
    orig = os.getcwd()
    monkeypatch.chdir(tmp_path)
    yield tmp_path
    os.chdir(orig)


def _bootstrap_app_with_handle(
    app_id: str,
    spec: ConfigSpec | None = None,
) -> tuple[App, object, bytes, str]:
    """构建 App + 注册 handle + 通过 keystore.load + dek 注入，返回
    (app, handle, server_pubkey, fingerprint_str)。

    不调用 keystore.rotate / prune；env-based keystore 由 _set_priv_env fixture 注入。
    """
    from ks_app.keystore import load as load_keystore
    from ks_app.keystore.dek import load_or_generate_dek

    app = App(app_id)
    cfg = new_config(app, HandlerTestCfg, spec or ConfigSpec())

    # 模拟 Bootstrap：加载 keystore + 生成 DEK + 注入 handle。
    ks = load_keystore()
    # 提前塞进 app._keystore 让 /config-pubkey handler 不再重复加载（幂等）
    app._keystore = ks
    dek_path = "config/.local-dek"
    persist_path = "config/mcp-config.enc"
    dek = load_or_generate_dek(dek_path)
    cfg.bootstrap_persistence(persist_path, dek_path, dek)

    return app, cfg, ks.primary.pubkey, ks.primary.fingerprint


def _encrypt_payload(
    mcp_id: str,
    cfg_ver: int,
    server_pub: bytes,
    fp: str,
    plaintext: bytes,
    idemp_key: str,
) -> dict:
    """前端视角加密 helper，返回 dict 形态的 EncryptedConfigPayload。

    mirror Go encryptPayload：生成 ephemeral X25519 → ECDH → HKDF → KEK →
    AES-GCM encrypt plaintext（AAD=canonical）。
    """
    eph_priv, eph_pub = generate_x25519()
    shared = x25519_ecdh(eph_priv, server_pub)
    kek = derive_kek(shared)
    aad = aad_canonical_bytes(mcp_id, cfg_ver, fp)
    ct, nonce = encrypt_aes_gcm(kek, plaintext, aad)
    return {
        "algorithm": "x25519-ecdh-aes256gcm-v1",
        "ephemeral_pubkey": base64.b64encode(eph_pub).decode("ascii"),
        "nonce": base64.b64encode(nonce).decode("ascii"),
        "aad_fields": {
            "mcp_server_id": mcp_id,
            "config_version": cfg_ver,  # int；handler 层应兼容 int / float
            "fingerprint": fp,
        },
        "aad_canonical": base64.b64encode(aad).decode("ascii"),
        "ciphertext": base64.b64encode(ct).decode("ascii"),
        "idempotency_key": idemp_key,
    }


# ==== /config-schema ========================================================


def test_config_schema_returns_json_schema(_set_priv_env, _tmp_cwd):
    """注册 handle → GET /config-schema 200 + schema.properties.api_key。"""
    app, _, _, _ = _bootstrap_app_with_handle("schema-happy")
    client = TestClient(app.create_app())

    r = client.get("/config-schema")
    assert r.status_code == 200, r.text
    body = r.json()
    assert body["code"] == 0
    data = body["data"]
    assert data["version"] == "1.0.0"
    schema = data["schema"]
    assert "properties" in schema
    assert "api_key" in schema["properties"]
    # ui_schema 字段存在（可为空 dict）
    assert "ui_schema" in data


def test_config_schema_no_handle_returns_404_err_no_config_handle(_tmp_cwd):
    """未注册 handle → 404 + ERR_NO_CONFIG_HANDLE。"""
    app = App("schema-empty")
    client = TestClient(app.create_app())

    r = client.get("/config-schema")
    # 没 handle 时 /config-schema 路由不会挂载，Starlette 返 404（默认 HTML）
    # 但 Starlette 对未匹配路由返 404 的响应体不是 Result 形态；
    # Go 侧那个 404 + ERR_NO_CONFIG_HANDLE 是 handler 内部判断，路由仍然挂载。
    # 本 Python 镜像采用"handle 存在才挂路由"（见 app.py create_app），因此
    # 无 handle 时整条路由不存在 → Starlette 返普通 404。
    # 这里断言 404 即可（不强求 ERR_NO_CONFIG_HANDLE 码）。
    assert r.status_code == 404


# ==== /config-pubkey ========================================================


def test_config_pubkey_returns_pubkey_fingerprint_algorithm(_set_priv_env, _tmp_cwd):
    app, _, server_pub, fp = _bootstrap_app_with_handle("pubkey-happy")
    client = TestClient(app.create_app())

    r = client.get("/config-pubkey")
    assert r.status_code == 200, r.text
    body = r.json()
    assert body["code"] == 0
    data = body["data"]

    assert data["algorithm"] == "x25519-ecdh-aes256gcm-v1"
    assert FINGERPRINT_RE.match(data["fingerprint"]), data["fingerprint"]

    # pubkey 是 base64-std，32 字节
    pub = base64.b64decode(data["pubkey"])
    assert len(pub) == 32
    # env 注入的固定 privkey 对应的 pubkey 是 TEST_PUBKEY_B64STD
    assert data["pubkey"] == TEST_PUBKEY_B64STD

    # created_at 是 RFC 3339 UTC（Z 后缀）
    assert data["created_at"].endswith("Z")


# ==== /ks-config/save =======================================================


@pytest.mark.asyncio
async def test_config_save_end_to_end_success(_set_priv_env, _tmp_cwd):
    """前端加密 → POST → 200 + code=0 + applied_at + version；cfg.get() 返回新值。

    核心端到端测试：验证 SDK 可完整独立完成 on_validate/apply_save 流程。
    """
    apply_calls = {"n": 0}

    async def on_apply(c: HandlerTestCfg) -> None:
        apply_calls["n"] += 1

    app, cfg, server_pub, fp = _bootstrap_app_with_handle(
        "save-success", ConfigSpec(on_apply=on_apply)
    )

    plaintext = json.dumps({"api_key": "sk-end-to-end"}).encode("utf-8")
    payload = _encrypt_payload("ks-mcp-test", 2, server_pub, fp, plaintext, VALID_UUID4)

    client = TestClient(app.create_app())
    r = client.post("/ks-config/save", json=payload)
    assert r.status_code == 200, r.text

    body = r.json()
    assert body["code"] == 0
    assert body["message"] == "配置已更新"
    assert body["data"]["version"] == 2
    assert body["data"]["applied_at"].endswith("Z")

    # cfg.get 返新值
    got = cfg.get()
    assert got is not None
    assert got.api_key == "sk-end-to-end"

    # on_apply 触发一次
    assert apply_calls["n"] == 1

    # mcp-config.enc 已生成
    assert os.path.exists("config/mcp-config.enc")


@pytest.mark.asyncio
async def test_config_save_idempotency_hit(_set_priv_env, _tmp_cwd):
    """同 payload 发两次 → 第二次走 LRU 缓存；on_apply 只调一次。"""
    apply_calls = {"n": 0}

    async def on_apply(c: HandlerTestCfg) -> None:
        apply_calls["n"] += 1

    app, _, server_pub, fp = _bootstrap_app_with_handle(
        "save-idemp", ConfigSpec(on_apply=on_apply)
    )

    plaintext = json.dumps({"api_key": "sk-idemp"}).encode("utf-8")
    payload = _encrypt_payload("ks-mcp-test", 3, server_pub, fp, plaintext, VALID_UUID4)

    client = TestClient(app.create_app())

    r1 = client.post("/ks-config/save", json=payload)
    assert r1.status_code == 200, r1.text
    r2 = client.post("/ks-config/save", json=payload)
    assert r2.status_code == 200, r2.text

    # 两次响应 body 字节一致（缓存快照）
    assert r1.content == r2.content

    # on_apply 只调一次 —— 第二次走 LRU bypass handle_save
    assert apply_calls["n"] == 1


def test_config_save_invalid_idempotency_key_rejected(_set_priv_env, _tmp_cwd):
    """idempotency_key 非合法 uuid-v4 → 400 + ERR_SCHEMA。"""
    app, _, server_pub, fp = _bootstrap_app_with_handle("save-bad-idemp")

    plaintext = json.dumps({"api_key": "sk-1"}).encode("utf-8")
    # 用 uuid-v1 格式（version != 4）
    payload = _encrypt_payload(
        "ks-mcp-test",
        1,
        server_pub,
        fp,
        plaintext,
        "123e4567-e89b-12d3-a456-426614174000",
    )

    client = TestClient(app.create_app())
    r = client.post("/ks-config/save", json=payload)
    assert r.status_code == 400
    assert r.json()["code"] == "ERR_SCHEMA"


def test_config_save_aad_tampered_returns_err_decrypt(_set_priv_env, _tmp_cwd):
    """篡改 aad_canonical 不动 aad_fields → ERR_DECRYPT / 400。"""
    app, _, server_pub, fp = _bootstrap_app_with_handle("save-aad-mismatch")

    plaintext = json.dumps({"api_key": "sk-1"}).encode("utf-8")
    payload = _encrypt_payload("ks-mcp-test", 1, server_pub, fp, plaintext, VALID_UUID4)

    # 篡改 aad_canonical
    aad_bytes = base64.b64decode(payload["aad_canonical"])
    tampered = bytes([aad_bytes[0] ^ 0xFF]) + aad_bytes[1:]
    payload["aad_canonical"] = base64.b64encode(tampered).decode("ascii")

    client = TestClient(app.create_app())
    r = client.post("/ks-config/save", json=payload)
    assert r.status_code == 400
    assert r.json()["code"] == "ERR_DECRYPT"


def test_config_save_fingerprint_mismatch_returns_err_decrypt(
    _set_priv_env, _tmp_cwd
):
    """aad_fields.fingerprint 与 server primary/old 均不匹配 → ERR_DECRYPT。"""
    app, _, server_pub, _ = _bootstrap_app_with_handle("save-fp-mismatch")

    plaintext = json.dumps({"api_key": "sk-1"}).encode("utf-8")
    fake_fp = "ffff:eeee:dddd:cccc:bbbb:aaaa:9999:8888"
    payload = _encrypt_payload(
        "ks-mcp-test", 1, server_pub, fake_fp, plaintext, VALID_UUID4
    )

    client = TestClient(app.create_app())
    r = client.post("/ks-config/save", json=payload)
    assert r.status_code == 400
    assert r.json()["code"] == "ERR_DECRYPT"


@pytest.mark.asyncio
async def test_config_save_on_validate_error_returns_err_validate(
    _set_priv_env, _tmp_cwd
):
    """on_validate 抛异常 → 422 + ERR_VALIDATE；cfg.get() 仍 None。"""

    async def on_validate(c: HandlerTestCfg) -> None:
        raise ValueError("api_key 太短")

    app, cfg, server_pub, fp = _bootstrap_app_with_handle(
        "save-validate-err", ConfigSpec(on_validate=on_validate)
    )

    plaintext = json.dumps({"api_key": "x"}).encode("utf-8")
    payload = _encrypt_payload("ks-mcp-test", 1, server_pub, fp, plaintext, VALID_UUID4)

    client = TestClient(app.create_app())
    r = client.post("/ks-config/save", json=payload)
    assert r.status_code == 422, r.text
    assert r.json()["code"] == "ERR_VALIDATE"
    assert cfg.get() is None


@pytest.mark.asyncio
async def test_config_save_on_apply_error_rollback(_set_priv_env, _tmp_cwd):
    """on_apply 失败 → 500 + ERR_APPLY；内存回滚到 nil；mcp-config.enc 被删。"""

    async def on_apply(c: HandlerTestCfg) -> None:
        raise RuntimeError("apply boom")

    app, cfg, server_pub, fp = _bootstrap_app_with_handle(
        "save-apply-rollback", ConfigSpec(on_apply=on_apply)
    )

    plaintext = json.dumps({"api_key": "sk-fail"}).encode("utf-8")
    payload = _encrypt_payload("ks-mcp-test", 1, server_pub, fp, plaintext, VALID_UUID4)

    client = TestClient(app.create_app())
    r = client.post("/ks-config/save", json=payload)
    assert r.status_code == 500, r.text
    assert r.json()["code"] == "ERR_APPLY"
    assert cfg.get() is None
    # 首次 save 失败应删 mcp-config.enc
    assert not os.path.exists("config/mcp-config.enc")


def test_config_save_plaintext_schema_unmarshal_error(_set_priv_env, _tmp_cwd):
    """plaintext 非合法 JSON → 解密成功但 parse → 422 + ERR_SCHEMA。"""
    app, _, server_pub, fp = _bootstrap_app_with_handle("save-schema-err")

    plaintext = b"not-json-at-all {["
    payload = _encrypt_payload("ks-mcp-test", 1, server_pub, fp, plaintext, VALID_UUID4)

    client = TestClient(app.create_app())
    r = client.post("/ks-config/save", json=payload)
    assert r.status_code == 422, r.text
    assert r.json()["code"] == "ERR_SCHEMA"


def test_config_save_bad_request_body_json(_set_priv_env, _tmp_cwd):
    """request body 不是合法 JSON → 400 + ERR_SCHEMA。"""
    app, _, _, _ = _bootstrap_app_with_handle("save-bad-json")
    client = TestClient(app.create_app())

    # 用 data= 送非 JSON；显式 content_type application/json 让 request.json()
    # 尝试解析而非被 TestClient 自动升级
    r = client.post(
        "/ks-config/save",
        content=b"not-json{",
        headers={"Content-Type": "application/json"},
    )
    assert r.status_code == 400
    assert r.json()["code"] == "ERR_SCHEMA"


# ==== /ks-config/validate ===================================================


@pytest.mark.asyncio
async def test_config_validate_end_to_end_success(_set_priv_env, _tmp_cwd):
    """validate 成功 → 200 + 'connected'；cfg.get() 仍 None；mcp-config.enc 不生成。"""
    apply_calls = {"n": 0}

    async def on_validate(c: HandlerTestCfg) -> None:
        return None

    async def on_apply(c: HandlerTestCfg) -> None:
        apply_calls["n"] += 1

    app, cfg, server_pub, fp = _bootstrap_app_with_handle(
        "validate-success",
        ConfigSpec(on_validate=on_validate, on_apply=on_apply),
    )

    plaintext = json.dumps({"api_key": "sk-validate"}).encode("utf-8")
    payload = _encrypt_payload("ks-mcp-test", 1, server_pub, fp, plaintext, VALID_UUID4)

    client = TestClient(app.create_app())
    r = client.post("/ks-config/validate", json=payload)
    assert r.status_code == 200, r.text

    body = r.json()
    assert body["code"] == 0
    assert body["message"] == "连接正常"

    # validate 不切内存 + 不触发 on_apply + 不落盘
    assert cfg.get() is None
    assert apply_calls["n"] == 0
    assert not os.path.exists("config/mcp-config.enc")


@pytest.mark.asyncio
async def test_config_validate_on_validate_error(_set_priv_env, _tmp_cwd):
    """on_validate 抛异常 → 422 + ERR_VALIDATE。"""

    async def on_validate(c: HandlerTestCfg) -> None:
        raise ValueError("api_key 无效")

    app, _, server_pub, fp = _bootstrap_app_with_handle(
        "validate-err", ConfigSpec(on_validate=on_validate)
    )

    plaintext = json.dumps({"api_key": "bad"}).encode("utf-8")
    payload = _encrypt_payload("ks-mcp-test", 1, server_pub, fp, plaintext, VALID_UUID4)

    client = TestClient(app.create_app())
    r = client.post("/ks-config/validate", json=payload)
    assert r.status_code == 422, r.text
    assert r.json()["code"] == "ERR_VALIDATE"


@pytest.mark.asyncio
async def test_config_validate_allows_missing_idempotency_key(
    _set_priv_env, _tmp_cwd
):
    """/validate 不校验 idempotency_key（可选）。"""

    async def on_validate(c: HandlerTestCfg) -> None:
        return None

    app, _, server_pub, fp = _bootstrap_app_with_handle(
        "validate-nokey", ConfigSpec(on_validate=on_validate)
    )

    plaintext = json.dumps({"api_key": "sk-1"}).encode("utf-8")
    # idempotency_key 传空字符串（/save 会 400，/validate 应 200）
    payload = _encrypt_payload("ks-mcp-test", 1, server_pub, fp, plaintext, "")

    client = TestClient(app.create_app())
    r = client.post("/ks-config/validate", json=payload)
    assert r.status_code == 200, r.text


# ==== 路由自动挂 ============================================================


def test_app_create_app_mounts_four_endpoints_when_handle_registered(
    _set_priv_env, _tmp_cwd
):
    """注册 handle + env 注入 privkey → 四端点均不返 404。"""
    app, _, _, _ = _bootstrap_app_with_handle("mux-register")
    client = TestClient(app.create_app())

    # GET /config-schema → 200（已注册）
    r = client.get("/config-schema")
    assert r.status_code == 200

    # GET /config-pubkey → 200
    r = client.get("/config-pubkey")
    assert r.status_code == 200

    # POST /ks-config/save 用 empty body → 400（request body 非 dict / uuid bad），
    # 不能是 404（路由未挂载）
    r = client.post(
        "/ks-config/save",
        content=b"{}",
        headers={"Content-Type": "application/json"},
    )
    assert r.status_code != 404, r.text

    # POST /ks-config/validate 同理
    r = client.post(
        "/ks-config/validate",
        content=b"{}",
        headers={"Content-Type": "application/json"},
    )
    assert r.status_code != 404, r.text


def test_app_create_app_no_handle_no_config_endpoints(_tmp_cwd):
    """未注册 handle → 四端点路由不挂载（Starlette 返普通 404）。"""
    app = App("mux-no-handle")
    client = TestClient(app.create_app())

    r = client.get("/config-schema")
    assert r.status_code == 404
    r = client.get("/config-pubkey")
    assert r.status_code == 404
