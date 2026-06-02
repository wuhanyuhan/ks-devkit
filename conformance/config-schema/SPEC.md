# MCP 配置 Schema 协议 Conformance 套件

**版本**：v1（对齐 `Keystone MCP config schema spec v1`）

本套件验证 Go / Python / TypeScript 三语言 SDK 对同一权威 spec 的字节级互通性。凡是跨语言传输的二进制结构（AAD canonical 字节、fingerprint 字符串、show_when 编译后 JSON Schema、加密 payload）都必须在三端产出完全一致的字节，才能保证 MCP 端点与前端 / CLI 的 E2E 加密握手成功。

## 套件结构

与 `conformance/auth/` 平行：

- `cases/NN_*.sh` — 每个 case 是一个 bash 脚本，退出码 0 = pass
- `lib.sh` — 共享辅助函数（fail / pass / load_vectors / bytes_eq / build_go_tool / canonical_json）
- `run.sh` — 单个 case 执行器（`./run.sh 01_aad_go_python_parity`）
- `orchestrate.sh` — 遍历所有 case + 汇总报告
- `mock-tools/` — 三端 mini CLI（AAD / fingerprint / show_when + encrypt / decrypt / keygen 全部已交付）
- `testvectors.json` — 本地固定的三端共享 golden vectors

## Case 分类

### AAD / Fingerprint / show_when（7 个 case）

**AAD canonical 字节互通**（12 testvectors × 3 组合）
- 01 Go ↔ Python
- 02 Go ↔ TS
- 03 Python ↔ TS

**Fingerprint 算法互通**（8 testvectors × 1 组合，三端同时跑）
- 04 Go + Python + TS 三端对同一 pubkey 算出相同 fingerprint 字符串

**show_when 语义等价**（12 个 accept + 3 个 reject testvectors × 3 组合）
- 05 Go ↔ Python
- 06 Go ↔ TS
- 07 Python ↔ TS

### Encrypt / Decrypt / 幂等（10 个 case）

**Encrypt/Decrypt 9 组合互通**（3 × 3 pairwise matrix；每 case 跑真实 X25519-ECDH + AES-256-GCM 加解密 round-trip，plaintext 还原字节级校验）
- 08 Go encrypt → Go decrypt
- 09 Go encrypt → Python decrypt
- 10 Go encrypt → TS decrypt
- 11 Python encrypt → Go decrypt
- 12 Python encrypt → Python decrypt
- 13 Python encrypt → TS decrypt
- 14 TS encrypt → Go decrypt
- 15 TS encrypt → Python decrypt
- 16 TS encrypt → TS decrypt

**幂等 key 格式 smoke**
- 17 Python `uuid.uuid4()` 生成 10 次 UUID 全部匹配正则 `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`

**AAD / fingerprint 正向 golden 比对** 已折入 case 01-04（三端各自输出 vs testvectors.json 字节级比对），未单开 case。`show_when` reject case 已折入 05-07（每个 reject 向量三端退出码非 0 且 **相等**）。

## Mock-tool I/O Contract（极简）

所有 mock-tool 都是 **stdin/argv → stdout** 的纯函数，不启 server，不读外部文件（除 stdin）。

### AAD：3 argv → stdout hex

```
./ks-conf-go-aad <mcp_server_id> <config_version> <fingerprint>
python3 py-aad.py <mcp_server_id> <config_version> <fingerprint>
bun run ts-aad/index.ts <mcp_server_id> <config_version> <fingerprint>
```

输出：小写 hex 字符串（纯字符，无空格/换行/前缀），例如：
```
00106b732d6d63702d696d6167652d67656e00000000000000020027...
```

**关键精度坑**：`config_version` 可能高达 `2^63-1 = 9223372036854775807`，超出 JS `Number.MAX_SAFE_INTEGER`（2^53-1）。TS 侧必须直接走 `process.argv[i]` 保精度字符串，再 `BigInt(str)` 处理，**不能**走 `Number(str)` 或 `JSON.parse`。

### Fingerprint：1 argv → stdout string

```
./ks-conf-go-fingerprint <pubkey_hex>
```

输入：32 字节公钥的 hex（64 字符）。
输出：`fp:sha256:` 分段字符串，例如 `6668:7aad:f862:bd77:6c8f:c18b:8e9f:8e20`（**不带 `fp:sha256:` 前缀**，spec-v1 §4.2 Fingerprint 字段本身就是纯 hex 分段）。

### show_when：stdin DSL + argv[1] field_name → stdout canonical JSON

```
echo "backend == 'github'" | ./ks-conf-go-showwhen <field_name>
```

输出：编译后的 `if_then_else` 对象（不含 `ui_hidden_when`），用 **canonical JSON** 序列化：
- 字段按字典序排序
- 无缩进 / 无尾随空格
- 字符串用 `\u00XX` 避免转义差异（三端必须选相同策略 — 本套件统一用 Python `json.dumps(..., sort_keys=True, separators=(',', ':'), ensure_ascii=False)` 等价格式）

canonical 比对命令见 `lib.sh` 的 `canonical_json_eq`。

## TS 运行时

选用 **Bun 1.3+**（`which bun` 确认；单一二进制，启动快，TypeScript 原生跑）。

- **AAD / fingerprint / show_when**：仅依赖 Node/Bun 内置 `crypto` + 手写 DSL 编译器（与前端 mcp-config 实现保持同步的 `show_when-compiler.ts`），无 npm 依赖，`npx tsx` + Node 20+ fallback 可跑
- **encrypt / decrypt / keygen**：需要 `@noble/curves@^2.2.0`（X25519 + HKDF）；每个 `ts-*/` 目录独立 `bun install`。由于使用 `Bun.stdin.text()` 稳定读 stdin（规避 `for-await` 的 async flush 丢尾输出问题），此三个 mock-tool **强依赖 Bun**，不走 tsx fallback

每个 `ts-*/` 目录是独立最小 TS 项目：`package.json` + `tsconfig.json` + `index.ts`（+ encrypt/decrypt/keygen 各自精简的 `crypto-e2e.ts` 子集）。核心源码与前端 mcp-config 的 `crypto-e2e` / `show_when-compiler` 实现保持同步、**按需 trim 拷贝**（仅保留该 tool 实际调用的 export；三份 trim 拷贝各自长度不同，见对应文件头注释）。如果前端实现更新，conformance TS 侧必须**每一份独立同步**。

## 运行

```bash
cd ks-devkit/conformance/config-schema
./orchestrate.sh                       # 跑全部 17 case
./run.sh 01_aad_go_python_parity       # 跑单个
./run.sh --only=01,04                  # 跑指定编号
```

## 依赖

- `bun >= 1.3`（encrypt/decrypt/keygen 强依赖 `Bun.stdin.text()`；AAD/fingerprint/show_when 支持 `npx tsx` + Node 20+ fallback）
- `python >= 3.10`
- `go >= 1.22`（`go.mod` 声明 `go 1.26.1`，但实际只用到稳定 API；CI 机器 go ≥ 1.22 也能跑）
- `jq >= 1.7`（u64 大整数精度必需，`orchestrate.sh` preflight 会校验）

Python 侧直接用 `python3` 导入 `sys.path` 里的 `ks_app.crypto.aad` / `ks_app.crypto.fingerprint` / `ks_app.ksconfig.show_when`。若未激活 SDK venv，脚本会自动 `PYTHONPATH` 指向 `sdk/python/src`。
