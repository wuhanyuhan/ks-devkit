package ksconfig

import (
	"fmt"
	"strconv"
	"strings"
)

// TagSpec 承载 ksconfig:"..." struct tag 中声明的所有字段约束与 UI 元信息。
// tag 语法规范源：config schema tag contract。
type TagSpec struct {
	Required   bool
	Type       string // password / textarea / radio / select 等
	Label      string
	LabelZH    string
	LabelEN    string
	Hint       string
	Default    string // 字符串形式，反射时再按字段类型 parse
	Min        *int64
	Max        *int64
	MinLen     *int64
	MaxLen     *int64
	Pattern    string
	Enum       []string
	Sensitive  bool
	ItemSchema string // 数组元素引用的类型名（L1 数组场景）
	ShowWhen   string // show_when DSL 字符串；reflect 时由 CompileShowWhen 编译为 JSON Schema + UI Schema 片段
}

// ParseTag 将单条 ksconfig struct tag 解析为 TagSpec。
//
// 语法：`,` 分隔多条规则；`:` 分隔 `key:value`；`|` 在 enum 值内表 OR。
// 空字符串返回零值 TagSpec + nil error。
//
// 错误处理区分：
//   - programmer error（启动期快速失败）：未知 key、label/hint 含非法字符 → panic
//   - 运行时可恢复错误：min / max / minLen / maxLen 非整数 → 返回 error
func ParseTag(tag string) (TagSpec, error) {
	var spec TagSpec
	if tag == "" {
		return spec, nil
	}
	for _, part := range strings.Split(tag, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		idx := strings.IndexByte(part, ':')
		var key, val string
		if idx == -1 {
			key = part
		} else {
			key = part[:idx]
			val = part[idx+1:]
		}
		switch key {
		case "required":
			if val != "" {
				panic(fmt.Sprintf("ksconfig: required 是 bool flag，不接值 (tag = %q) — 去掉 %q 后面的 :...", tag, "required"))
			}
			spec.Required = true
		case "sensitive":
			if val != "" {
				panic(fmt.Sprintf("ksconfig: sensitive 是 bool flag，不接值 (tag = %q)", tag))
			}
			spec.Sensitive = true
		case "type":
			spec.Type = val
		case "label":
			if strings.ContainsAny(val, ",:|") {
				panic(fmt.Sprintf("ksconfig: label 值含非法字符 (,|:), 收到 %q — 改走 L2 yaml 声明", val))
			}
			spec.Label = val
		case "label_zh":
			if strings.ContainsAny(val, ",:|") {
				panic(fmt.Sprintf("ksconfig: label_zh 值含非法字符 (,|:), 收到 %q — 改走 L2 yaml 声明", val))
			}
			spec.LabelZH = val
		case "label_en":
			if strings.ContainsAny(val, ",:|") {
				panic(fmt.Sprintf("ksconfig: label_en 值含非法字符 (,|:), 收到 %q — 改走 L2 yaml 声明", val))
			}
			spec.LabelEN = val
		case "hint":
			if strings.ContainsAny(val, ",:|") {
				panic(fmt.Sprintf("ksconfig: hint 值含非法字符 (,|:), 收到 %q", val))
			}
			spec.Hint = val
		case "default":
			spec.Default = val
		case "min":
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return spec, fmt.Errorf("ksconfig: min 非整数: %q", val)
			}
			spec.Min = &n
		case "max":
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return spec, fmt.Errorf("ksconfig: max 非整数: %q", val)
			}
			spec.Max = &n
		case "minLen":
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return spec, fmt.Errorf("ksconfig: minLen 非整数: %q", val)
			}
			spec.MinLen = &n
		case "maxLen":
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return spec, fmt.Errorf("ksconfig: maxLen 非整数: %q", val)
			}
			spec.MaxLen = &n
		case "pattern":
			spec.Pattern = val
		case "enum":
			if val == "" {
				panic(fmt.Sprintf("ksconfig: enum 值不能为空 (tag = %q)", tag))
			}
			parts := strings.Split(val, "|")
			for _, p := range parts {
				if p == "" {
					panic(fmt.Sprintf("ksconfig: enum 含空元素 (tag = %q) — 末尾或连续 | 都非法", tag))
				}
			}
			spec.Enum = parts
		case "item_schema":
			spec.ItemSchema = val
		case "show_when":
			spec.ShowWhen = val
		default:
			panic(fmt.Sprintf("ksconfig: 未知 tag key %q (tag = %q) — 支持的 key 见 protocol-design.md §5.2", key, tag))
		}
	}
	return spec, nil
}
