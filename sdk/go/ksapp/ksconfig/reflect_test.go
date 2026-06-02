package ksconfig

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type testConfig struct {
	MiniMaxAPIKey string `ksconfig:"required,type:password,label_zh:MiniMax API 密钥,label_en:MiniMax API Key,hint:从控制台获取"`
	Region        string `ksconfig:"enum:cn|us|eu,default:cn,label:区域"`
	MaxRetries    int    `ksconfig:"default:3,min:1,max:10,label:最大重试次数"`
	EnableCache   bool   `ksconfig:"default:true,label:启用缓存"`
}

func TestReflectConfigSchema_Basic(t *testing.T) {
	t.Parallel()
	schema, uiSchema, err := ReflectConfigSchema[testConfig]()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, _ := json.MarshalIndent(schema, "", "  ")
	t.Logf("schema: %s", b)

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema.properties 不是 map")
	}
	if _, ok := props["mini_max_api_key"]; !ok {
		t.Errorf("缺 mini_max_api_key 字段 (PascalCase→snake_case 应含缩写组合并：APIKey → api_key)")
	}
	if _, ok := props["max_retries"]; !ok {
		t.Errorf("缺 max_retries")
	}
	maxRetries := props["max_retries"].(map[string]any)
	if maxRetries["title"] != "最大重试次数" {
		t.Errorf("max_retries title = %v, want 最大重试次数", maxRetries["title"])
	}

	required, ok := schema["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "mini_max_api_key" {
		t.Errorf("required 应只含 mini_max_api_key, 收到: %v", required)
	}

	keyUI, _ := uiSchema["mini_max_api_key"].(map[string]any)
	if keyUI["ui:widget"] != "password" {
		t.Errorf("mini_max_api_key UI widget 应 password")
	}
	if keyUI["ui:label"] != "MiniMax API 密钥" {
		t.Errorf("mini_max_api_key ui:label = %v, want MiniMax API 密钥", keyUI["ui:label"])
	}
	keySchema := props["mini_max_api_key"].(map[string]any)
	if keySchema["title"] != "MiniMax API 密钥" {
		t.Errorf("mini_max_api_key title = %v, want MiniMax API 密钥", keySchema["title"])
	}
	i18n, _ := keyUI["ks:label_i18n"].(map[string]string)
	if i18n["zh-CN"] != "MiniMax API 密钥" || i18n["en-US"] != "MiniMax API Key" {
		t.Errorf("mini_max_api_key ks:label_i18n = %v", keyUI["ks:label_i18n"])
	}
	order, ok := uiSchema["ui:order"].([]any)
	if !ok {
		t.Fatalf("ui:order 缺失或类型错误: %T", uiSchema["ui:order"])
	}
	wantOrder := []any{"mini_max_api_key", "region", "max_retries", "enable_cache"}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Errorf("ui:order = %#v, want %#v", order, wantOrder)
	}
}

type abbreviationTest struct {
	APIKey   string `ksconfig:"label:API Key"`
	UserID   int    `ksconfig:"label:用户ID"`
	HTTPHost string `ksconfig:"label:HTTP Host"`
}

func TestReflectConfigSchema_Abbreviation(t *testing.T) {
	t.Parallel()
	schema, _, _ := ReflectConfigSchema[abbreviationTest]()
	props := schema["properties"].(map[string]any)
	for _, want := range []string{"api_key", "user_id", "http_host"} {
		if _, ok := props[want]; !ok {
			t.Errorf("缺字段 %q", want)
		}
	}
}

type jsonTagOverride struct {
	SomeField string `ksconfig:"label:X" json:"custom_name"`
}

func TestReflectConfigSchema_JSONTagOverride(t *testing.T) {
	t.Parallel()
	schema, _, _ := ReflectConfigSchema[jsonTagOverride]()
	props := schema["properties"].(map[string]any)
	if _, ok := props["custom_name"]; !ok {
		t.Errorf("json tag 应优先于自动 snake_case 转换")
	}
}

type enumConfig struct {
	Region string `ksconfig:"enum:cn|us|eu,default:cn"`
}

func TestReflectConfigSchema_EnumAndDefault(t *testing.T) {
	t.Parallel()
	schema, _, _ := ReflectConfigSchema[enumConfig]()
	props := schema["properties"].(map[string]any)
	region := props["region"].(map[string]any)
	enumVal := region["enum"].([]any)
	if len(enumVal) != 3 {
		t.Errorf("enum len = %d, want 3", len(enumVal))
	}
	if region["default"] != "cn" {
		t.Errorf("default should be cn")
	}
	_ = reflect.TypeOf(enumVal) // 抑制 unused warning
}

// TestReflectConfigSchema_IntDefaultTyped 覆盖 C2：int 字段 default 必须是 int64，不能是 string。
func TestReflectConfigSchema_IntDefaultTyped(t *testing.T) {
	t.Parallel()
	type intCfg struct {
		MaxRetries int `ksconfig:"default:3,min:1,max:10"`
	}
	schema, _, err := ReflectConfigSchema[intCfg]()
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	props := schema["properties"].(map[string]any)
	mr := props["max_retries"].(map[string]any)
	if mr["type"] != "integer" {
		t.Errorf("type = %v, want integer", mr["type"])
	}
	// 关键断言：default 必须是 int64（不是 string）
	if got, ok := mr["default"].(int64); !ok || got != 3 {
		t.Errorf("default = %v (%T), want int64(3)", mr["default"], mr["default"])
	}
	if mr["minimum"] != int64(1) {
		t.Errorf("minimum = %v, want int64(1)", mr["minimum"])
	}
	if mr["maximum"] != int64(10) {
		t.Errorf("maximum = %v, want int64(10)", mr["maximum"])
	}
}

// TestReflectConfigSchema_IntDefaultInvalid 覆盖 C2：无法解析的 default 应返回 error。
func TestReflectConfigSchema_IntDefaultInvalid(t *testing.T) {
	t.Parallel()
	type badIntCfg struct {
		Count int `ksconfig:"default:not_a_number"`
	}
	_, _, err := ReflectConfigSchema[badIntCfg]()
	if err == nil {
		t.Fatal("expected error for non-integer default, got nil")
	}
	// 错误信息要包含字段级上下文
	if got := err.Error(); !strings.Contains(got, "default 不是合法整数") {
		t.Errorf("error msg = %q, want 含 \"default 不是合法整数\"", got)
	}
}

// show_when 反射管道：JSON Schema allOf + UI Schema ui:show_when
// 两管道必须同时通。
func TestReflectConfigSchema_ShowWhen_AllOfAndUIShow(t *testing.T) {
	t.Parallel()
	type Backend struct {
		Type    string `json:"type" ksconfig:"required,enum:github|gitlab|gitea"`
		BaseURL string `json:"base_url" ksconfig:"show_when:type != 'github'"`
	}
	schema, ui, err := ReflectConfigSchema[Backend]()
	if err != nil {
		t.Fatalf("reflect: %v", err)
	}

	// JSON Schema 侧：allOf 含 if/then/else 片段
	allOf, ok := schema["allOf"].([]map[string]any)
	if !ok || len(allOf) == 0 {
		t.Fatalf("schema.allOf 缺失或类型错: %T %v", schema["allOf"], schema["allOf"])
	}
	if _, hasIf := allOf[0]["if"]; !hasIf {
		t.Errorf("allOf[0] 缺 if 字段；got keys=%v", keysOfAny(allOf[0]))
	}
	if _, hasThen := allOf[0]["then"]; !hasThen {
		t.Errorf("allOf[0] 缺 then 字段")
	}
	if _, hasElse := allOf[0]["else"]; !hasElse {
		t.Errorf("allOf[0] 缺 else 字段")
	}

	// UI Schema 侧：ui[base_url]["ui:show_when"] 存在
	baseURLUI, ok := ui["base_url"].(map[string]any)
	if !ok {
		t.Fatalf("ui.base_url 缺失或类型错: %T", ui["base_url"])
	}
	if _, ok := baseURLUI["ui:show_when"]; !ok {
		t.Errorf("ui.base_url[\"ui:show_when\"] 缺失；keys = %v", keysOfAny(baseURLUI))
	}
}

// keysOfAny 辅助提取 map 的 key 列表（测试调试用）。
func keysOfAny(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// 多字段 show_when 组合：两字段分别 show_when → schema.allOf 两条。
func TestReflectConfigSchema_ShowWhen_MultipleFieldsAllOf(t *testing.T) {
	t.Parallel()
	type Spec struct {
		Type string `json:"type" ksconfig:"required,enum:a|b|c"`
		F1   string `json:"f1" ksconfig:"show_when:type == 'a'"`
		F2   string `json:"f2" ksconfig:"show_when:type == 'b'"`
	}
	schema, _, err := ReflectConfigSchema[Spec]()
	if err != nil {
		t.Fatalf("reflect: %v", err)
	}
	allOf, ok := schema["allOf"].([]map[string]any)
	if !ok {
		t.Fatalf("allOf 类型: %T", schema["allOf"])
	}
	if len(allOf) != 2 {
		t.Fatalf("allOf 长度: want 2, got %d", len(allOf))
	}
}

// slice-of-struct 内部字段的 show_when allOf 应透传到父级 items schema。
// 修复早期 MVP 限制：sub-struct 的 allOf 不向上传。
type backendForSlice struct {
	Type    string `json:"type" ksconfig:"required,enum:github|gitlab|gitea"`
	BaseURL string `json:"base_url" ksconfig:"show_when:type != 'github'"`
}
type cfgWithSliceBackends struct {
	Backends []backendForSlice `json:"backends"`
}

func TestReflect_SliceOfStruct_ShowWhen_AllOfPropagated(t *testing.T) {
	t.Parallel()
	schema, _, err := ReflectConfigSchema[cfgWithSliceBackends]()
	if err != nil {
		t.Fatalf("reflect: %v", err)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema.properties 类型错: %T", schema["properties"])
	}
	backends, ok := props["backends"].(map[string]any)
	if !ok {
		t.Fatalf("backends 缺失或类型错: %T", props["backends"])
	}
	items, ok := backends["items"].(map[string]any)
	if !ok {
		t.Fatalf("backends.items 缺失或类型错: %T", backends["items"])
	}
	allOf, ok := items["allOf"]
	if !ok {
		t.Fatalf("slice item schema 必须含 allOf（sub-struct 内 show_when 透传），items keys=%v", keysOfAny(items))
	}
	// allOf 应非空（类型 []map[string]any 或 []any，均可接受）
	switch v := allOf.(type) {
	case []map[string]any:
		if len(v) == 0 {
			t.Fatalf("items.allOf 为空")
		}
	case []any:
		if len(v) == 0 {
			t.Fatalf("items.allOf 为空")
		}
	default:
		t.Fatalf("items.allOf 类型意外: %T", allOf)
	}
}

func TestReflect_SliceOfStruct_ShowWhen_UIPropagated(t *testing.T) {
	t.Parallel()
	_, ui, err := ReflectConfigSchema[cfgWithSliceBackends]()
	if err != nil {
		t.Fatalf("reflect: %v", err)
	}
	backendsUI, ok := ui["backends"].(map[string]any)
	if !ok {
		t.Fatalf("ui.backends 缺失或类型错: %T", ui["backends"])
	}
	items, ok := backendsUI["items"].(map[string]any)
	if !ok {
		t.Fatalf("ui.backends.items 缺失或类型错: %T keys=%v", backendsUI["items"], keysOfAny(backendsUI))
	}
	baseURL, ok := items["base_url"].(map[string]any)
	if !ok {
		t.Fatalf("ui.backends.items.base_url 缺失或类型错: %T", items["base_url"])
	}
	showWhen, ok := baseURL["ui:show_when"]
	if !ok {
		t.Fatalf("ui.backends.items.base_url[\"ui:show_when\"] 缺失; keys=%v", keysOfAny(baseURL))
	}
	if showWhen == nil {
		t.Fatalf("ui:show_when 为 nil")
	}
}

// sub-struct（非 slice）内部字段的 show_when allOf 应透传到父字段 schema。
type nestedDetailStruct struct {
	Mode   string `json:"mode" ksconfig:"required,enum:off|on|auto"`
	Detail string `json:"detail" ksconfig:"show_when:mode != 'off'"`
}
type cfgWithNestedStruct struct {
	Nested nestedDetailStruct `json:"nested"`
}

func TestReflect_StructField_ShowWhen_AllOfPropagated(t *testing.T) {
	t.Parallel()
	schema, _, err := ReflectConfigSchema[cfgWithNestedStruct]()
	if err != nil {
		t.Fatalf("reflect: %v", err)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema.properties 类型错: %T", schema["properties"])
	}
	nested, ok := props["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested 缺失或类型错: %T", props["nested"])
	}
	allOf, ok := nested["allOf"]
	if !ok {
		t.Fatalf("sub-struct field schema 必须含 allOf（sub-struct 内 show_when 透传），nested keys=%v", keysOfAny(nested))
	}
	switch v := allOf.(type) {
	case []map[string]any:
		if len(v) == 0 {
			t.Fatalf("nested.allOf 为空")
		}
	case []any:
		if len(v) == 0 {
			t.Fatalf("nested.allOf 为空")
		}
	default:
		t.Fatalf("nested.allOf 类型意外: %T", allOf)
	}
}

func TestReflect_StructField_ShowWhen_UIPropagated(t *testing.T) {
	t.Parallel()
	_, ui, err := ReflectConfigSchema[cfgWithNestedStruct]()
	if err != nil {
		t.Fatalf("reflect: %v", err)
	}
	nestedUI, ok := ui["nested"].(map[string]any)
	if !ok {
		t.Fatalf("ui.nested 缺失或类型错: %T", ui["nested"])
	}
	detail, ok := nestedUI["detail"].(map[string]any)
	if !ok {
		t.Fatalf("ui.nested.detail 缺失或类型错: %T keys=%v", nestedUI["detail"], keysOfAny(nestedUI))
	}
	if _, ok := detail["ui:show_when"]; !ok {
		t.Fatalf("ui.nested.detail[\"ui:show_when\"] 缺失; keys=%v", keysOfAny(detail))
	}
}
