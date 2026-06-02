package ksconfig

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// JSONSchema 是生成的 JSON Schema（draft 2020-12 子集）；UISchema 是 rjsf 消费的 UI Schema。
type JSONSchema = map[string]any
type UISchema = map[string]any

// ReflectConfigSchema 反射 T 的 struct 字段，生成 JSON Schema + UI Schema。
//
// sub-struct / slice-of-struct 的内部字段 UI Schema 与 show_when
// 产出的 allOf 均会向上透传（items.allOf / items.<child> / <parent>.<child>）。
// 注意：敏感字段（password / sensitive）建议放在顶层 struct 内，嵌套内部的
// password widget 需前端渲染层对齐支持。
//
// 规则：
//   - 字段名：优先 json:"x"；否则 PascalCase → snake_case（连续大写视一个词）
//   - required：tag 含 `required` 的字段加入 schema.required
//   - 类型：string / int / bool / []T / struct T（嵌套对象）
//   - UI Schema：type:password → ui:widget=password；类似 textarea；hint → ui:help；sensitive → ks:sensitive
//   - show_when（含嵌套透传）：
//   - JSON Schema 侧 → schema.allOf[] 追加 CompileShowWhen 返回的 if/then/else 片段
//   - UI Schema 侧   → ui[fieldName]["ui:show_when"] 注入 uiShowWhen（rjsf widget 消费）
//   - slice-of-struct：sub allOf 透传到 items.allOf；sub UI 透传到 ui[field].items
//   - sub-struct：sub allOf 透传到 field.allOf；sub UI 合并到 ui[field]
func ReflectConfigSchema[T any]() (JSONSchema, UISchema, error) {
	var zero T
	t := reflect.TypeOf(zero)
	if t.Kind() != reflect.Struct {
		return nil, nil, fmt.Errorf("ReflectConfigSchema: T 必须是 struct 类型，收到 %v", t.Kind())
	}
	props, uiProps, requiredList, allOfFragments, err := reflectStructFields(t)
	if err != nil {
		return nil, nil, err
	}
	schema := JSONSchema{
		"type":       "object",
		"properties": props,
	}
	if len(requiredList) > 0 {
		schema["required"] = requiredList
	}
	if len(allOfFragments) > 0 {
		schema["allOf"] = allOfFragments
	}
	return schema, uiProps, nil
}

// reflectStructFields 反射单层 struct 的字段，返回 (props, uiProps, required,
// allOfFragments, err) 五元组。allOf 片段由 show_when 字段产生；单层 struct 不含
// show_when 字段时 allOfFragments 为 nil（ReflectConfigSchema 据此决定是否塞入 schema）。
// 调用方 (buildFieldSchema 的 Slice / Struct 分支) 负责把这些返回值
// 向父级 schema / uiSchema 透传，不应再丢弃第 2/第 4 返回值。
func reflectStructFields(t reflect.Type) (map[string]any, map[string]any, []string, []map[string]any, error) {
	props := map[string]any{}
	uiProps := map[string]any{}
	var requiredList []string
	var allOfFragments []map[string]any
	var uiOrder []any

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		ksTag := f.Tag.Get("ksconfig")
		jsonTag := f.Tag.Get("json")

		tagSpec, err := ParseTag(ksTag)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("字段 %s: %w", f.Name, err)
		}

		name := jsonTagName(jsonTag, f.Name)
		uiOrder = append(uiOrder, name)

		fieldSchema, fieldUI, err := buildFieldSchema(f.Type, tagSpec)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("字段 %s: %w", f.Name, err)
		}
		props[name] = fieldSchema
		if len(fieldUI) > 0 {
			uiProps[name] = fieldUI
		}
		if tagSpec.Required {
			requiredList = append(requiredList, name)
		}

		// show_when 双管道
		if tagSpec.ShowWhen != "" {
			ifThenElse, uiShowWhen, err := CompileShowWhen(tagSpec.ShowWhen, name)
			if err != nil {
				return nil, nil, nil, nil, fmt.Errorf("字段 %s show_when 编译失败: %w", f.Name, err)
			}
			allOfFragments = append(allOfFragments, ifThenElse)
			// UI Schema 侧：约定 key "ui:show_when"（条件成立时显示；对齐 show_when tag 语义）
			if uiProps[name] == nil {
				uiProps[name] = map[string]any{}
			}
			uiProps[name].(map[string]any)["ui:show_when"] = uiShowWhen
		}
	}
	if len(uiOrder) > 0 {
		uiProps["ui:order"] = uiOrder
	}
	return props, uiProps, requiredList, allOfFragments, nil
}

func buildFieldSchema(ft reflect.Type, spec TagSpec) (map[string]any, map[string]any, error) {
	schema := map[string]any{}
	ui := map[string]any{}

	switch ft.Kind() {
	case reflect.String:
		schema["type"] = "string"
		if spec.MinLen != nil {
			schema["minLength"] = *spec.MinLen
		}
		if spec.MaxLen != nil {
			schema["maxLength"] = *spec.MaxLen
		}
		if spec.Pattern != "" {
			schema["pattern"] = spec.Pattern
		}
		if len(spec.Enum) > 0 {
			enum := make([]any, len(spec.Enum))
			for i, v := range spec.Enum {
				enum[i] = v
			}
			schema["enum"] = enum
		}
		if spec.Default != "" {
			schema["default"] = spec.Default
		}
		switch {
		case spec.Type == "password":
			ui["ui:widget"] = "password"
		case spec.Type == "textarea":
			ui["ui:widget"] = "textarea"
		case len(spec.Enum) > 0:
			ui["ui:widget"] = "select"
		}
	case reflect.Int, reflect.Int64:
		schema["type"] = "integer"
		if spec.Min != nil {
			schema["minimum"] = *spec.Min
		}
		if spec.Max != nil {
			schema["maximum"] = *spec.Max
		}
		if spec.Default != "" {
			n, err := strconv.ParseInt(spec.Default, 10, 64)
			if err != nil {
				return nil, nil, fmt.Errorf("ksconfig: 字段 default 不是合法整数: %q (字段类型 %v)", spec.Default, ft.Kind())
			}
			schema["default"] = n
		}
	case reflect.Bool:
		schema["type"] = "boolean"
		switch spec.Default {
		case "true":
			schema["default"] = true
		case "false":
			schema["default"] = false
		}
	case reflect.Slice:
		itemSchema := map[string]any{"type": "string"} // MVP 默认
		if spec.ItemSchema != "" {
			// 延迟解析：引用其他 struct；SDK 在注册完成后二次扫描
			itemSchema = map[string]any{"$ref": "#/definitions/" + spec.ItemSchema}
		} else if ft.Elem().Kind() == reflect.Struct {
			// sub-struct 的 allOf 透传到 items.allOf；
			// sub-UI 透传到父级 UI Schema 的 items 层（rjsf array 约定）。
			subProps, subUI, subReq, subAllOf, err := reflectStructFields(ft.Elem())
			if err != nil {
				return nil, nil, err
			}
			itemSchema = map[string]any{
				"type":       "object",
				"properties": subProps,
			}
			if len(subReq) > 0 {
				itemSchema["required"] = subReq
			}
			if len(subAllOf) > 0 {
				itemSchema["allOf"] = subAllOf
			}
			if len(subUI) > 0 {
				// rjsf array UI schema 约定：ui["<field>"]["items"] 持有 item 级 UI map。
				ui["items"] = subUI
			}
		}
		schema["type"] = "array"
		schema["items"] = itemSchema
	case reflect.Struct:
		// sub-struct 的 allOf 透传到父字段 schema；
		// sub-UI 直接合并到父字段的 UI map（rjsf object 约定：uiSchema 形态与 schema 一致）。
		subProps, subUI, subReq, subAllOf, err := reflectStructFields(ft)
		if err != nil {
			return nil, nil, err
		}
		schema["type"] = "object"
		schema["properties"] = subProps
		if len(subReq) > 0 {
			schema["required"] = subReq
		}
		if len(subAllOf) > 0 {
			schema["allOf"] = subAllOf
		}
		if len(subUI) > 0 {
			// 合并 sub UI 键（形如 "<child_field>": {...}）到父 ui map
			for k, v := range subUI {
				ui[k] = v
			}
		}
	default:
		return nil, nil, fmt.Errorf("不支持的字段类型: %v", ft.Kind())
	}

	if label := spec.effectiveLabel(); label != "" {
		schema["title"] = label
		ui["ui:label"] = label
	}
	if spec.LabelZH != "" || spec.LabelEN != "" {
		labelI18n := map[string]string{}
		if spec.LabelZH != "" {
			labelI18n["zh-CN"] = spec.LabelZH
		}
		if spec.LabelEN != "" {
			labelI18n["en-US"] = spec.LabelEN
		}
		ui["ks:label_i18n"] = labelI18n
	}
	if spec.Hint != "" {
		ui["ui:help"] = spec.Hint
	}
	if spec.Sensitive {
		ui["ks:sensitive"] = true
	}
	return schema, ui, nil
}

func (spec TagSpec) effectiveLabel() string {
	if spec.LabelZH != "" {
		return spec.LabelZH
	}
	return spec.Label
}

// jsonTagName 按优先级决定字段 JSON 名：
//  1. json:"custom" → 用 custom
//  2. 否则 PascalCase → snake_case（缩写组合并）
func jsonTagName(jsonTag, goName string) string {
	if jsonTag != "" && jsonTag != "-" {
		parts := strings.SplitN(jsonTag, ",", 2)
		if parts[0] != "" {
			return parts[0]
		}
	}
	return pascalToSnake(goName)
}

// pascalToSnake 处理缩写组：连续大写视一个词
//
//	MiniMaxAPIKey → mini_max_api_key
//	UserID → user_id
//	HTTPHost → http_host
var (
	reFirstCap = regexp.MustCompile(`(.)([A-Z][a-z]+)`)
	reAllCap   = regexp.MustCompile(`([a-z0-9])([A-Z])`)
)

func pascalToSnake(s string) string {
	s = reFirstCap.ReplaceAllString(s, `${1}_${2}`)
	s = reAllCap.ReplaceAllString(s, `${1}_${2}`)
	return strings.ToLower(s)
}
