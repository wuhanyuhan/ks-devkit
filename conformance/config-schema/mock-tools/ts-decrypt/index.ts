// ks-conf-ts-decrypt — X25519-ECDH + AES-256-GCM 解密 mock-tool（TypeScript 侧）。
//
// 用法:
//   echo '<json>' | bun run index.ts
//
// 输入 (stdin, JSON):
//   {
//     "mcp_privkey_b64":   "base64-std 32B",
//     "ephemeral_pubkey":  "base64-std 32B",
//     "nonce":             "base64-std 12B",
//     "aad_canonical":     "base64-std AAD bytes",
//     "ciphertext":        "base64-std ct||tag"
//   }
//
// 输出 (stdout, JSON):
//   { "plaintext_b64": "base64-std 明文" }
//
// 退出码:
//   - 0:  解密成功
//   - 2:  用法错 / JSON 解析错 / 其他异常
//   - 20: （保留，本 mock 未实现 AAD 重算对比）
//   - 21: privkey / ephemeral_pubkey / nonce 长度错 / base64 解码错
//   - 22: GCM tag 校验失败

import {
  AES_GCM_NONCE_LEN,
  decryptAESGCM,
  deriveKEK,
  X25519_PRIVKEY_LEN,
  X25519_PUBKEY_LEN,
  x25519ECDH,
} from "./crypto-e2e.js";

function base64ToBytes(s: string): Uint8Array {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function bytesToBase64(b: Uint8Array): string {
  let s = "";
  const chunk = 0x8000;
  for (let i = 0; i < b.length; i += chunk) {
    s += String.fromCharCode(...b.subarray(i, i + chunk));
  }
  return btoa(s);
}

async function readStdin(): Promise<string> {
  // 见 ts-encrypt/index.ts 的同名注释：Bun.stdin.text() 比 Node-style for-await
  // 更稳定，short-lived 脚本 + stdin pipe 下不会丢输出。
  return await Bun.stdin.text();
}

function exitLen(msg: string): never {
  process.stderr.write(msg + "\n");
  process.exit(21);
}

async function main(): Promise<number> {
  const raw = await readStdin();
  let payload: Record<string, unknown>;
  try {
    payload = JSON.parse(raw);
  } catch (e) {
    process.stderr.write(`JSON 解析失败：${String(e)}\n`);
    return 2;
  }

  let priv: Uint8Array;
  try {
    priv = base64ToBytes(String(payload.mcp_privkey_b64));
  } catch (e) {
    exitLen(`mcp_privkey_b64 解码失败：${String(e)}`);
  }
  if (priv.length !== X25519_PRIVKEY_LEN) {
    exitLen(`mcp_privkey 长度 = ${priv.length}, 期望 ${X25519_PRIVKEY_LEN}`);
  }

  let ephPub: Uint8Array;
  try {
    ephPub = base64ToBytes(String(payload.ephemeral_pubkey));
  } catch (e) {
    exitLen(`ephemeral_pubkey 解码失败：${String(e)}`);
  }
  if (ephPub.length !== X25519_PUBKEY_LEN) {
    exitLen(
      `ephemeral_pubkey 长度 = ${ephPub.length}, 期望 ${X25519_PUBKEY_LEN}`,
    );
  }

  let nonce: Uint8Array;
  try {
    nonce = base64ToBytes(String(payload.nonce));
  } catch (e) {
    exitLen(`nonce 解码失败：${String(e)}`);
  }
  if (nonce.length !== AES_GCM_NONCE_LEN) {
    exitLen(`nonce 长度 = ${nonce.length}, 期望 ${AES_GCM_NONCE_LEN}`);
  }

  let aad: Uint8Array;
  try {
    aad = base64ToBytes(String(payload.aad_canonical));
  } catch (e) {
    exitLen(`aad_canonical 解码失败：${String(e)}`);
  }

  let ct: Uint8Array;
  try {
    ct = base64ToBytes(String(payload.ciphertext));
  } catch (e) {
    exitLen(`ciphertext 解码失败：${String(e)}`);
  }

  const shared = x25519ECDH(priv, ephPub);
  const kek = await deriveKEK(shared);

  let plaintext: Uint8Array;
  try {
    plaintext = await decryptAESGCM(kek, nonce, ct, aad);
  } catch (e) {
    // Web Crypto 的 subtle.decrypt 在 AES-GCM tag 失败时抛 OperationError。
    // 本 mock 把所有解密 throw 都映射为退出码 22（GCM tag 失败）。
    process.stderr.write(`GCM tag 校验失败：${String(e)}\n`);
    return 22;
  }

  const out = { plaintext_b64: bytesToBase64(plaintext) };
  // 用 console.log：Bun 短生命周期脚本 + stdin pipe + process.exit 的组合下
  // process.stdout.write 偶发丢尾段；console.log 走 line-buffered + flush 更稳。
  console.log(JSON.stringify(out));
  return 0;
}

main().then((code) => process.exit(code)).catch((e) => {
  process.stderr.write(`解密失败：${String(e)}\n`);
  process.exit(2);
});
