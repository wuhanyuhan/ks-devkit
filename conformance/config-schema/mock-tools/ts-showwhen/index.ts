// ks-conf-ts-showwhen — show_when DSL 编译 mock-tool（TypeScript 侧）。
//
// 用法：
//   echo "backend == 'github'" | bun run index.ts <field_name>
//
// 输入：stdin 是 DSL 源码（可能含换行，末尾 trim）。
// 输出（stdout）：编译后的 if_then_else 对象，canonical JSON 序列化
//   （字段按字典序排序、无缩进、无尾随空格）。
//
// 退出码：
//   - 0:  编译成功
//   - 10: 运行期错误（arithmetic / cross-level / 非法字面量 等）
//   - 11: SyntaxError（括号嵌套 — programmer error）
//   - 2:  用法错
//
// 编译器源：./show_when-compiler.ts（与前端 mcp-config 实现保持同步，去掉 uiHidden 部分）。

import { compileShowWhen } from "./show_when-compiler.js";

/**
 * Canonical JSON 序列化：递归按字段名字典序输出，无缩进、无空格。
 * 对齐：
 *   - Go:    json.NewEncoder + map[string]any（encoder 按 key 排序）
 *   - Python json.dumps(..., sort_keys=True, separators=(',', ':'),
 *            ensure_ascii=False)
 */
function canonicalJson(v: unknown): string {
  if (v === null) return "null";
  if (typeof v === "boolean") return v ? "true" : "false";
  if (typeof v === "number") {
    if (!Number.isFinite(v)) {
      throw new Error(`canonicalJson: 非法 number ${v}`);
    }
    return String(v);
  }
  if (typeof v === "string") return JSON.stringify(v);
  if (Array.isArray(v)) {
    return "[" + v.map(canonicalJson).join(",") + "]";
  }
  if (typeof v === "object") {
    const obj = v as Record<string, unknown>;
    const keys = Object.keys(obj).sort();
    const parts = keys.map(
      (k) => JSON.stringify(k) + ":" + canonicalJson(obj[k])
    );
    return "{" + parts.join(",") + "}";
  }
  throw new Error(`canonicalJson: 不支持的类型 ${typeof v}`);
}

async function readStdin(): Promise<string> {
  const chunks: Buffer[] = [];
  for await (const c of process.stdin) {
    chunks.push(Buffer.from(c as Buffer | string));
  }
  return Buffer.concat(chunks).toString("utf-8");
}

async function main(): Promise<number> {
  const argv = process.argv.slice(2);
  if (argv.length !== 1) {
    process.stderr.write(
      "usage: echo '<dsl>' | ts-showwhen <field_name>\n"
    );
    return 2;
  }
  const fieldName = argv[0]!;
  const raw = await readStdin();
  const dsl = raw.replace(/[\r\n\t ]+$/, "");

  let result;
  try {
    result = compileShowWhen(dsl, fieldName);
  } catch (e) {
    if (e instanceof SyntaxError) {
      process.stderr.write(`SyntaxError: ${e.message}\n`);
      return 11;
    }
    process.stderr.write(`parse error: ${String(e)}\n`);
    return 10;
  }
  process.stdout.write(canonicalJson(result));
  return 0;
}

main().then((code) => process.exit(code));
