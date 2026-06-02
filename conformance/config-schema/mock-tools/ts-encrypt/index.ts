// ks-conf-ts-encrypt — X25519-ECDH + AES-256-GCM 加密 mock-tool（TypeScript 侧）。
//
// 用法:
//   echo '<json>' | bun run index.ts
//
// 输入 (stdin, JSON):
//   {
//     "mcp_pubkey_b64":  "base64-std 32B",
//     "mcp_server_id":   "ks-mcp-test",
//     "config_version":  123,          // JSON number（JS 侧会走 BigInt 保精度）
//                                       // 若需超过 2^53-1，前端约定传字符串；本 mock 同时支持
//     "fingerprint":     "ab12:...",
//     "plaintext_b64":   "base64-std 明文"
//   }
//
// 输出 (stdout, JSON): 对齐 EncryptedConfigPayload（idempotency_key 省略）:
//   {
//     "algorithm":        "x25519-ecdh-aes256gcm-v1",
//     "ephemeral_pubkey": "base64-std 32B",
//     "nonce":            "base64-std 12B",
//     "aad_fields":       { "mcp_server_id": ..., "config_version": ..., "fingerprint": ... },
//     "aad_canonical":    "base64-std AAD bytes",
//     "ciphertext":       "base64-std ct||tag"
//   }
//
// 退出码:
//   - 0:  加密成功
//   - 2:  用法错 / JSON 解析错
//   - 21: pubkey 长度错 / base64 解码错

import {
  aadCanonicalBytes,
  ALGORITHM_V1,
  deriveKEK,
  encryptAESGCM,
  generateKeyPair,
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
  // 用 Bun.stdin.text()：conformance 套件统一用 bun 作为 TS runtime（SPEC.md）。
  // 注：早期用 `for await (const c of process.stdin)` 在 bun short-lived 脚本 +
  // stdin pipe 组合下偶发 hang / 输出丢失；Bun.stdin.text() 是官方推荐 API，
  // 内部正确处理 stream close + flush，稳定性胜于 Node-style for-await。
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

  let mcpPub: Uint8Array;
  try {
    mcpPub = base64ToBytes(String(payload.mcp_pubkey_b64));
  } catch (e) {
    exitLen(`mcp_pubkey_b64 解码失败：${String(e)}`);
  }
  if (mcpPub.length !== X25519_PUBKEY_LEN) {
    exitLen(
      `mcp_pubkey 长度 = ${mcpPub.length}, 期望 ${X25519_PUBKEY_LEN}`,
    );
  }

  let plaintext: Uint8Array;
  try {
    plaintext = base64ToBytes(String(payload.plaintext_b64));
  } catch (e) {
    exitLen(`plaintext_b64 解码失败：${String(e)}`);
  }

  const mcpServerID = String(payload.mcp_server_id);
  // config_version 支持 number 或字符串（后者是为了 > 2^53-1 精度）
  const rawV = payload.config_version;
  const configVersion =
    typeof rawV === "string" ? BigInt(rawV) : BigInt(Number(rawV));
  const fingerprint = String(payload.fingerprint);

  const eph = await generateKeyPair();
  const shared = x25519ECDH(eph.privateKey, mcpPub);
  const kek = await deriveKEK(shared);
  const aad = aadCanonicalBytes(mcpServerID, configVersion, fingerprint);
  const { ciphertext, nonce } = await encryptAESGCM(kek, plaintext, aad);

  // aad_fields.config_version 输出策略：
  //   - 若输入是 number 且 ≤ Number.MAX_SAFE_INTEGER → 保留 number 型（与 Go/Python 一致）
  //   - 否则输出字符串（保精度，u64 语义）
  // 本套件的测试 config_version 都是小整数，走 number 分支。
  const aadVersionOut: number | string =
    configVersion <= BigInt(Number.MAX_SAFE_INTEGER) &&
    configVersion >= 0n
      ? Number(configVersion)
      : configVersion.toString();

  const out = {
    algorithm: ALGORITHM_V1,
    ephemeral_pubkey: bytesToBase64(eph.publicKey),
    nonce: bytesToBase64(nonce),
    aad_fields: {
      mcp_server_id: mcpServerID,
      config_version: aadVersionOut,
      fingerprint,
    },
    aad_canonical: bytesToBase64(aad),
    ciphertext: bytesToBase64(ciphertext),
  };
  // 用 console.log 而非 process.stdout.write：Bun 在 short-lived 脚本 +
  // process.exit() 组合下对 stdout 有缓冲 race（可能吞掉最后一段输出）；
  // console.log 内部会把 stdout 当 line buffered + 等待 flush，更稳。
  // 注意：console.log 会附加 '\n'，下游 jq 解析 JSON 不受影响。
  console.log(JSON.stringify(out));
  return 0;
}

main().then((code) => process.exit(code)).catch((e) => {
  process.stderr.write(`加密失败：${String(e)}\n`);
  process.exit(2);
});
