// ks-conf-ts-keygen — X25519 密钥对 + fingerprint 生成 mock-tool（TypeScript 侧）。
//
// 用法:
//   bun run index.ts
//
// 输入: 无。
// 输出 (stdout, JSON):
//   {
//     "privkey_b64":   "base64-std 32B",
//     "pubkey_b64":    "base64-std 32B",
//     "fingerprint":   "ab12:cd34:..."
//   }
//
// 使用 @noble/curves 的 x25519.keygen()（与前端 crypto-e2e.ts 一致）生成密钥对，
// fingerprint 由同一 vendored crypto-e2e.computeFingerprint 计算。
//
// 退出码:
//   - 0: 成功
//   - 2: 生成失败（极少见）

import {
  computeFingerprint,
  generateKeyPair,
} from "./crypto-e2e.js";

function bytesToBase64(b: Uint8Array): string {
  let s = "";
  const chunk = 0x8000;
  for (let i = 0; i < b.length; i += chunk) {
    s += String.fromCharCode(...b.subarray(i, i + chunk));
  }
  return btoa(s);
}

async function main(): Promise<number> {
  try {
    const { privateKey, publicKey } = await generateKeyPair();
    const fp = await computeFingerprint(publicKey);
    const out = {
      privkey_b64: bytesToBase64(privateKey),
      pubkey_b64: bytesToBase64(publicKey),
      fingerprint: fp,
    };
    // console.log 代替 process.stdout.write：Bun 短生命周期 + process.exit 组合下
    // stdout buffer 偶发不 flush；console.log 走 line-buffered 更稳（尾部换行被
    // 下游 jq 正确忽略，不影响 conformance 验证）。
    console.log(JSON.stringify(out));
    return 0;
  } catch (e) {
    process.stderr.write(`keygen 失败：${String(e)}\n`);
    return 2;
  }
}

main().then((code) => process.exit(code));
