// 配置加密 E2E 工具的精简副本（与前端 mcp-config 实现保持同步）。
//
// 同步契约：若加密实现修改（HKDF info 串 / salt 策略 / AAD 编码 / algorithm ID
// 等），必须同步更新本副本，否则三端互通 case（08-16）会立即失败。
//
// 按 ts-encrypt 实际使用的 export 按需 trim；ts-decrypt / ts-keygen 各自 trim
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

function toArrayBufferView(b: Uint8Array): Uint8Array<ArrayBuffer> {
  const copy = new Uint8Array(b.length)
  copy.set(b)
  return copy as Uint8Array<ArrayBuffer>
}

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

export async function generateKeyPair(): Promise<{
  privateKey: Uint8Array
  publicKey: Uint8Array
}> {
  const kp = x25519.keygen()
  return { privateKey: kp.secretKey, publicKey: kp.publicKey }
}

export function x25519ECDH(
  privateKey: Uint8Array,
  peerPublicKey: Uint8Array,
): Uint8Array {
  assertLength('privateKey', privateKey.length, X25519_PRIVKEY_LEN)
  assertLength('peerPublicKey', peerPublicKey.length, X25519_PUBKEY_LEN)
  return x25519.getSharedSecret(privateKey, peerPublicKey)
}

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
