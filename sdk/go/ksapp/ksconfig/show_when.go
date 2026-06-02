package ksconfig

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// show_when DSL 解析器（手写递归下降 parser）+ JSON Schema 编译器。
//
// DSL 语法（EBNF）：
//
//	expr       = or_expr
//	or_expr    = and_expr { "||" and_expr }
//	and_expr   = cmp_expr { "&&" cmp_expr }
//	cmp_expr   = field op literal | field "in" "[" literals "]"
//	field      = identifier           // 不支持 `.` 跨 level
//	op         = "==" | "!="
//	literal    = string | number | bool | null
//
// 禁止规则：
//   - 括号嵌套 → panic（programmer error）
//   - 跨 level 字段引用 / 算术运算 / 未闭合字符串 → error

// nodeKind 区分 AST 节点的 4 种形态。
type nodeKind int

const (
	nodeOr nodeKind = iota
	nodeAnd
	nodeCmp
	nodeIn
)

// showWhenNode 是 show_when 表达式的 AST 节点。
// 一个节点只有一种形态（由 kind 决定使用哪些字段）。
type showWhenNode struct {
	kind     nodeKind
	field    string        // cmp / in 专用
	op       string        // cmp 专用："==" 或 "!="
	literal  any           // cmp 专用：string / int64 / bool / nil
	literals []any         // in 专用
	left     *showWhenNode // and / or 专用
	right    *showWhenNode // and / or 专用
}

// CompileShowWhen 把 show_when 表达式字符串编译为 JSON Schema if/then/else 结构
// 和 UI Schema ui:show_when。
//
// 返回值形态：
//   - ifThenElse: {if:..., then:..., else:...} — **调用方负责把它包进 allOf**。
//     reflect.go 生成字段级 schema 时会做：
//     {... properties:{fieldX:{...}}, allOf:[{if,then,else}]}
//   - uiShowWhen: rjsf 自定义 widget 求值用的 AST 副本（cmp/in/logical 三种 shape）
//   - err: parse 错（跨 level / 算术 / 未闭合字符串 等可恢复错）
//
// panic 条件（programmer error）：
//   - 括号嵌套 `(a || b) && c`
//
// 实现细节注释：
//   - compileNode 产出的 allOf/anyOf 子数组类型为 []any（弱类型），
//     为了统一 JSON 编码；consumer 如需 type-assert 请用 .([]any)。
//
// show_when DSL 规范源：
// Keystone config schema spec v1
func CompileShowWhen(expr string, fieldName string) (map[string]any, map[string]any, error) {
	p := &parser{src: expr, pos: 0}
	p.skipWhitespace()
	node, err := p.parseOr()
	if err != nil {
		return nil, nil, err
	}
	p.skipWhitespace()
	if p.pos < len(p.src) {
		rem := p.src[p.pos:]
		// 尾部若以算术运算符起始，分类报 "算术运算不支持"，与 parseCmp 里
		// field 后遇到 +/-/*// 的处理保持一致。
		trimmed := strings.TrimLeft(rem, " \t")
		if len(trimmed) > 0 {
			switch trimmed[0] {
			case '+', '-', '*', '/':
				return nil, nil, fmt.Errorf("show_when: 算术运算不支持 (spec-v1 §3.3)，位置 %d 遇到 %q", p.pos, trimmed[:1])
			}
		}
		return nil, nil, fmt.Errorf("show_when: 表达式尾部存在未消费字符 (位置 %d, 剩余 %q)", p.pos, rem)
	}

	ifSchema := compileNode(node)
	ifThenElse := map[string]any{
		"if":   ifSchema,
		"then": map[string]any{"required": []any{fieldName}},
		"else": map[string]any{"properties": map[string]any{fieldName: false}},
	}
	uiShowWhen := buildUIShowWhen(node)
	return ifThenElse, uiShowWhen, nil
}

// parser 是递归下降 parser 的状态。
type parser struct {
	src string
	pos int
}

// peek 返回当前位置的字符（若越界返回 0）。
func (p *parser) peek() byte {
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

// skipWhitespace 跳过空白（含 tab / 换行）。
func (p *parser) skipWhitespace() {
	for p.pos < len(p.src) && unicode.IsSpace(rune(p.src[p.pos])) {
		p.pos++
	}
}

// consume 若当前位置以 s 为前缀则消费并返回 true，否则 false。
func (p *parser) consume(s string) bool {
	if strings.HasPrefix(p.src[p.pos:], s) {
		p.pos += len(s)
		return true
	}
	return false
}

// consumeWord 与 consume 类似，但仅当匹配的 s 之后不是 identifier 续字符
// （字母/数字/下划线）时才成功。用于关键字 in/true/false/null，避免把
// 以 in 开头的字段名（inbox / interval）或 trueFlag / nullFoo 之类的
// identifier 误识别为关键字。
func (p *parser) consumeWord(s string) bool {
	p.skipWhitespace()
	if !strings.HasPrefix(p.src[p.pos:], s) {
		return false
	}
	end := p.pos + len(s)
	if end < len(p.src) {
		c := p.src[end]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			return false
		}
	}
	p.pos = end
	return true
}

// parseOr：or_expr = and_expr { "||" and_expr }
func (p *parser) parseOr() (*showWhenNode, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for {
		p.skipWhitespace()
		if !p.consume("||") {
			break
		}
		p.skipWhitespace()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &showWhenNode{kind: nodeOr, left: left, right: right}
	}
	return left, nil
}

// parseAnd：and_expr = cmp_expr { "&&" cmp_expr }
func (p *parser) parseAnd() (*showWhenNode, error) {
	left, err := p.parseCmp()
	if err != nil {
		return nil, err
	}
	for {
		p.skipWhitespace()
		if !p.consume("&&") {
			break
		}
		p.skipWhitespace()
		right, err := p.parseCmp()
		if err != nil {
			return nil, err
		}
		left = &showWhenNode{kind: nodeAnd, left: left, right: right}
	}
	return left, nil
}

// parseCmp：cmp_expr = field op literal | field "in" "[" literals "]"
//
// 括号嵌套直接 panic（programmer error）。
func (p *parser) parseCmp() (*showWhenNode, error) {
	p.skipWhitespace()
	// 括号嵌套不支持
	if p.peek() == '(' {
		panic(fmt.Sprintf("show_when: 括号嵌套不支持（spec-v1 §3.3），位置 %d", p.pos))
	}

	field, err := p.parseIdentifier()
	if err != nil {
		return nil, err
	}
	if strings.Contains(field, ".") {
		return nil, fmt.Errorf("show_when: 跨 level 字段引用不支持 (%q)", field)
	}

	p.skipWhitespace()
	// 检查算术运算：+ - * / 出现在 field 后
	if c := p.peek(); c == '+' || c == '-' || c == '*' || c == '/' {
		return nil, fmt.Errorf("show_when: 算术运算不支持 (%q 位置 %d)", string(c), p.pos)
	}

	// "in" 运算符（word，需 word-boundary 避免吃掉以 "in" 开头的字段名）
	if p.consumeWord("in") {
		p.skipWhitespace()
		if !p.consume("[") {
			return nil, fmt.Errorf("show_when: 'in' 后应为 '['，位置 %d", p.pos)
		}
		literals, err := p.parseLiterals()
		if err != nil {
			return nil, err
		}
		p.skipWhitespace()
		if !p.consume("]") {
			return nil, fmt.Errorf("show_when: 'in' 列表未闭合，缺少 ']'，位置 %d", p.pos)
		}
		return &showWhenNode{kind: nodeIn, field: field, literals: literals}, nil
	}

	// "==" / "!=" 运算符
	var op string
	if p.consume("==") {
		op = "=="
	} else if p.consume("!=") {
		op = "!="
	} else {
		return nil, fmt.Errorf("show_when: 期望 '==' / '!=' / 'in'，位置 %d (remaining=%q)", p.pos, p.src[p.pos:])
	}

	p.skipWhitespace()
	lit, err := p.parseLiteral()
	if err != nil {
		return nil, err
	}
	return &showWhenNode{kind: nodeCmp, field: field, op: op, literal: lit}, nil
}

// parseIdentifier 读取一个标识符（含可能的 `.`；`.` 后续会被 parseCmp 用 error 拒绝）。
// 合法字符：字母 / 数字 / 下划线 / 点；首字符必须为字母或下划线。
func (p *parser) parseIdentifier() (string, error) {
	p.skipWhitespace()
	start := p.pos
	if p.pos >= len(p.src) {
		return "", fmt.Errorf("show_when: 期望标识符，输入已结束")
	}
	c := p.src[p.pos]
	if !(isLetter(c) || c == '_') {
		return "", fmt.Errorf("show_when: 标识符首字符非法 %q 位置 %d", string(c), p.pos)
	}
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if isLetter(c) || isDigit(c) || c == '_' || c == '.' {
			p.pos++
		} else {
			break
		}
	}
	if p.pos == start {
		return "", fmt.Errorf("show_when: 期望标识符，位置 %d", p.pos)
	}
	return p.src[start:p.pos], nil
}

// parseLiterals 读取 "in [...]" 中的多个 literal（逗号分隔）。
func (p *parser) parseLiterals() ([]any, error) {
	var out []any
	p.skipWhitespace()
	// 空列表不允许
	if p.peek() == ']' {
		return nil, fmt.Errorf("show_when: 'in' 列表不可为空，位置 %d", p.pos)
	}
	for {
		p.skipWhitespace()
		lit, err := p.parseLiteral()
		if err != nil {
			return nil, err
		}
		out = append(out, lit)
		p.skipWhitespace()
		if !p.consume(",") {
			break
		}
	}
	return out, nil
}

// parseLiteral 按首字符分派到具体 literal 解析器。
func (p *parser) parseLiteral() (any, error) {
	p.skipWhitespace()
	if p.pos >= len(p.src) {
		return nil, fmt.Errorf("show_when: 期望字面量，输入已结束")
	}
	c := p.src[p.pos]
	switch {
	case c == '\'':
		return p.parseString()
	case c == 't' || c == 'f':
		return p.parseBool()
	case c == 'n':
		return p.parseNull()
	case isDigit(c) || c == '-':
		return p.parseNumber()
	default:
		return nil, fmt.Errorf("show_when: 无法识别的字面量起始字符 %q 位置 %d", string(c), p.pos)
	}
}

// parseString 读取单引号字符串（MVP：禁转义）。
func (p *parser) parseString() (string, error) {
	if p.peek() != '\'' {
		return "", fmt.Errorf("show_when: 字符串应以 ' 开头，位置 %d", p.pos)
	}
	p.pos++ // consume 开头 '
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != '\'' {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return "", fmt.Errorf("show_when: 未闭合字符串，起始位置 %d", start-1)
	}
	s := p.src[start:p.pos]
	p.pos++ // consume 结尾 '
	return s, nil
}

// parseBool 读取 true/false（word-boundary，避免 trueFlag / falseX 被吃掉）。
func (p *parser) parseBool() (bool, error) {
	if p.consumeWord("true") {
		return true, nil
	}
	if p.consumeWord("false") {
		return false, nil
	}
	return false, fmt.Errorf("show_when: 期望 true/false，位置 %d", p.pos)
}

// parseNull 读取 null（word-boundary，避免 nullFoo 被吃掉）。
func (p *parser) parseNull() (any, error) {
	if p.consumeWord("null") {
		return nil, nil
	}
	return nil, fmt.Errorf("show_when: 期望 null，位置 %d", p.pos)
}

// parseNumber 读取整数字面量，返回 int64。支持前导负号。
func (p *parser) parseNumber() (int64, error) {
	start := p.pos
	if p.peek() == '-' {
		p.pos++
	}
	if p.pos >= len(p.src) || !isDigit(p.src[p.pos]) {
		return 0, fmt.Errorf("show_when: 数字字面量非法，位置 %d", start)
	}
	for p.pos < len(p.src) && isDigit(p.src[p.pos]) {
		p.pos++
	}
	n, err := strconv.ParseInt(p.src[start:p.pos], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("show_when: 数字解析失败 %q: %v", p.src[start:p.pos], err)
	}
	return n, nil
}

// compileNode 把 AST 递归编译为 JSON Schema `if` 子树。
func compileNode(n *showWhenNode) map[string]any {
	switch n.kind {
	case nodeCmp:
		var inner map[string]any
		if n.op == "==" {
			inner = map[string]any{"const": n.literal}
		} else {
			// !=
			inner = map[string]any{"not": map[string]any{"const": n.literal}}
		}
		return map[string]any{
			"properties": map[string]any{n.field: inner},
		}
	case nodeIn:
		enum := make([]any, len(n.literals))
		copy(enum, n.literals)
		return map[string]any{
			"properties": map[string]any{
				n.field: map[string]any{"enum": enum},
			},
		}
	case nodeAnd:
		return map[string]any{
			"allOf": []any{compileNode(n.left), compileNode(n.right)},
		}
	case nodeOr:
		return map[string]any{
			"anyOf": []any{compileNode(n.left), compileNode(n.right)},
		}
	}
	// 防御性（不应到达）
	return map[string]any{}
}

// buildUIShowWhen 从 AST 根节点构造 rjsf ui:show_when 对象。
//
// 形态（供 Python SDK / TS 前端镜像复用）：
//   - cmp:    {field, op, value, negate: false}
//   - in:     {field, op: "in", values, negate: false}
//   - and/or: {logical: {kind: "and"/"or", left, right}}
//
// 选择嵌套 {logical:{kind,left,right}} 而非平铺 {op,args}：rjsf widget 递归消费
// 每层同 key 的嵌套结构更自然；canonical 示例里也有 `negate` 字段，
// 说明协议认可这个字段。
func buildUIShowWhen(n *showWhenNode) map[string]any {
	switch n.kind {
	case nodeCmp:
		return map[string]any{
			"field":  n.field,
			"op":     n.op,
			"value":  n.literal,
			"negate": false,
		}
	case nodeIn:
		vals := make([]any, len(n.literals))
		copy(vals, n.literals)
		return map[string]any{
			"field":  n.field,
			"op":     "in",
			"values": vals,
			"negate": false,
		}
	case nodeAnd, nodeOr:
		kind := "and"
		if n.kind == nodeOr {
			kind = "or"
		}
		return map[string]any{
			"logical": map[string]any{
				"kind":  kind,
				"left":  buildUIShowWhen(n.left),
				"right": buildUIShowWhen(n.right),
			},
		}
	}
	return nil
}

func isLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
