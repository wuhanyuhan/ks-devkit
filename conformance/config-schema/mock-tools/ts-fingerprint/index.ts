// ks-conf-ts-fingerprint — 公钥指纹 mock-tool（TypeScript 侧）。
//
// 用法：
//   bun run index.ts <pubkey_hex>
//
// 输入：32 字节 X25519 公钥的 hex（64 字符）。
// 输出（stdout）：spec-v1 §4.2 fingerprint 字符串（8 段 × 4 hex × ':'），
// 例如 `6668:7aad:f862:bd77:6c8f:c18b:8e9f:8e20`。
//
// 源码镜像（与前端 TS 字节级一致）：
//   mcp-config 前端 source mirror: crypto-e2e.ts
//     → export async function computeFingerprint(pubkey)
//
// 本侧用 Node 内置 crypto.createHash（Bun 也兼容），避免拉 Web Crypto subtle
// 的异步 + BufferSource 包装成本（conformance 侧只需字节正确）。结果与前端
// Web Crypto SHA-256 完全一致。

import { createHash } from "node:crypto";

const X25519_PUBKEY_LEN = 32;

function hexToBytes(hex: string): Uint8Array {
  if (hex.length % 2 !== 0) {
    throw new Error(`pubkey hex 长度 ${hex.length} 非偶数`);
  }
  const out = new Uint8Array(hex.length / 2);
  for (let i = 0; i < out.length; i++) {
    const b = parseInt(hex.substr(i * 2, 2), 16);
    if (Number.isNaN(b)) throw new Error(`非法 hex 字符 位置 ${i * 2}`);
    out[i] = b;
  }
  return out;
}

function bytesToHex(b: Uint8Array): string {
  let s = "";
  for (const x of b) s += x.toString(16).padStart(2, "0");
  return s;
}

function fingerprint(pubkey: Uint8Array): string {
  if (pubkey.length !== X25519_PUBKEY_LEN) {
    throw new Error(
      `fingerprint pubkey 长度 = ${pubkey.length}, 期望 ${X25519_PUBKEY_LEN}`
    );
  }
  const h = createHash("sha256").update(pubkey).digest();
  const first16 = h.subarray(0, 16);
  const hex = bytesToHex(first16);
  const parts: string[] = [];
  for (let i = 0; i < 32; i += 4) parts.push(hex.slice(i, i + 4));
  return parts.join(":");
}

function main(): number {
  const argv = process.argv.slice(2);
  if (argv.length !== 1) {
    process.stderr.write("usage: ts-fingerprint <pubkey_hex>\n");
    return 2;
  }
  let pub: Uint8Array;
  try {
    pub = hexToBytes(argv[0]!);
  } catch (e) {
    process.stderr.write(`pubkey hex 解析失败：${String(e)}\n`);
    return 2;
  }
  try {
    process.stdout.write(fingerprint(pub));
  } catch (e) {
    process.stderr.write(`fingerprint 失败：${String(e)}\n`);
    return 2;
  }
  return 0;
}

process.exit(main());
