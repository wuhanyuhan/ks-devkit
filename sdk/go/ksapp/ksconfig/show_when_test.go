package ksconfig

import (
	"reflect"
	"strings"
	"testing"
)

// TestCompileShowWhen_SimpleEquality：backend == 'github'
func TestCompileShowWhen_SimpleEquality(t *testing.T) {
	t.Parallel()
	got, ui, err := CompileShowWhen("backend == 'github'", "github_token")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := map[string]any{
		"if": map[string]any{
			"properties": map[string]any{
				"backend": map[string]any{"const": "github"},
			},
		},
		"then": map[string]any{"required": []any{"github_token"}},
		"else": map[string]any{"properties": map[string]any{"github_token": false}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("if/then/else mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
	if ui == nil {
		t.Fatalf("uiShowWhen should not be nil")
	}
	// C1：ui:show_when 的 cmp shape = {field, op, value, negate:false}
	if ui["field"] != "backend" || ui["op"] != "==" {
		t.Errorf("ui_show_when field/op 错: %v", ui)
	}
	if ui["value"] != "github" {
		t.Errorf("ui_show_when value 应为 github, 收到 %v", ui["value"])
	}
	if ui["negate"] != false {
		t.Errorf("ui_show_when negate 应为 false, 收到 %v", ui["negate"])
	}
}

// TestCompileShowWhen_Inequality：type != 'github'
func TestCompileShowWhen_Inequality(t *testing.T) {
	t.Parallel()
	got, _, err := CompileShowWhen("type != 'github'", "base_url")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := map[string]any{
		"if": map[string]any{
			"properties": map[string]any{
				"type": map[string]any{"not": map[string]any{"const": "github"}},
			},
		},
		"then": map[string]any{"required": []any{"base_url"}},
		"else": map[string]any{"properties": map[string]any{"base_url": false}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

// TestCompileShowWhen_InList：region in ['cn','us','eu']
func TestCompileShowWhen_InList(t *testing.T) {
	t.Parallel()
	got, _, err := CompileShowWhen("region in ['cn','us','eu']", "currency")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	ifNode := got["if"].(map[string]any)
	props := ifNode["properties"].(map[string]any)
	region := props["region"].(map[string]any)
	enum, ok := region["enum"].([]any)
	if !ok {
		t.Fatalf("region.enum should be []any, got %T", region["enum"])
	}
	want := []any{"cn", "us", "eu"}
	if !reflect.DeepEqual(enum, want) {
		t.Errorf("enum mismatch: got %#v, want %#v", enum, want)
	}
}

// TestCompileShowWhen_And：backend == 'github' && enabled == true
func TestCompileShowWhen_And(t *testing.T) {
	t.Parallel()
	got, _, err := CompileShowWhen("backend == 'github' && enabled == true", "github_token")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	ifNode := got["if"].(map[string]any)
	allOf, ok := ifNode["allOf"].([]any)
	if !ok {
		t.Fatalf("if.allOf should be []any, got %T", ifNode["allOf"])
	}
	if len(allOf) != 2 {
		t.Fatalf("allOf length expected 2, got %d", len(allOf))
	}
	// 第 1 个子句：backend == 'github'
	first := allOf[0].(map[string]any)
	firstProps := first["properties"].(map[string]any)
	backend := firstProps["backend"].(map[string]any)
	if backend["const"] != "github" {
		t.Errorf("allOf[0].backend.const = %v, want 'github'", backend["const"])
	}
	// 第 2 个子句：enabled == true
	second := allOf[1].(map[string]any)
	secondProps := second["properties"].(map[string]any)
	enabled := secondProps["enabled"].(map[string]any)
	if enabled["const"] != true {
		t.Errorf("allOf[1].enabled.const = %v, want true", enabled["const"])
	}
}

// TestCompileShowWhen_Or：region == 'cn' || region == 'us'
func TestCompileShowWhen_Or(t *testing.T) {
	t.Parallel()
	got, _, err := CompileShowWhen("region == 'cn' || region == 'us'", "locale")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	ifNode := got["if"].(map[string]any)
	anyOf, ok := ifNode["anyOf"].([]any)
	if !ok {
		t.Fatalf("if.anyOf should be []any, got %T", ifNode["anyOf"])
	}
	if len(anyOf) != 2 {
		t.Fatalf("anyOf length expected 2, got %d", len(anyOf))
	}
}

// TestCompileShowWhen_RejectParenthesis：括号嵌套 → panic
func TestCompileShowWhen_RejectParenthesis(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic for parenthesis, got none")
		}
		msg, ok := r.(string)
		if !ok {
			// panic 值可能是 error 或其他类型
			t.Logf("panic value type: %T, value: %v", r, r)
			return
		}
		if !strings.Contains(msg, "spec-v1 §3.3") {
			t.Errorf("panic message should contain 'spec-v1 §3.3', got %q", msg)
		}
	}()
	_, _, _ = CompileShowWhen("(a || b) && c", "field")
}

// TestCompileShowWhen_RejectCrossLevel：parent.field == 'x' → error
func TestCompileShowWhen_RejectCrossLevel(t *testing.T) {
	t.Parallel()
	_, _, err := CompileShowWhen("parent.field == 'x'", "child")
	if err == nil {
		t.Fatalf("expected error for cross-level reference, got nil")
	}
	if !strings.Contains(err.Error(), "跨 level") {
		t.Errorf("error should mention cross level, got %q", err.Error())
	}
}

// TestCompileShowWhen_RejectArithmetic：x + 1 == 2 → error
func TestCompileShowWhen_RejectArithmetic(t *testing.T) {
	t.Parallel()
	_, _, err := CompileShowWhen("x + 1 == 2", "field")
	if err == nil {
		t.Fatalf("expected error for arithmetic, got nil")
	}
}

// TestCompileShowWhen_NumberLiteral：max_retries == 3（数字字面量，int64）
func TestCompileShowWhen_NumberLiteral(t *testing.T) {
	t.Parallel()
	got, _, err := CompileShowWhen("max_retries == 3", "retry_delay_ms")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	ifNode := got["if"].(map[string]any)
	props := ifNode["properties"].(map[string]any)
	mr := props["max_retries"].(map[string]any)
	constVal := mr["const"]
	n, ok := constVal.(int64)
	if !ok {
		t.Fatalf("const should be int64, got %T (value=%v)", constVal, constVal)
	}
	if n != 3 {
		t.Errorf("const = %d, want 3", n)
	}
}

// TestCompileShowWhen_BooleanLiteral：enabled == true
func TestCompileShowWhen_BooleanLiteral(t *testing.T) {
	t.Parallel()
	got, _, err := CompileShowWhen("enabled == true", "api_key")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	ifNode := got["if"].(map[string]any)
	props := ifNode["properties"].(map[string]any)
	enabled := props["enabled"].(map[string]any)
	if enabled["const"] != true {
		t.Errorf("enabled.const = %v, want true", enabled["const"])
	}
}

// TestCompileShowWhen_NullLiteral：proxy == null
func TestCompileShowWhen_NullLiteral(t *testing.T) {
	t.Parallel()
	got, _, err := CompileShowWhen("proxy == null", "direct_url")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	ifNode := got["if"].(map[string]any)
	props := ifNode["properties"].(map[string]any)
	proxy := props["proxy"].(map[string]any)
	if _, has := proxy["const"]; !has {
		t.Fatalf("proxy.const key missing")
	}
	if proxy["const"] != nil {
		t.Errorf("proxy.const = %v, want nil", proxy["const"])
	}
}

// TestCompileShowWhen_UIShowWhen_AndShape：&& → {logical:{kind:"and", left, right}}
func TestCompileShowWhen_UIShowWhen_AndShape(t *testing.T) {
	t.Parallel()
	_, ui, err := CompileShowWhen("a == 'x' && b == 'y'", "target")
	if err != nil {
		t.Fatal(err)
	}
	logical, ok := ui["logical"].(map[string]any)
	if !ok {
		t.Fatalf("ui.logical 应为 map, 收到 %v", ui["logical"])
	}
	if logical["kind"] != "and" {
		t.Errorf("logical.kind 应为 and, 收到 %v", logical["kind"])
	}
	left, ok := logical["left"].(map[string]any)
	if !ok || left["field"] != "a" || left["op"] != "==" || left["value"] != "x" {
		t.Errorf("logical.left 错: %v", logical["left"])
	}
	right, ok := logical["right"].(map[string]any)
	if !ok || right["field"] != "b" || right["op"] != "==" || right["value"] != "y" {
		t.Errorf("logical.right 错: %v", logical["right"])
	}
	// 嵌套 cmp 节点也必须有 negate:false
	if left["negate"] != false || right["negate"] != false {
		t.Errorf("嵌套 cmp 节点 negate 应为 false: left=%v right=%v", left["negate"], right["negate"])
	}
}

// TestCompileShowWhen_UIShowWhen_OrShape：|| → {logical:{kind:"or", left, right}}
func TestCompileShowWhen_UIShowWhen_OrShape(t *testing.T) {
	t.Parallel()
	_, ui, err := CompileShowWhen("a == 'x' || b == 'y'", "target")
	if err != nil {
		t.Fatal(err)
	}
	logical, ok := ui["logical"].(map[string]any)
	if !ok || logical["kind"] != "or" {
		t.Errorf("ui.logical.kind 应为 or, 收到 %v", ui["logical"])
	}
}

// TestCompileShowWhen_UIShowWhen_InShape：in → {field, op:"in", values, negate:false}
func TestCompileShowWhen_UIShowWhen_InShape(t *testing.T) {
	t.Parallel()
	_, ui, err := CompileShowWhen("region in ['cn','us']", "locale")
	if err != nil {
		t.Fatal(err)
	}
	if ui["op"] != "in" || ui["field"] != "region" || ui["negate"] != false {
		t.Errorf("ui in-shape 错: %v", ui)
	}
	values, ok := ui["values"].([]any)
	if !ok || len(values) != 2 || values[0] != "cn" || values[1] != "us" {
		t.Errorf("ui.values 错: %v", ui["values"])
	}
}

// TestCompileShowWhen_FieldNameStartingWithKeyword_NotMisinterpreted：
// C2：inbox 字段名以 "in" 开头，不应被 in 操作符吃掉。
func TestCompileShowWhen_FieldNameStartingWithKeyword_NotMisinterpreted(t *testing.T) {
	t.Parallel()
	// "inbox" 字段名以 "in" 开头，不应被误识别为 in 操作符
	_, _, err := CompileShowWhen("inbox == 'something'", "target")
	if err != nil {
		t.Fatalf("inbox 字段名不应报错: %v", err)
	}
}

// TestCompileShowWhen_LiteralTrueX_ShouldError：
// C2：trueFlag 作为 RHS 不是合法 literal。
func TestCompileShowWhen_LiteralTrueX_ShouldError(t *testing.T) {
	t.Parallel()
	_, _, err := CompileShowWhen("enabled == trueFlag", "target")
	if err == nil {
		t.Fatal("trueFlag 作为 literal 应报错")
	}
}

// TestCompileShowWhen_LiteralNullFoo_ShouldError：
// C2：nullFoo 作为 RHS 不是合法 literal。
func TestCompileShowWhen_LiteralNullFoo_ShouldError(t *testing.T) {
	t.Parallel()
	_, _, err := CompileShowWhen("x == nullFoo", "target")
	if err == nil {
		t.Fatal("nullFoo 作为 literal 应报错")
	}
}

// TestCompileShowWhen_RHSArithmetic_ReportsAsArithmetic：
// I1：RHS 算术 "a == 1 + 2" 应分类为 "算术运算不支持"，而非 "尾部未消费"。
func TestCompileShowWhen_RHSArithmetic_ReportsAsArithmetic(t *testing.T) {
	t.Parallel()
	_, _, err := CompileShowWhen("a == 1 + 2", "target")
	if err == nil {
		t.Fatal("RHS 算术应拒")
	}
	if !strings.Contains(err.Error(), "算术") {
		t.Errorf("error 应提到算术, 收到: %v", err)
	}
}
