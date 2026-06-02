# 鉴权与安全模型

Keystone 生态的 app 有**两层**鉴权，别混淆：

| 层 | 保护什么 | 机制 | 谁触发 |
|---|---|---|---|
| **app 级入站**（`auth.mode`） | app 的主入站端点（`/mcp` 等） | keystone 签发的入站 JWT，SDK 用 JWKS 验签 | `WithKeystoneAuth()` / `keystone_auth=True` |
| **capability 级 scoped JWT** | `backend.kind=http_endpoint` 的单个能力路由 | 每路由按 `aud=canonical_name` 校验短期 scoped JWT | SDK 自动挂载（http_endpoint backend） |

两层最终都把**发起 user / 直接 caller / 调用链**喂进 handler 的 `CapabilityContext`（见 [sdk-api-reference.md](sdk-api-reference.md)）——业务代码读身份只认 `CapabilityContext`，不用自己解 token。

## 1. app 级入站鉴权（auth.mode）

`auth.mode`（manifest 顶层 `auth.mode`）描述 app 入站 `/mcp` 端点的鉴权模式。三态：

| mode | 行为 |
|---|---|
| `none` | `/mcp` 不做鉴权，依赖网络边界。agent/skill（无独立入站端点）默认 none。 |
| `keystone_jwks` | 通过 keystone `/.well-known/jwks.json` 公钥验签入站 JWT。**secure-by-default**，有独立进程的 app/squad 默认此模式。 |
| `static_bearer` | 比对静态 Bearer token（调用方在平台侧配置）。 |

**secure-by-default**：`auth.mode` 留空时由 SDK 按形态派生——有入站端点（container/process）→ `keystone_jwks`，无入站端点 → `none`。所以"不写"不等于"不鉴权"。

开启：

```go
app := ksapp.New("my-app", ksapp.WithKeystoneAuth())   // → keystone_jwks
```
```python
app = App(id="my-app", keystone_auth=True)              # → keystone_jwks
```
```typescript
const app = createApp({ id: "my-app", auth: "keystone_jwks" });
```

### 环境变量

| 变量 | 作用 |
|---|---|
| `KEYSTONE_JWKS_URL` | keystone JWKS 端点。`keystone_jwks` 模式必需（Keystone 安装时注入）。 |
| `KS_APP_AUTH_MODE=insecure` | **全局逃生阀**：把 effective mode 强制降级为 `none`。仅本地裸跑用。 |

> ⚠️ **fail-fast**：若 effective mode 是 `keystone_jwks` 但 `KEYSTONE_JWKS_URL` 未设、且没设 `KS_APP_AUTH_MODE=insecure`，SDK 启动期直接 **panic**（"生产必须设置此 env，或本地开发用 `KS_APP_AUTH_MODE=insecure` 降级"）。
>
> **别把 `auth.mode` 改成 `none` 提交**。本地裸跑请用 `export KS_APP_AUTH_MODE=insecure`（见 [best-practices.md](best-practices.md)）。

## 2. capability 级 scoped JWT（http_endpoint backend）

声明了 `backend.kind=http_endpoint` 的能力，SDK 会**自动**给每个能力路由套上 scoped JWT 中间件：校验 `aud` 等于该能力的 `canonical_name`，验签通过后把 `ScopedClaims` 注入请求上下文（再桥接到 `CapabilityContext`）。

### 出站签发 → 入站校验

```
调用方（user / 另一 app）
        │
        ▼
keystone：签发短期 scoped JWT
   sub=user, aud=<目标 canonical_name>,
   kx_caller_id / kx_caller_kind / kx_chain_id / kx_request_id
        │  Authorization: Bearer <jwt>
        ▼
app SDK scoped JWT 中间件：JWKS 验签 + aud 校验
        │
        ▼
ScopedClaims → CapabilityContext：handler 拿到 发起 user / caller / 调用链
```

### claims 形状

| JWT claim | 含义 | Go `ScopedClaims` | Python `ScopedClaims` | TS `ScopedClaims` |
|---|---|---|---|---|
| `sub` | 发起用户 | `UserID` | `user_id` | `userId` |
| `aud` | 目标能力 canonical_name | `CanonicalName` | `canonical_name` | `canonicalName` |
| `kx_caller_id` | 直接调用方 | `CallerID` | `caller_id` | `callerId` |
| `kx_caller_kind` | 调用方类型 | `CallerKind` | `caller_kind` | `callerKind` |
| `kx_chain_id` | 调用链 ID | `ChainID` | `chain_id` | `chainId` |
| `kx_request_id` | 请求 ID | `RequestID` | `request_id` | `requestId` |
| `iat` / `exp` | 签发 / 过期 | `IssuedAt` / `ExpiresAt` | `issued_at` / `expires_at` | `issuedAt` / `expiresAt` |

### 底层 verifier / middleware

通常**不需要手动调用**（SDK 为 http_endpoint 能力自动挂载）。底层 API：

```go
verifier := ksapp.NewScopedJWTVerifier(jwksURL)
mw := ksapp.ScopedJWTMiddleware(verifier, map[string]string{
    "/my/route": "my-app.my_capability",  // path → 期望 aud（canonical_name）
})
```
```python
verifier = ScopedJWTVerifier(jwks_url=...)   # 中间件见 ks_app/auth/scoped_jwt_middleware.py
```
```typescript
const verifier = new ScopedJWTVerifier(jwksUrl);
app.use(scopedJwtMiddleware(verifier, { "/my/route": "my-app.my_capability" }));
```

> JWKS 拉取成熟度有差异：TS（`createRemoteJWKSet`）、Python（`PyJWKClient`）已做远程 JWKS 拉取；Go 的 `ScopedJWTVerifier` 当前 MVP 走 `SetStaticKey` 注入受信公钥（JWKS lazy fetch 为预留入口）。

## 3. 诚实理解 enforcement 状态

并非所有安全声明都已在运行时强制（详见 [best-practices.md](best-practices.md) §6）：

| 声明 | 现在强制什么 | 还没接线 |
|---|---|---|
| `auth.mode: keystone_jwks` | ✅ 入站 JWT 验签（真拦截） | — |
| scoped JWT（http_endpoint） | ✅ aud + 签名校验（真拦截） | — |
| `permissions.network` / `filesystem` | 声明 + admin 安装期透明审查 | 运行时 egress / 路径沙箱（未接线） |

填 `permissions.allowed_domains` **不等于**被沙箱拦截——当前只是声明 + 让 admin 安装时看见。**鉴权（auth.mode / scoped JWT）是真校验，权限沙箱目前是声明审查。**
