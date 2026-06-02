# keystore 与 crypto（端到端加密底座）

> **这是 config-schema 的底层。** 绝大多数 app **不直接调用**本文 API——[config-schema.md](config-schema.md) 已封装好配置的加密下发与落盘。只有要做**密钥轮换 SOP**、或在相同原语上自建加密通道时才需要这层。
>
> **可用性**：Go SDK（`ksapp/keystore`、`ksapp/crypto`）与 Python SDK（`ks_app.keystore`、`ks_app.crypto`）都提供，**三语言字节级互通**（常量一致：`HKDF_INFO = "ksapp-config-v1"`、AES-GCM nonce 12 字节、X25519/KEK 密钥 32 字节）。**TypeScript SDK 无对应包**——TS 侧配置加密由 Keystone Web 前端承担（见 `sdk/typescript/src/types.ts` 注释），SDK 不暴露 keystore/crypto。

## 1. keystore — 私钥加载 / DEK 落盘 / 密钥轮换

`keystore` 管两类密钥：

- **X25519 私钥**：app 的配置解密身份（管理员用对应公钥对配置做信封加密）。三来源加载：env → secret 文件 → fallback 文件（不存在时自动生成）。
- **DEK**（32 字节对称密钥）：本地落盘加密用（AES-GCM 加密 `mcp-config.enc`），与 X25519 私钥**完全无关**。

### 公开面

| 用途 | Go (`ksapp/keystore`) | Python (`ks_app.keystore`) |
|---|---|---|
| 加载私钥 keystore | `Load(opts *LoadOptions) (*Keystore, error)` | `load(opts: LoadOptions \| None = None) -> Keystore` |
| 轮换 X25519 密钥 | `Rotate(opts *RotateOptions) (*RotateResult, error)` | `rotate(opts: RotateOptions \| None = None) -> RotateResult` |
| 清理过期旧密钥 | `PruneOld(path string) error` | `prune_old(path: str = "") -> None` |
| 加载/生成 DEK | `LoadOrGenerateDEK(path string) ([]byte, error)` | `load_or_generate_dek(path: str) -> bytes` |
| DEK 加密落盘 | `EncryptConfigToFile(cfgPath string, dek, plaintext []byte) error` | `encrypt_config_to_file(cfg_path, dek, plaintext) -> None` |
| DEK 解密读盘 | `DecryptConfigFromFile(cfgPath string, dek []byte) ([]byte, error)` | `decrypt_config_from_file(cfg_path, dek) -> bytes` |

类型（两端对齐）：`Source` / `Keypair` / `Keystore` / `LoadOptions` / `RotateOptions` / `RotateResult`。

> **密钥轮换 SOP**：`rotate` 生成新 X25519 keypair 并保留旧私钥一段时间（`OLD_KEY_RETENTION_DAYS`），让在途密文仍可解；过保留期后 `prune_old` 清理。轮换窗口内新旧公钥都能解，平滑切换不停机。

## 2. crypto — 端到端加密低层原语

config-schema 信封加密的底层原语，三组：

| 原语 | Go (`ksapp/crypto`) | Python (`ks_app.crypto`) |
|---|---|---|
| 生成 X25519 keypair | `GenerateX25519() (priv, pub []byte, err error)` | `generate_x25519() -> tuple[bytes, bytes]` |
| X25519 ECDH | `X25519(privkey, peerPubkey []byte) ([]byte, error)` | `x25519_ecdh(privkey, peer_pubkey) -> bytes` |
| 私钥派生公钥 | `DeriveX25519Pub(privkey []byte) (priv, pub []byte, err error)` | `derive_pubkey_from_privkey(privkey) -> tuple[bytes, bytes]` |
| HKDF 派生 KEK | `DeriveKEK(shared []byte) ([]byte, error)` | `derive_kek(shared) -> bytes` |
| AES-256-GCM 加密 | `EncryptAESGCM(kek, plaintext, aad []byte) (ciphertext, nonce []byte, err error)` | `encrypt_aes_gcm(...) ` |
| AES-256-GCM 解密 | `DecryptAESGCM(kek, nonce, ciphertext, aad []byte) ([]byte, error)` | `decrypt_aes_gcm(...)` |
| 公钥 fingerprint | `kstypes.Fingerprint(pubkey []byte) string` ¹ | `fingerprint(pubkey) -> str` |
| AAD canonical 字节 | `kstypes.AADCanonicalBytes(mcpServerID, configVersion, fingerprint)` ¹ | `aad_canonical_bytes(...)` |

¹ Go 侧 AAD/fingerprint 由 `ks-types` 包提供（`ksapp/crypto` 不重复实现）；Python 在 `ks_app.crypto` 内提供。

规范见 [config-schema.md](config-schema.md)。

## 3. 何时用 / 何时不用

- **不用**（绝大多数场景）：声明 config-schema 即可，SDK 自动用这层加密下发 + 落盘。见 [config-schema.md](config-schema.md)。
- **用**：运维侧的 X25519 密钥轮换；或在相同 spec 上自建跨进程加密通道。
