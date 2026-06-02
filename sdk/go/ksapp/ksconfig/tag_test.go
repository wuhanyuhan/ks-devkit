package ksconfig

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseTag_Required(t *testing.T) {
	t.Parallel()
	spec, err := ParseTag("required")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !spec.Required {
		t.Fatalf("Required = false, want true")
	}
}

func TestParseTag_TypeAndLabel(t *testing.T) {
	t.Parallel()
	spec, err := ParseTag("type:password,label:API Key")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if spec.Type != "password" {
		t.Fatalf("Type = %q, want %q", spec.Type, "password")
	}
	if spec.Label != "API Key" {
		t.Fatalf("Label = %q, want %q", spec.Label, "API Key")
	}
}

func TestParseTag_DefaultAndRange(t *testing.T) {
	t.Parallel()
	spec, err := ParseTag("default:3,min:1,max:10")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if spec.Default != "3" {
		t.Fatalf("Default = %q, want %q", spec.Default, "3")
	}
	if spec.Min == nil || *spec.Min != 1 {
		t.Fatalf("Min = %v, want *Min = 1", spec.Min)
	}
	if spec.Max == nil || *spec.Max != 10 {
		t.Fatalf("Max = %v, want *Max = 10", spec.Max)
	}
}

func TestParseTag_EnumWithPipe(t *testing.T) {
	t.Parallel()
	spec, err := ParseTag("enum:cn|us|eu,default:cn")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := []string{"cn", "us", "eu"}
	if len(spec.Enum) != len(want) {
		t.Fatalf("Enum len = %d, want %d (got %v)", len(spec.Enum), len(want), spec.Enum)
	}
	for i, v := range want {
		if spec.Enum[i] != v {
			t.Fatalf("Enum[%d] = %q, want %q", i, spec.Enum[i], v)
		}
	}
	if spec.Default != "cn" {
		t.Fatalf("Default = %q, want %q", spec.Default, "cn")
	}
}

func TestParseTag_ChineseLabelAndHint(t *testing.T) {
	t.Parallel()
	spec, err := ParseTag("required,type:password,label:MiniMax API Key,hint:从控制台获取")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !spec.Required {
		t.Fatalf("Required = false, want true")
	}
	if spec.Type != "password" {
		t.Fatalf("Type = %q, want password", spec.Type)
	}
	if spec.Label != "MiniMax API Key" {
		t.Fatalf("Label = %q, want MiniMax API Key", spec.Label)
	}
	if spec.Hint != "从控制台获取" {
		t.Fatalf("Hint = %q, want 从控制台获取", spec.Hint)
	}
}

func TestParseTag_LabelI18n(t *testing.T) {
	t.Parallel()
	spec, err := ParseTag("label_zh:MiniMax API 密钥,label_en:MiniMax API Key")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if spec.LabelZH != "MiniMax API 密钥" {
		t.Fatalf("LabelZH = %q, want MiniMax API 密钥", spec.LabelZH)
	}
	if spec.LabelEN != "MiniMax API Key" {
		t.Fatalf("LabelEN = %q, want MiniMax API Key", spec.LabelEN)
	}
}

func TestParseTag_Pattern(t *testing.T) {
	t.Parallel()
	spec, err := ParseTag(`pattern:^sk-[a-z0-9]+$`)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if spec.Pattern != `^sk-[a-z0-9]+$` {
		t.Fatalf("Pattern = %q, want %q", spec.Pattern, `^sk-[a-z0-9]+$`)
	}
}

func TestParseTag_Sensitive(t *testing.T) {
	t.Parallel()
	spec, err := ParseTag("sensitive,type:password")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !spec.Sensitive {
		t.Fatalf("Sensitive = false, want true")
	}
	if spec.Type != "password" {
		t.Fatalf("Type = %q, want password", spec.Type)
	}
}

func TestParseTag_ItemSchema(t *testing.T) {
	t.Parallel()
	spec, err := ParseTag("item_schema:BackendConfig")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if spec.ItemSchema != "BackendConfig" {
		t.Fatalf("ItemSchema = %q, want BackendConfig", spec.ItemSchema)
	}
}

// label 值含 ',' → 会被 ',' 切开：label 值变成 "a"，后半 "b" 被当作未知 key → panic。
// 注意这里实际走的是 default 分支（"b" 是未知 key）而非 label 的 ContainsAny 分支。
// label 本身的 `|` / `:` 非法字符检查由 TestParseTag_LabelWithPipePanics 负责。
func TestParseTag_LabelSplitByCommaTriggersUnknownKeyPanic(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic on label with ',', got none")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value type = %T, want string", r)
		}
		if !strings.Contains(msg, "ksconfig") {
			t.Fatalf("panic message %q missing 'ksconfig' prefix", msg)
		}
	}()
	_, _ = ParseTag("label:a,b")
}

// label 含 '|' 才能真正命中 label 的 ContainsAny(val, ",:|") 分支。
func TestParseTag_LabelWithPipePanics(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for label containing |")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic payload not string: %T", r)
		}
		if !strings.Contains(msg, "label") {
			t.Fatalf("panic msg %q should mention 'label'", msg)
		}
	}()
	_, _ = ParseTag("label:a|b") // | 会命中 ContainsAny(val, ",:|") 分支
}

func TestParseTag_Empty(t *testing.T) {
	t.Parallel()
	spec, err := ParseTag("")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	var zero TagSpec
	if !reflect.DeepEqual(spec, zero) {
		t.Fatalf("spec = %+v, want zero value", spec)
	}
}

func TestParseTag_MinLenMaxLen(t *testing.T) {
	t.Parallel()
	spec, err := ParseTag("minLen:32,maxLen:64")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if spec.MinLen == nil || *spec.MinLen != 32 {
		t.Fatalf("MinLen = %v, want *MinLen = 32", spec.MinLen)
	}
	if spec.MaxLen == nil || *spec.MaxLen != 64 {
		t.Fatalf("MaxLen = %v, want *MaxLen = 64", spec.MaxLen)
	}
}

// 补充：未知 key 必须 panic（启动期快速失败）
func TestParseTag_UnknownKeyPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic on unknown key, got none")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value type = %T, want string", r)
		}
		if !strings.Contains(msg, "foobar") {
			t.Fatalf("panic message %q missing unknown key name", msg)
		}
	}()
	_, _ = ParseTag("foobar")
}

// 补充：min/max 非整数应返回 error，不 panic
func TestParseTag_MinNotInteger(t *testing.T) {
	t.Parallel()
	_, err := ParseTag("min:abc")
	if err == nil {
		t.Fatalf("expected error on non-integer min, got nil")
	}
}

func TestParseTag_MaxNotInteger(t *testing.T) {
	t.Parallel()
	_, err := ParseTag("max:xyz")
	if err == nil {
		t.Fatalf("expected error on non-integer max, got nil")
	}
}

// I-3：enum 空值 → panic（启动期快速失败）
func TestParseTag_EnumEmptyValuePanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty enum value")
		}
	}()
	_, _ = ParseTag("enum:")
}

// I-4：enum 尾部空元素（连续 | 或末尾 |）→ panic
func TestParseTag_EnumTrailingPipePanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for trailing pipe in enum")
		}
	}()
	_, _ = ParseTag("enum:a|b|")
}

// I-5：required 是 bool flag，带值非法 → panic
func TestParseTag_RequiredWithValuePanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for required:false")
		}
	}()
	_, _ = ParseTag("required:false")
}

// I-5：sensitive 是 bool flag，带值非法 → panic
func TestParseTag_SensitiveWithValuePanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for sensitive:true")
		}
	}()
	_, _ = ParseTag("sensitive:true")
}

// show_when tag 入口
// 覆盖 6 种运算符组合：==, !=, in (单元素), &&, ||, 以及与其他 key 组合。
//
// 已知现实限制：
//
//	show_when 表达式里禁止出现 `,` / `:`（这两个字符是 ksconfig tag 的
//	外层分隔符，会把 show_when 值切散）。
//	show_when DSL 的 `in [a,b,c]` 多元素列表在 tag 上无法承载；
//	多分支判断改用 `op1 || op2 || op3` 或单元素 in 替代。
//	CompileShowWhen 仍完整支持 `in [a,b,c]` 语法（编程式调用场景）。
func TestParseTag_ShowWhen(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		tag  string
		want string
	}{
		{"equality", `show_when:type == 'gitlab'`, "type == 'gitlab'"},
		{"inequality", `show_when:type != 'github'`, "type != 'github'"},
		{"in_single", `show_when:backend in ['gitlab']`, "backend in ['gitlab']"},
		{"and", `show_when:type != 'github' && enabled == true`, "type != 'github' && enabled == true"},
		{"or", `show_when:type == 'gitlab' || type == 'gitea'`, "type == 'gitlab' || type == 'gitea'"},
		{"combined_with_other", `required,show_when:type != 'github'`, "type != 'github'"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			spec, err := ParseTag(c.tag)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if spec.ShowWhen != c.want {
				t.Errorf("ShowWhen: want %q, got %q", c.want, spec.ShowWhen)
			}
		})
	}
}
