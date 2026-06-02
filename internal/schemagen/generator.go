// Package schemagen 用 go/packages AST 解析 pinned ks-types 模块源码，
// 把结构体字段 + doc 注释生成 JSON Schema（draft 2020-12）。单一真值源=ks-types 注释。
package schemagen

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"reflect"
	"strings"

	"golang.org/x/tools/go/packages"
)

// 已知枚举类型 → 允许值。来源：ks-types apptypes.go / compliance.go 的常量。
// 仅对“字段类型即该命名枚举类型”的字段生效（如 Type AppType / Auth.Mode AuthMode /
// DecisionMode DecisionMode / Pricing.Type PricingType）；side_effect_level / execution_mode /
// backend.kind 在 ks-types 里是裸 string 字段，故不在此列、渲染为自由 string。
var knownEnums = map[string][]string{
	"AppType":      {"app", "squad", "agent", "skill"},
	"AuthMode":     {"none", "keystone_jwks", "static_bearer"},
	"DecisionMode": {"user_only", "user_authorized", "agent_autonomous"},
	"PricingType":  {"free", "paid", "freemium"},
}

// Generate 解析 modulePath 的源码，以 rootType 为根产出 JSON Schema 字节。
func Generate(modulePath, rootType string) ([]byte, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedDeps,
	}
	pkgs, err := packages.Load(cfg, modulePath)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", modulePath, err)
	}
	if len(pkgs) == 0 || len(pkgs[0].Syntax) == 0 {
		return nil, fmt.Errorf("模块 %s 无可解析源码（确认已 go get、模块缓存含源）", modulePath)
	}
	g := &gen{defs: map[string]any{}, structs: map[string]*ast.StructType{}, docs: map[string]string{}}
	g.collect(pkgs[0])
	if _, ok := g.structs[rootType]; !ok {
		return nil, fmt.Errorf("根类型 %s 未在模块内找到", rootType)
	}
	g.build(rootType)
	schema := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$ref":    "#/$defs/" + rootType,
		"$defs":   g.defs,
	}
	return json.MarshalIndent(schema, "", "  ")
}

type gen struct {
	defs    map[string]any
	structs map[string]*ast.StructType
	docs    map[string]string // "Struct.Field" → doc 注释（已清洗）
}

// collect 遍历 AST，登记所有结构体定义 + 字段 doc 注释。
func (g *gen) collect(pkg *packages.Package) {
	for _, f := range pkg.Syntax {
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return true
			}
			g.structs[ts.Name.Name] = st
			for _, field := range st.Fields.List {
				if len(field.Names) == 0 || field.Doc == nil {
					continue
				}
				g.docs[ts.Name.Name+"."+field.Names[0].Name] = cleanDoc(field.Doc.Text())
			}
			return true
		})
	}
}

// build 递归把结构体渲染进 $defs。
func (g *gen) build(name string) {
	if _, done := g.defs[name]; done {
		return
	}
	st := g.structs[name]
	if st == nil {
		return
	}
	props := map[string]any{}
	var required []string
	g.defs[name] = map[string]any{"type": "object", "properties": props} // 占位防环
	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			continue // 跳过嵌入字段（按需扩展）
		}
		fname := field.Names[0].Name
		tag := ""
		if field.Tag != nil {
			tag = reflect.StructTag(strings.Trim(field.Tag.Value, "`")).Get("yaml")
		}
		yamlName, opts := splitTag(tag)
		if yamlName == "" || yamlName == "-" {
			continue
		}
		prop := g.typeToSchema(field.Type)
		if doc, ok := g.docs[name+"."+fname]; ok {
			prop["description"] = doc
		}
		props[yamlName] = prop
		if !opts.omitempty {
			required = append(required, yamlName)
		}
	}
	def := g.defs[name].(map[string]any)
	if len(required) > 0 {
		def["required"] = required
	}
}

// typeToSchema 把 AST 类型表达式映射到 schema 片段（含已知枚举、嵌套 struct $ref、数组、map）。
func (g *gen) typeToSchema(expr ast.Expr) map[string]any {
	switch t := expr.(type) {
	case *ast.Ident:
		if vals, ok := knownEnums[t.Name]; ok {
			return map[string]any{"type": "string", "enum": vals}
		}
		switch t.Name {
		case "string":
			return map[string]any{"type": "string"}
		case "int", "int64":
			return map[string]any{"type": "integer"}
		case "bool":
			return map[string]any{"type": "boolean"}
		}
		if _, ok := g.structs[t.Name]; ok {
			g.build(t.Name)
			return map[string]any{"$ref": "#/$defs/" + t.Name}
		}
		return map[string]any{} // 未知命名类型（如 LocalizedString）→ 宽松
	case *ast.StarExpr:
		return g.typeToSchema(t.X)
	case *ast.ArrayType:
		return map[string]any{"type": "array", "items": g.typeToSchema(t.Elt)}
	case *ast.MapType:
		return map[string]any{"type": "object"}
	case *ast.SelectorExpr:
		return map[string]any{} // 外部包类型 → 宽松
	default:
		return map[string]any{}
	}
}

type tagOpts struct{ omitempty bool }

func splitTag(tag string) (string, tagOpts) {
	parts := strings.Split(tag, ",")
	o := tagOpts{}
	for _, p := range parts[1:] {
		if p == "omitempty" {
			o.omitempty = true
		}
	}
	if len(parts) == 0 {
		return "", o
	}
	return parts[0], o
}

// cleanDoc 把多行 doc 注释压成单行 description（去换行、压空格）。
func cleanDoc(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
