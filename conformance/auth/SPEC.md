# MCP Service Auth Conformance

**Version:** v1.0.0 (同 `VERSION` 文件)

**Scope:** Service 端（被 Keystone 调用方）的 `/mcp` 鉴权与 `/healthz /readyz /meta` 端点行为契约。不覆盖签发端（Keystone → service）。

---

## Evolution Principles

> 以下三条原则不可变，修改本 SPEC 需同时更新所有 claimant：
>
> 1. **Contract before implementation** —— 契约文字先于代码
> 2. **Claimant claims version** —— 版本号由 SDK 自主承诺，不由 conformance 强加
> 3. **Three existing claimants pass before merge** —— 新 case 合并前所有现有 claimant 必须通过

## 1. Endpoints

每个 conforming service MUST 暴露以下端点：

| Method | Path | Auth | Success |
|--------|------|------|---------|
| GET | `/healthz` | 不鉴权 | 200, body `{"status":"ok"}` |
| GET | `/readyz` | 不鉴权 | 200, body `{"status":"ok"}` |
| GET | `/meta` | 不鉴权 | 200, body matching §4 MetaResponse schema |
| POST | `/mcp` | 按 `auth_mode` 执行 | JSON-RPC 2.0 response |

## 2. AuthMode 枚举

`auth_mode` 取值对齐 `kstypes.AuthMode`（见 ks-types v0.4.0+）：

| 值 | 语义 |
|----|------|
| `none` | /mcp 不做鉴权 |
| `keystone_jwks` | /mcp 要求 `Authorization: Bearer <RS256 JWT>`，验签方式见 §3 |
| `static_bearer` | /mcp 要求 `Authorization: Bearer <static-token>`（本 conformance v1.0.0 暂不覆盖） |

空字符串等价于 `none`。

## 3. JWT Validation Rules（`auth_mode=keystone_jwks` 时）

合规 service MUST：

- **MUST reject** algorithm 不是 `RS256` 的 JWT（包括 `none` / `HS256` 等）
- **MUST reject** 缺少 `kid` header 的 JWT
- **MUST fetch** JWKS endpoint 当本地缓存无对应 `kid`（至少一次即可）
- **MUST reject** 签名无法通过 JWKS 对应公钥验证的 JWT
- **MUST reject** `exp` 已过的 JWT
- **MUST reject** 缺少 `Authorization` header 的请求
- **MUST reject** `Authorization` 格式非 `Bearer <token>` 的请求

成功验签的请求：JWT 的 claims 可被 handler 层访问（具体机制由实现决定）。

## 4. Meta Response Schema

`GET /meta` 的 JSON 响应 MUST 符合以下结构（对齐 `kstypes.MetaResponse`）：

```json
{
  "name": "<string, non-empty>",
  "version": "<string, non-empty>",
  "auth_mode": "<string, 可选；值需在 §2 枚举内>",
  "config_ui": null,
  "tools": [
    {"name": "<string>", "description": "<string, 可选>"}
  ]
}
```

- `auth_mode` 字段值 MUST 等于该 service **实际生效**的模式（不是 manifest 原始声明）——例如 `KS_APP_AUTH_MODE=insecure` 导致降级为 `none` 时，`/meta` 返回 `none` 或省略该字段（等价）
- `tools` 数组列出所有 MCP 注册的工具
- `config_ui` 为 null 时 MUST 省略或显式 null（不得出现空对象 `{}`）

## 5. Error Response Shape

所有 401 响应 MUST 符合：

- `Content-Type: application/json`
- Body: `{"error": "<non-empty string>"}`

其他错误（400/500 等）的 body shape 不由本 conformance 约束。

## 6. MCP Streamable HTTP Protocol

`POST /mcp` 处理 JSON-RPC 2.0 消息。合规 service MUST 支持：

- `initialize` → 返回 `{"protocolVersion":"2025-03-26", ...}`
- `tools/list` → 返回 `{"tools":[...]}`，数组含所有注册工具
- `tools/call` → 按工具名分发并返回 `{"content":[...]}`
- 无 `id` 字段的通知（notification）→ 返回 `202 Accepted`（无 body）

## 7. Case Index

每条规则对应 `cases/` 下的哪个测试：

| § | 规则 | Case |
|---|------|------|
| §3 MUST reject alg != RS256 | `04_alg_none_attack.sh`, `05_alg_hs256_confusion.sh` |
| §3 MUST reject missing kid | 隐含于 `04` |
| §3 MUST fetch JWKS on unknown kid | `03_unknown_kid.sh`, `09_kid_rotation.sh` |
| §3 MUST reject expired | `02_expired_jwt.sh` |
| §3 MUST reject tampered signature | `08_tampered_signature.sh` |
| §3 MUST reject missing auth header | `06_no_auth_header.sh` |
| §3 MUST reject malformed auth header | `07_malformed_auth_header.sh` |
| §3 Valid JWT succeeds | `01_valid_jwt.sh` |
| §4 Meta schema | `12_meta_schema.sh` |
| §4 Meta auth_mode accuracy | `13_meta_auth_mode_accuracy.sh` |
| §5 Error JSON shape | `14_error_json_shape.sh` |
| §6 MCP initialize | `15_mcp_initialize.sh` |
| §6 MCP tools/list | `16_mcp_tools_list.sh` |
| §6 MCP notification 202 | `17_mcp_notification_202.sh` |
| §1 Healthz | `10_healthz_ok.sh` |
| §1 Readyz | `11_readyz_ok.sh` |
| §3 Spec B 扩展 mcp_config_ui token | `18_mcp_config_ui_auth.sh` |
| §4 Spec B 扩展 /meta.nav schema | `19_meta_nav_schema.sh` |
| §4 Spec B 扩展 /meta.permissions schema | `20_meta_permissions_schema.sh` |
| §4 Spec B 扩展 /meta.config_mode 枚举 | `21_meta_config_mode_schema.sh` |
| §4 Spec A §6.4 引入 /meta.config_status 枚举 | `22_meta_config_status_schema.sh` |
