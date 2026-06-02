// 配置加密 E2E 工具的精简副本（与前端 mcp-config 实现保持同步）。
//
// 同步契约：若加密实现修改（HKDF info 串 / salt 策略 / AAD 编码 / algorithm ID
// 等），必须同步更新本副本，否则三端互通 case（08-16）会立即失败。
//
// 按 ts-keygen 实际使用的 export 按需 trim；ts-encrypt / ts-decrypt 各自 trim
// 范围不同，三份副本非字节级相同。
//
// 本副本只用于 conformance mock-tool（开发用），不经生产 bundle。

import { x25519 } from '@noble/curves/ed25519.js'

/** HKDF-SHA256 派生 KEK 的 info 串（三语言一致，禁止修改）。 */
const HKDF_INFO = 'ksapp-config-v1'

/** X25519 公钥长度。 */
export const X25519_PUBKEY_LEN = 32
/** X25519 私钥长度。 */
export const X25519_PRIVKEY_LEN = 32
/** AES-256 KEK 长度。 */
export const KEK_LEN = 32
/** AES-GCM nonce 长度（三语言一致）。 */
export const AES_GCM_NONCE_LEN = 12

/** Spec A 当前唯一算法标识；未来 HPKE / P-256 降级切 v2 时新增。 */
export const ALGORITHM_V1 = 'x25519-ecdh-aes256gcm-v1' as const

const textEncoder = new TextEncoder()

function assertLength(
  name: string,
  actual: number,
  expected: number,
): void {
  if (actual !== expected) {
    throw new Error(`crypto: ${name} 长度 = ${actual}, 期望 ${expected}`)
  }
}

// 把 Uint8Array 归正为独立 ArrayBuffer 视图：TS 5.x 下 Web Crypto 的
// BufferSource 不接受带 ArrayBufferLike 参数类型，需确保 buffer 类型是 ArrayBuffer。
function toArrayBufferView(b: Uint8Array): Uint8Array<ArrayBuffer> {
  const copy = new Uint8Array(b.length)
  copy.set(b)
  return copy as Uint8Array<ArrayBuffer>
}

/**
 * 生成 AES-GCM AAD 的 canonical 字节串，大端编码。
 *
 * 字节布局（镜像 ks-types kstypes.AADCanonicalBytes）：
 *   u16_be(len(mcpServerID)) || utf8(mcpServerID)
 *     || u64_be(configVersion)
 *     || u16_be(len(fingerprint)) || utf8(fingerprint)
 */
export function aadCanonicalBytes(
  mcpServerID: string,
  configVersion: bigint,
  fingerprint: string,
): Uint8Array {
  const idBytes = textEncoder.encode(mcpServerID)
  const fpBytes = textEncoder.encode(fingerprint)
  const buf = new Uint8Array(2 + idBytes.length + 8 + 2 + fpBytes.length)
  const view = new DataView(buf.buffer)
  let off = 0
  view.setUint16(off, idBytes.length, false)
  off += 2
  buf.set(idBytes, off)
  off += idBytes.length
  view.setBigUint64(off, configVersion, false)
  off += 8
  view.setUint16(off, fpBytes.length, false)
  off += 2
  buf.set(fpBytes, off)
  return buf
}

/**
 * 计算 X25519 公钥的 spec-v1 §4.2 fingerprint：`sha256(pubkey)[:16]` 转小写 hex，
 * 每 4 字符插 `:`，共 8 段。pubkey 必须 32 字节。
 */
export async function computeFingerprint(pubkey: Uint8Array): Promise<string> {
  assertLength('fingerprint pubkey', pubkey.length, X25519_PUBKEY_LEN)
  const digest = new Uint8Array(
    await crypto.subtle.digest('SHA-256', toArrayBufferView(pubkey)),
  )
  const first16 = digest.slice(0, 16)
  let hex = ''
  for (const b of first16) hex += b.toString(16).padStart(2, '0')
  const groups: string[] = []
  for (let i = 0; i < 32; i += 4) groups.push(hex.slice(i, i + 4))
  return groups.join(':')
}

/** 生成一对随机 X25519 密钥（私钥 32B + 公钥 32B）。 */
export async function generateKeyPair(): Promise<{
  privateKey: Uint8Array
  publicKey: Uint8Array
}> {
  const kp = x25519.keygen()
  return { privateKey: kp.secretKey, publicKey: kp.publicKey }
}

/**
 * 用本端私钥与对端公钥执行 X25519-ECDH，返回 32 字节共享秘密。
 * 长度错误抛 Error；低阶公钥由 noble/curves 抛错。
 */
export function x25519ECDH(
  privateKey: Uint8Array,
  peerPublicKey: Uint8Array,
): Uint8Array {
  assertLength('privateKey', privateKey.length, X25519_PRIVKEY_LEN)
  assertLength('peerPublicKey', peerPublicKey.length, X25519_PUBKEY_LEN)
  return x25519.getSharedSecret(privateKey, peerPublicKey)
}

/**
 * 用 HKDF-SHA256 从 X25519 共享秘密派生 32 字节 KEK。
 * salt = 32 字节全零；info = HKDF_INFO。
 */
export async function deriveKEK(shared: Uint8Array): Promise<Uint8Array> {
  if (shared.length === 0) {
    throw new Error('crypto: shared secret 不能为空')
  }
  const ikm = await crypto.subtle.importKey(
    'raw',
    toArrayBufferView(shared),
    { name: 'HKDF' },
    false,
    ['deriveBits'],
  )
  const bits = await crypto.subtle.deriveBits(
    {
      name: 'HKDF',
      hash: 'SHA-256',
      salt: new Uint8Array(32) as Uint8Array<ArrayBuffer>,
      info: toArrayBufferView(textEncoder.encode(HKDF_INFO)),
    },
    ikm,
    KEK_LEN * 8,
  )
  return new Uint8Array(bits)
}

async function importAESKey(kek: Uint8Array): Promise<CryptoKey> {
  assertLength('kek', kek.length, KEK_LEN)
  return crypto.subtle.importKey(
    'raw',
    toArrayBufferView(kek),
    { name: 'AES-GCM' },
    false,
    ['encrypt', 'decrypt'],
  )
}

/**
 * AES-256-GCM 加密。返回的 ciphertext 末尾附 16 字节 GCM tag，
 * 与 Go `gcm.Seal` / Python `AESGCM.encrypt` 兼容。
 */
export async function encryptAESGCM(
  kek: Uint8Array,
  plaintext: Uint8Array,
  aad: Uint8Array,
): Promise<{ ciphertext: Uint8Array; nonce: Uint8Array }> {
  const key = await importAESKey(kek)
  const nonce = crypto.getRandomValues(new Uint8Array(AES_GCM_NONCE_LEN))
  const ct = await crypto.subtle.encrypt(
    {
      name: 'AES-GCM',
      iv: nonce as Uint8Array<ArrayBuffer>,
      additionalData: toArrayBufferView(aad),
    },
    key,
    toArrayBufferView(plaintext),
  )
  return { ciphertext: new Uint8Array(ct), nonce }
}

/**
 * AES-256-GCM 解密。主要服务测试 + 对称性验证（前端 POST 路径不解密，
 * 解密发生在 MCP 侧）。aad 不一致、密钥错误、密文被改都会抛错。
 */
export async function decryptAESGCM(
  kek: Uint8Array,
  nonce: Uint8Array,
  ciphertext: Uint8Array,
  aad: Uint8Array,
): Promise<Uint8Array> {
  assertLength('nonce', nonce.length, AES_GCM_NONCE_LEN)
  const key = await importAESKey(kek)
  const pt = await crypto.subtle.decrypt(
    {
      name: 'AES-GCM',
      iv: toArrayBufferView(nonce),
      additionalData: toArrayBufferView(aad),
    },
    key,
    toArrayBufferView(ciphertext),
  )
  return new Uint8Array(pt)
}
