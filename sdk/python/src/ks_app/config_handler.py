"""Config endpoint Starlette route factories。

镜像 Go sdk/go/ksapp/config_handler.go。4 个端点：

  - GET  /config-schema     → make_config_schema_route
  - GET  /config-pubkey     → make_config_pubkey_route
  - POST /ks-config/save    → make_config_save_route
  - POST /ks-config/validate → make_config_validate_route

由 App.create_app() 在检测到 Config handle 注册时自动挂载到 Starlette Route。

响应遵循 Keystone Result 形态：
    成功: {"code": 0, "message": "", "data": {...}}
    失败: {"code": "ERR_XXX", "message": "...", "data": null}

错误码：
    ERR_SCHEMA (400/422)     ERR_DECRYPT (400)     ERR_VALIDATE (422)
    ERR_STORE (500)          ERR_APPLY (500)       ERR_INTERNAL (500)
    ERR_NO_CONFIG_HANDLE (404) — SDK 侧新增，协议错误码表未列
"""
from __future__ import annotations

import base64
import json
from datetime import datetime, timezone
from typing import TYPE_CHECKING, Any, Awaitable, Callable

from cryptography.exceptions import InvalidTag
from starlette.requests import Request
from starlette.responses import JSONResponse, Response

from .crypto.aad import aad_canonical_bytes
from .crypto.aesgcm import decrypt_aes_gcm
from .crypto.x25519 import derive_kek, x25519_ecdh

if TYPE_CHECKING:
    from .app import App


# ---- Result helpers ----


def _result_ok(data: Any = None, message: str = "") -> dict[str, Any]:
    """Result 成功响应：{code:0, message, data}。"""
    return {"code": 0, "message": message, "data": data}


def _result_err(code: str, message: str, data: Any = None) -> dict[str, Any]:
    """Result 错误响应：{code:"ERR_XXX", message, data}。"""
    return {"code": code, "message": message, "data": data}


# ---- Route factories ----


def make_config_schema_route(app: "App") -> Callable[[Request], Awaitable[Response]]:
    """GET /config-schema handler。

      - 200 + data.{schema, ui_schema, version="1.0.0"}
      - 无 handle → 404 + ERR_NO_CONFIG_HANDLE

    version 字段语义：MCP 声明的 Schema 结构版本（非协议版本）。当 MCP
    升级 Schema 结构时应升版本。
    """

    async def handler(request: Request) -> Response:
        if not app._config_handles:
            return JSONResponse(
                _result_err(
                    "ERR_NO_CONFIG_HANDLE",
                    "当前 App 未注册任何 Config handle（调用 new_config() 注册）",
                ),
                status_code=404,
            )
        # MVP：单 Config handle；多 handle 场景留给未来支持
        h = app._config_handles[0]
        schema, ui_schema = h.schema_json()
        return JSONResponse(
            _result_ok({"schema": schema, "ui_schema": ui_schema, "version": "1.0.0"}),
            status_code=200,
        )

    return handler


def make_config_pubkey_route(app: "App") -> Callable[[Request], Awaitable[Response]]:
    """GET /config-pubkey handler。

      - 200 + data.{pubkey (base64-std), fingerprint, algorithm="x25519-ecdh-aes256gcm-v1",
                    created_at (RFC 3339 UTC)}

    keystore 加载失败属于 SDK programmer-error（部署错配），_get_or_load_keystore
    内部抛 RuntimeError；handler 层包装为 500 + ERR_INTERNAL 避免裸堆栈泄漏。
    """

    async def handler(request: Request) -> Response:
        try:
            ks = app._get_or_load_keystore()
        except Exception as e:
            return JSONResponse(
                _result_err("ERR_INTERNAL", f"keystore 加载失败: {e}"),
                status_code=500,
            )

        kp = ks.primary
        if kp is None:
            # 不应发生：keystore.load() 保证 primary 非 None
            return JSONResponse(
                _result_err("ERR_INTERNAL", "keystore primary 未加载"),
                status_code=500,
            )
        return JSONResponse(
            _result_ok(
                {
                    "pubkey": base64.b64encode(kp.pubkey).decode("ascii"),
                    "fingerprint": kp.fingerprint,
                    "algorithm": "x25519-ecdh-aes256gcm-v1",
                    "created_at": kp.created_at.astimezone(timezone.utc).strftime(
                        "%Y-%m-%dT%H:%M:%SZ"
                    ),
                }
            ),
            status_code=200,
        )

    return handler


def make_config_save_route(app: "App") -> Callable[[Request], Awaitable[Response]]:
    """POST /ks-config/save handler。

    流程（步骤 1-9）：
      1. decode request body JSON → 失败 → 400 + ERR_SCHEMA
      2. is_valid_idempotency_key → 不合法 → 400 + ERR_SCHEMA
      3. 无 handle → 404 + ERR_NO_CONFIG_HANDLE
      4. 幂等 LRU 命中 → 200 + 缓存 body
      5. decrypt_payload（AAD 重建对比 + X25519 + HKDF + AES-GCM）→ 失败 →
         400 + ERR_DECRYPT（AAD / fingerprint / GCM tag 不细分）
      6-9. handle.apply_save_from_bytes 映射错误码 → 成功时构造 response + 缓存。
    """

    async def handler(request: Request) -> Response:
        # 1. 解 request body JSON
        try:
            body = await request.json()
        except Exception as e:
            return JSONResponse(
                _result_err(
                    "ERR_SCHEMA", f"request body JSON 解析失败: {e}"
                ),
                status_code=400,
            )
        if not isinstance(body, dict):
            return JSONResponse(
                _result_err("ERR_SCHEMA", "request body 必须是 JSON object"),
                status_code=400,
            )

        # 2. idempotency_key 必填且必须合法 uuid-v4
        from .idempotency import is_valid_idempotency_key

        idem_key = body.get("idempotency_key", "")
        if not is_valid_idempotency_key(idem_key):
            return JSONResponse(
                _result_err(
                    "ERR_SCHEMA", "idempotency_key 不是合法 uuid-v4 格式"
                ),
                status_code=400,
            )

        # 3. 无 handle → 404
        if not app._config_handles:
            return JSONResponse(
                _result_err(
                    "ERR_NO_CONFIG_HANDLE",
                    "当前 App 未注册任何 Config handle（调用 new_config() 注册）",
                ),
                status_code=404,
            )
        handle = app._config_handles[0]

        # 4. 幂等 LRU 命中 → 直接返回缓存（per handle scope）
        lru = handle.ensure_idemp_lru()
        cached = lru.get(idem_key)
        if cached is not None:
            return Response(
                content=cached,
                status_code=200,
                media_type="application/json; charset=utf-8",
            )

        # 5. 解密（AAD 重建 + X25519 + HKDF + AES-GCM）
        try:
            plaintext = _decrypt_payload(app, body)
        except _DecryptError as e:
            # ERR_DECRYPT 覆盖 AAD 不匹配 + fingerprint 不匹配 + GCM tag 失败
            # 不细分以防 oracle 攻击
            return JSONResponse(
                _result_err("ERR_DECRYPT", str(e)),
                status_code=400,
            )

        # 6-9. handle.apply_save_from_bytes 走完 handleSave 全流程 + 错误码映射
        aad_fields = body.get("aad_fields", {}) or {}
        applied_ver, http_status, err_code, err_msg = await handle.apply_save_from_bytes(
            plaintext, aad_fields
        )
        if err_code:
            return JSONResponse(
                _result_err(err_code, err_msg),
                status_code=http_status,
            )

        # 成功：构造 response body 并缓存到 LRU（成功才缓存）
        resp = _result_ok(
            {
                "applied_at": datetime.now(timezone.utc).strftime(
                    "%Y-%m-%dT%H:%M:%SZ"
                ),
                "version": applied_ver,
            },
            message="配置已更新",
        )
        body_bytes = json.dumps(resp).encode("utf-8")
        lru.put(idem_key, body_bytes)
        return Response(
            content=body_bytes,
            status_code=200,
            media_type="application/json; charset=utf-8",
        )

    return handler


def make_config_validate_route(
    app: "App",
) -> Callable[[Request], Awaitable[Response]]:
    """POST /ks-config/validate handler。

    流程（仅走 AAD 对比 + X25519 + AES-GCM + Schema 反序列化 +
    on_validate）：
      1. decode payload → 失败 → 400 + ERR_SCHEMA
      2. 无 handle → 404 + ERR_NO_CONFIG_HANDLE
      3. decrypt_payload → ERR_DECRYPT / 400
      4. handle.validate_from_bytes → ERR_SCHEMA / ERR_VALIDATE / 422
      5. 成功 → 200 + Result{code:0, message:"连接正常"}

    不校验 idempotency_key（明确可选）。
    """

    async def handler(request: Request) -> Response:
        try:
            body = await request.json()
        except Exception as e:
            return JSONResponse(
                _result_err(
                    "ERR_SCHEMA", f"request body JSON 解析失败: {e}"
                ),
                status_code=400,
            )
        if not isinstance(body, dict):
            return JSONResponse(
                _result_err("ERR_SCHEMA", "request body 必须是 JSON object"),
                status_code=400,
            )

        # /validate 不校验 idempotency_key（明确可选）

        if not app._config_handles:
            return JSONResponse(
                _result_err(
                    "ERR_NO_CONFIG_HANDLE",
                    "当前 App 未注册任何 Config handle（调用 new_config() 注册）",
                ),
                status_code=404,
            )
        handle = app._config_handles[0]

        try:
            plaintext = _decrypt_payload(app, body)
        except _DecryptError as e:
            return JSONResponse(
                _result_err("ERR_DECRYPT", str(e)),
                status_code=400,
            )

        err_code, err_msg = await handle.validate_from_bytes(plaintext)
        if err_code:
            # ERR_SCHEMA（来自 plaintext JSON/pydantic 校验）+ ERR_VALIDATE（来自
            # on_validate）均映射 422。
            return JSONResponse(
                _result_err(err_code, err_msg),
                status_code=422,
            )

        return JSONResponse(
            _result_ok(message="连接正常"),
            status_code=200,
        )

    return handler


# ---- 解密 helper（9 步流程的 1-3 步）----


class _DecryptError(Exception):
    """内部解密失败标记；handler 层统一包为 ERR_DECRYPT。

    message 只含通用描述，禁止暴露 plaintext / privkey / dek 字节
    （安全规范）。
    """


def _decrypt_payload(app: "App", body: dict[str, Any]) -> bytes:
    """对齐 Go decryptPayload：解密流程步骤 1-4。

    步骤：
      1. 从 aad_fields 三字段 aad_canonical_bytes 重建 canonical；与
         body.aad_canonical base64 解码字节比对 → 不等抛 _DecryptError
      2. 按 aad_fields.fingerprint 选 primary / old 密钥对（轮换支持）
      3. X25519 + HKDF-SHA256 → kek
      4. AES-256-GCM.decrypt(kek, nonce, ciphertext, aad=want_aad)

    任一步失败抛 _DecryptError（message 不含敏感字节）。
    """
    aad_fields = body.get("aad_fields") or {}
    if not isinstance(aad_fields, dict):
        raise _DecryptError("aad_fields 缺失或类型错误")

    mcp_id = aad_fields.get("mcp_server_id", "")
    if not isinstance(mcp_id, str):
        raise _DecryptError("aad_fields.mcp_server_id 必须是字符串")
    # config_version JSON 反序列化后可能是 int 或 float（对齐 Go
    # `verFloat, _ := p.AADFields["config_version"].(float64)`）
    ver_raw = aad_fields.get("config_version", 0)
    if isinstance(ver_raw, bool) or not isinstance(ver_raw, (int, float)):
        raise _DecryptError("aad_fields.config_version 必须是数值")
    ver = int(ver_raw)
    fp = aad_fields.get("fingerprint", "")
    if not isinstance(fp, str):
        raise _DecryptError("aad_fields.fingerprint 必须是字符串")

    want_aad = aad_canonical_bytes(mcp_id, ver, fp)

    got_aad_b64 = body.get("aad_canonical", "")
    if not isinstance(got_aad_b64, str):
        raise _DecryptError("aad_canonical 必须是 base64 字符串")
    try:
        got_aad = base64.b64decode(got_aad_b64, validate=True)
    except Exception as e:
        raise _DecryptError(f"aad_canonical base64 解码失败: {e}") from e
    if want_aad != got_aad:
        raise _DecryptError("aad_canonical 与 aad_fields 重建的 canonical 字节不一致")

    # 2. 按 fingerprint 选 primary / old（轮换支持）
    try:
        ks = app._get_or_load_keystore()
    except Exception as e:
        raise _DecryptError(f"keystore 加载失败: {e}") from e

    kp = None
    if ks.primary is not None and ks.primary.fingerprint == fp:
        kp = ks.primary
    elif ks.old is not None and ks.old.fingerprint == fp:
        kp = ks.old
    if kp is None:
        raise _DecryptError(f"fingerprint {fp!r} 不匹配任何已加载的密钥")

    # 3. X25519 + HKDF → kek
    eph_pub_b64 = body.get("ephemeral_pubkey", "")
    if not isinstance(eph_pub_b64, str):
        raise _DecryptError("ephemeral_pubkey 必须是 base64 字符串")
    try:
        eph_pub = base64.b64decode(eph_pub_b64, validate=True)
    except Exception as e:
        raise _DecryptError(f"ephemeral_pubkey base64 解码失败: {e}") from e
    try:
        shared = x25519_ecdh(kp.privkey, eph_pub)
    except ValueError as e:
        raise _DecryptError(f"X25519 ECDH 失败: {e}") from e
    try:
        kek = derive_kek(shared)
    except ValueError as e:
        raise _DecryptError(f"HKDF 派生 KEK 失败: {e}") from e

    # 4. AES-GCM 解密
    nonce_b64 = body.get("nonce", "")
    ct_b64 = body.get("ciphertext", "")
    if not isinstance(nonce_b64, str) or not isinstance(ct_b64, str):
        raise _DecryptError("nonce / ciphertext 必须是 base64 字符串")
    try:
        nonce = base64.b64decode(nonce_b64, validate=True)
    except Exception as e:
        raise _DecryptError(f"nonce base64 解码失败: {e}") from e
    try:
        ct = base64.b64decode(ct_b64, validate=True)
    except Exception as e:
        raise _DecryptError(f"ciphertext base64 解码失败: {e}") from e
    try:
        return decrypt_aes_gcm(kek, nonce, ct, want_aad)
    except InvalidTag as e:
        # GCM tag 失败：不暴露 plaintext / kek 字节
        raise _DecryptError("AES-GCM 解密失败") from e
    except ValueError as e:
        raise _DecryptError(f"AES-GCM 参数错误: {e}") from e
