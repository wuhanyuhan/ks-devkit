// show_when DSL 手写递归下降 parser + JSON Schema if/then/else 编译器。
//
// **镜像源（权威 TS 实现）**：
//   mcp-config 前端 source mirror: show_when-compiler.ts
//
// **同步契约**：如果前端 show_when-compiler.ts 改了，本文件**必须**同步更新，
// 否则 conformance case 05-07 会失败（三端 JSON Schema 不等价）。
//
// 为什么 vendored 而不 symlink / import：
//   - 前端是 React 项目，路径下有 rjsf-specific 依赖（SchemaForm 等），直接
//     import 会污染本 conformance 子项目的 node_modules 与 tsconfig
//   - 前端包独立发布，conformance 侧需要"可单独跑"
//   - 只复制纯算法子集（parser + compileNode），去掉 uiHidden 相关导出
//
// DSL 语法 (spec-v1 §3.1 EBNF):
//   expr     = or_expr
//   or_expr  = and_expr { "||" and_expr }
//   and_expr = cmp_expr { "&&" cmp_expr }
//   cmp_expr = field op literal | field "in" "[" literals "]"
//   field    = identifier        // 不支持 `.` 跨 level
//   op       = "==" | "!="
//   literal  = string | number | bool | null

export interface ShowWhenNode {
  kind: "cmp" | "in" | "and" | "or";
  field?: string;
  op?: "==" | "!=";
  literal?: unknown;
  literals?: unknown[];
  left?: ShowWhenNode;
  right?: ShowWhenNode;
}

export type JSONSchemaLike = Record<string, unknown>;

class Parser {
  private readonly src: string;
  private pos: number;

  constructor(src: string) {
    this.src = src;
    this.pos = 0;
  }

  getPos(): number {
    return this.pos;
  }
  getLen(): number {
    return this.src.length;
  }
  tailFromPos(): string {
    return this.src.slice(this.pos);
  }

  private peek(): string {
    if (this.pos >= this.src.length) return "";
    return this.src[this.pos] as string;
  }

  private skipWhitespace(): void {
    while (this.pos < this.src.length) {
      const c = this.src[this.pos] as string;
      if (c === " " || c === "\t" || c === "\n" || c === "\r") this.pos++;
      else break;
    }
  }

  private consume(s: string): boolean {
    if (this.src.startsWith(s, this.pos)) {
      this.pos += s.length;
      return true;
    }
    return false;
  }

  private consumeWord(s: string): boolean {
    this.skipWhitespace();
    if (!this.src.startsWith(s, this.pos)) return false;
    const end = this.pos + s.length;
    if (end < this.src.length) {
      const c = this.src[end] as string;
      if (isIdentContinue(c)) return false;
    }
    this.pos = end;
    return true;
  }

  parseOr(): ShowWhenNode {
    let left = this.parseAnd();
    while (true) {
      this.skipWhitespace();
      if (!this.consume("||")) break;
      this.skipWhitespace();
      const right = this.parseAnd();
      left = { kind: "or", left, right };
    }
    return left;
  }

  private parseAnd(): ShowWhenNode {
    let left = this.parseCmp();
    while (true) {
      this.skipWhitespace();
      if (!this.consume("&&")) break;
      this.skipWhitespace();
      const right = this.parseCmp();
      left = { kind: "and", left, right };
    }
    return left;
  }

  private parseCmp(): ShowWhenNode {
    this.skipWhitespace();
    if (this.peek() === "(") {
      throw new SyntaxError(
        `show_when: 括号嵌套不支持（spec-v1 §3.3），位置 ${this.pos}`
      );
    }

    const fieldName = this.parseIdentifier();
    if (fieldName.includes(".")) {
      throw new Error(
        `show_when: 跨 level 字段引用不支持 ('${fieldName}')`
      );
    }

    this.skipWhitespace();
    const c = this.peek();
    if (c === "+" || c === "-" || c === "*" || c === "/") {
      throw new Error(
        `show_when: 算术运算不支持 ('${c}' 位置 ${this.pos})`
      );
    }

    if (this.consumeWord("in")) {
      this.skipWhitespace();
      if (!this.consume("[")) {
        throw new Error(
          `show_when: 'in' 后应为 '['，位置 ${this.pos}`
        );
      }
      const literals = this.parseLiterals();
      this.skipWhitespace();
      if (!this.consume("]")) {
        throw new Error(
          `show_when: 'in' 列表未闭合，缺少 ']'，位置 ${this.pos}`
        );
      }
      return { kind: "in", field: fieldName, literals };
    }

    let op: "==" | "!=";
    if (this.consume("==")) op = "==";
    else if (this.consume("!=")) op = "!=";
    else {
      throw new Error(
        `show_when: 期望 '==' / '!=' / 'in'，位置 ${this.pos} ` +
          `(remaining='${this.src.slice(this.pos)}')`
      );
    }

    this.skipWhitespace();
    const literal = this.parseLiteral();
    return { kind: "cmp", field: fieldName, op, literal };
  }

  private parseIdentifier(): string {
    this.skipWhitespace();
    if (this.pos >= this.src.length) {
      throw new Error("show_when: 期望标识符，输入已结束");
    }
    const first = this.src[this.pos] as string;
    if (!isIdentStart(first)) {
      throw new Error(
        `show_when: 标识符首字符非法 '${first}' 位置 ${this.pos}`
      );
    }
    const start = this.pos;
    while (this.pos < this.src.length) {
      const c = this.src[this.pos] as string;
      if (isIdentContinue(c) || c === ".") this.pos++;
      else break;
    }
    return this.src.slice(start, this.pos);
  }

  private parseLiterals(): unknown[] {
    this.skipWhitespace();
    if (this.peek() === "]") {
      throw new Error(
        `show_when: 'in' 列表不可为空，位置 ${this.pos}`
      );
    }
    const out: unknown[] = [];
    while (true) {
      this.skipWhitespace();
      out.push(this.parseLiteral());
      this.skipWhitespace();
      if (!this.consume(",")) break;
    }
    return out;
  }

  private parseLiteral(): unknown {
    this.skipWhitespace();
    if (this.pos >= this.src.length) {
      throw new Error("show_when: 期望字面量，输入已结束");
    }
    const c = this.src[this.pos] as string;
    if (c === "'") return this.parseString();
    if (c === "t" || c === "f") return this.parseBool();
    if (c === "n") return this.parseNull();
    if (isDigit(c) || c === "-") return this.parseNumber();
    throw new Error(
      `show_when: 无法识别的字面量起始字符 '${c}' 位置 ${this.pos}`
    );
  }

  private parseString(): string {
    if (this.peek() !== "'") {
      throw new Error(
        `show_when: 字符串应以 ' 开头，位置 ${this.pos}`
      );
    }
    this.pos++;
    const start = this.pos;
    while (this.pos < this.src.length && this.src[this.pos] !== "'") {
      this.pos++;
    }
    if (this.pos >= this.src.length) {
      throw new Error(
        `show_when: 未闭合字符串，起始位置 ${start - 1}`
      );
    }
    const s = this.src.slice(start, this.pos);
    this.pos++;
    return s;
  }

  private parseBool(): boolean {
    if (this.consumeWord("true")) return true;
    if (this.consumeWord("false")) return false;
    throw new Error(`show_when: 期望 true/false，位置 ${this.pos}`);
  }

  private parseNull(): null {
    if (this.consumeWord("null")) return null;
    throw new Error(`show_when: 期望 null，位置 ${this.pos}`);
  }

  private parseNumber(): number {
    const start = this.pos;
    if (this.peek() === "-") this.pos++;
    if (
      this.pos >= this.src.length ||
      !isDigit(this.src[this.pos] as string)
    ) {
      throw new Error(
        `show_when: 数字字面量非法，位置 ${start}`
      );
    }
    while (
      this.pos < this.src.length &&
      isDigit(this.src[this.pos] as string)
    ) {
      this.pos++;
    }
    const raw = this.src.slice(start, this.pos);
    const n = Number(raw);
    if (!Number.isFinite(n)) {
      throw new Error(`show_when: 数字解析失败 '${raw}'`);
    }
    return n;
  }
}

function isIdentStart(c: string): boolean {
  return (
    (c >= "a" && c <= "z") || (c >= "A" && c <= "Z") || c === "_"
  );
}

function isIdentContinue(c: string): boolean {
  return isIdentStart(c) || (c >= "0" && c <= "9");
}

function isDigit(c: string): boolean {
  return c >= "0" && c <= "9";
}

function compileNode(n: ShowWhenNode): JSONSchemaLike {
  if (n.kind === "cmp") {
    const fieldName = n.field as string;
    const inner: JSONSchemaLike =
      n.op === "=="
        ? { const: n.literal }
        : { not: { const: n.literal } };
    return { properties: { [fieldName]: inner } };
  }
  if (n.kind === "in") {
    const fieldName = n.field as string;
    return {
      properties: {
        [fieldName]: { enum: [...(n.literals as unknown[])] },
      },
    };
  }
  if (n.kind === "and") {
    return {
      allOf: [
        compileNode(n.left as ShowWhenNode),
        compileNode(n.right as ShowWhenNode),
      ],
    };
  }
  return {
    anyOf: [
      compileNode(n.left as ShowWhenNode),
      compileNode(n.right as ShowWhenNode),
    ],
  };
}

/**
 * 把 show_when DSL 表达式编译为 JSON Schema if/then/else。
 * 返回单独的 ifThenElse 对象（不含 uiHidden — conformance 只比对编译产出的 schema）。
 */
export function compileShowWhen(
  expr: string,
  fieldName: string
): {
  if: JSONSchemaLike;
  then: { required: string[] };
  else: { properties: Record<string, false> };
} {
  const p = new Parser(expr);
  const node = p.parseOr();
  if (p.getPos() < p.getLen()) {
    const rem = p.tailFromPos();
    const trimmed = rem.replace(/^[ \t]+/, "");
    const firstCh = trimmed.length > 0 ? trimmed[0] : "";
    if (
      firstCh === "+" ||
      firstCh === "-" ||
      firstCh === "*" ||
      firstCh === "/"
    ) {
      throw new Error(
        `show_when: 算术运算不支持 (spec-v1 §3.3)，` +
          `位置 ${p.getPos()} 遇到 '${firstCh}'`
      );
    }
    throw new Error(
      `show_when: 表达式尾部存在未消费字符 (位置 ${p.getPos()}, 剩余 '${rem}')`
    );
  }
  const ifSchema = compileNode(node);
  return {
    if: ifSchema,
    then: { required: [fieldName] },
    else: { properties: { [fieldName]: false } },
  };
}
