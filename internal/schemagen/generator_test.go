package schemagen

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGenerate_CapabilitySpecFieldsAndDescriptions(t *testing.T) {
	// 解析 pinned ks-types 模块，产出 manifest JSON Schema。
	out, err := Generate("github.com/wuhanyuhan/ks-types", "AppSpec")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(out, &schema); err != nil {
		t.Fatalf("产出非合法 JSON: %v", err)
	}
	// $defs 含 CapabilitySpec，且其 name 字段的 description 来自 doc 注释（含"裸名"）。
	defs, _ := schema["$defs"].(map[string]any)
	capSpec, ok := defs["CapabilitySpec"].(map[string]any)
	if !ok {
		t.Fatalf("$defs 缺 CapabilitySpec")
	}
	props, _ := capSpec["properties"].(map[string]any)
	nameProp, _ := props["name"].(map[string]any)
	desc, _ := nameProp["description"].(string)
	if !strings.Contains(desc, "裸名") {
		t.Errorf("name.description 未取自 doc 注释：%q", desc)
	}
	// side_effect_level 属性应存在
	if _, ok := props["side_effect_level"]; !ok {
		t.Errorf("缺 side_effect_level 属性")
	}
	// 防回归：被砍字段不应出现
	for _, banned := range []string{"cost_hint", "typical_latency_ms", "intent_summary", "input_nl"} {
		if _, ok := props[banned]; ok {
			t.Errorf("schema 含已砍字段 %q", banned)
		}
	}
}
