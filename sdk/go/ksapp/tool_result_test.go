package ksapp

import (
	"encoding/json"
	"strings"
	"testing"

	kstypes "github.com/wuhanyuhan/ks-types"
)

func TestToolResult_TextOnly(t *testing.T) {
	t.Parallel()
	r := NewToolResult().WithText("已审阅")
	payload, err := r.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(payload), `"text":"已审阅"`) {
		t.Errorf("missing text: %s", string(payload))
	}
	if strings.Contains(string(payload), `"_meta"`) {
		t.Errorf("_meta should be omitted: %s", string(payload))
	}
}

func TestToolResult_WithUIData(t *testing.T) {
	t.Parallel()
	r := NewToolResult().
		WithText("已审阅 draft 42").
		WithUIData(kstypes.WidgetDiffReviewV1{
			Title:   "5月营销月报",
			Diff:    []kstypes.WidgetDiffSegment{{Type: "context", Text: "x"}},
			Actions: []kstypes.WidgetActionDescriptor{{ID: "approve", Label: "批准"}},
		})
	payload, err := r.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	meta, ok := parsed["_meta"].(map[string]any)
	if !ok {
		t.Fatalf("_meta not present, payload=%s", string(payload))
	}
	keystoneMeta, ok := meta["keystone"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.keystone not present")
	}
	if keystoneMeta["ui"] == nil {
		t.Errorf("_meta.keystone.ui not present")
	}
	// 进一步断言 _meta.keystone.ui.data 内容是 widget data
	uiNode, ok := keystoneMeta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.keystone.ui not a map")
	}
	dataNode, ok := uiNode["data"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.keystone.ui.data not a map")
	}
	if dataNode["title"] != "5月营销月报" {
		t.Errorf("data.title = %v, want 5月营销月报", dataNode["title"])
	}
}

func TestToolResult_WithUIData_ValidatesSchema(t *testing.T) {
	t.Parallel()
	// Diff: nil 触发 WidgetDiffReviewV1.Validate "diff requires at least 1 segment"
	r := NewToolResult().WithUIData(kstypes.WidgetDiffReviewV1{Title: "x", Diff: nil, Actions: nil})
	_, err := r.MarshalJSON()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "diff requires at least 1 segment") {
		t.Errorf("expected diff validation error, got: %v", err)
	}
}

// TestToolResult_WithUIOverride 验证 per-call override 注入到 _meta.ui。
func TestToolResult_WithUIOverride(t *testing.T) {
	t.Parallel()
	r := NewToolResult().
		WithText("ok").
		WithUIOverride(kstypes.MetaUIDecl{
			Widget:       "ks://widgets/list-actions@v1",
			SandboxHints: []string{"allow-scripts"},
		})
	payload, err := r.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	meta, ok := parsed["_meta"].(map[string]any)
	if !ok {
		t.Fatalf("_meta missing: %s", string(payload))
	}
	uiNode, ok := meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui missing: %v", meta)
	}
	if uiNode["widget"] != "ks://widgets/list-actions@v1" {
		t.Errorf("override widget = %v", uiNode["widget"])
	}
}

// TestToolResult_Empty 验证 NewToolResult() 不带任何字段时的序列化。
func TestToolResult_Empty(t *testing.T) {
	t.Parallel()
	r := NewToolResult()
	payload, err := r.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(payload), `"_meta"`) {
		t.Errorf("empty result should not contain _meta: %s", string(payload))
	}
	if strings.Contains(string(payload), `"content"`) {
		t.Errorf("empty result should not contain content: %s", string(payload))
	}
}

// TestToolResult_NonValidatorData 验证 widget data 类型未实现 Validate() 时直接序列化。
func TestToolResult_NonValidatorData(t *testing.T) {
	t.Parallel()
	r := NewToolResult().WithUIData(map[string]any{"foo": "bar"})
	payload, err := r.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(payload), `"foo":"bar"`) {
		t.Errorf("missing data: %s", string(payload))
	}
}
