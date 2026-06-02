// ks-conf-ts-aad — AAD canonical 字节 mock-tool（TypeScript 侧）。
//
// 用法：
//   bun run index.ts <mcp_server_id> <config_version> <fingerprint>
//   或
//   tsx index.ts <mcp_server_id> <config_version> <fingerprint>
//
// 输出（stdout）：小写 hex 字符串（无换行/空格），对应 AAD canonical 字节串。
//
// 源码镜像（为了保持与前端 TS 侧字节级一致）：
//   mcp-config 前端 source mirror: crypto-e2e.ts
//     → export function aadCanonicalBytes(id, version: bigint, fp)
//
// 精度坑：config_version 可能高达 2^63-1，超出 Number.MAX_SAFE_INTEGER。
// process.argv[3] 原样是字符串，直接 BigInt(str) 保精度 — **禁止** Number(str)
// 或 JSON.parse(str)，否则最大 int63 会 silently 丢精度，生成错误 AAD。

const encoder = new TextEncoder();

/**
 * 生成 AES-GCM AAD 的 canonical 字节串，大端编码。
 *
 * 字节布局（镜像 ks-types kstypes.AADCanonicalBytes）:
 *   u16_be(len(mcpServerID)) || utf8(mcpServerID)
 *     || u64_be(configVersion)
 *     || u16_be(len(fingerprint)) || utf8(fingerprint)
 */
function aadCanonicalBytes(
  mcpServerID: string,
  configVersion: bigint,
  fingerprint: string
): Uint8Array {
  const idBytes = encoder.encode(mcpServerID);
  const fpBytes = encoder.encode(fingerprint);
  const buf = new Uint8Array(2 + idBytes.length + 8 + 2 + fpBytes.length);
  const view = new DataView(buf.buffer);
  let off = 0;
  view.setUint16(off, idBytes.length, false);
  off += 2;
  buf.set(idBytes, off);
  off += idBytes.length;
  view.setBigUint64(off, configVersion, false);
  off += 8;
  view.setUint16(off, fpBytes.length, false);
  off += 2;
  buf.set(fpBytes, off);
  return buf;
}

function bytesToHex(b: Uint8Array): string {
  let s = "";
  for (const x of b) s += x.toString(16).padStart(2, "0");
  return s;
}

function main(): number {
  const argv = process.argv.slice(2);
  if (argv.length !== 3) {
    process.stderr.write(
      "usage: ts-aad <mcp_server_id> <config_version> <fingerprint>\n"
    );
    return 2;
  }
  const [mcpID, verStr, fp] = argv as [string, string, string];
  let version: bigint;
  try {
    version = BigInt(verStr);
  } catch (e) {
    process.stderr.write(`config_version 解析失败：${String(e)}\n`);
    return 2;
  }
  if (version < 0n) {
    process.stderr.write("config_version 必须 >= 0\n");
    return 2;
  }

  const aad = aadCanonicalBytes(mcpID, version, fp);
  process.stdout.write(bytesToHex(aad));
  return 0;
}

process.exit(main());
