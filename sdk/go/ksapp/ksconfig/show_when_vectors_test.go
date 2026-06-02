package ksconfig

import (
	"encoding/json"
	"os"
	"testing"
)

// TestCompileShowWhen_Vectors 把 conformance/config-schema/testvectors.json 中的
// show_when 段对每一条样本跑 CompileShowWhen，并与 expected_json_schema 做字节级对比。
//
// 字节级对比（json.Marshal 后字符串比对）的原因：
//   - JSON 数字在 Go map[string]any 中是 float64，但 parseNumber 生成 int64；
//     reflect.DeepEqual 会因类型不同失败，而 Marshal 后两边都是 "3"。
//
// 特殊情形：
//   - context.array_context == true 的条目（array_item_show_when）
//     其 expected_json_schema 是 {type:"array", items:{type:"object", allOf:[...]}}，
//     外层 array 包装是 reflect.go 对 []Struct 字段处理的职责，不是 CompileShowWhen 的责任，
//     故这一条 t.Skip。
//   - should_reject 条目：parenthesis 走 panic（defer recover 捕获）；
//     cross_level / arithmetic 走 error 返回。
func TestCompileShowWhen_Vectors(t *testing.T) {
	data, err := os.ReadFile("testdata/testvectors.json")
	if err != nil {
		t.Skipf("testvectors.json 缺失: %v", err)
	}
	var full struct {
		ShowWhen []json.RawMessage `json:"show_when"`
	}
	if err := json.Unmarshal(data, &full); err != nil {
		t.Fatalf("parse testvectors.json: %v", err)
	}
	if len(full.ShowWhen) == 0 {
		t.Fatalf("show_when 段为空")
	}

	for _, raw := range full.ShowWhen {
		var head struct {
			Name         string `json:"name"`
			DSL          string `json:"dsl"`
			ShouldReject bool   `json:"should_reject"`
		}
		if err := json.Unmarshal(raw, &head); err != nil {
			t.Fatalf("head parse: %v", err)
		}
		t.Run(head.Name, func(t *testing.T) {
			t.Parallel()

			if head.ShouldReject {
				// 括号嵌套走 panic；cross level / 算术走 error
				var recovered any
				var errGot error
				func() {
					defer func() { recovered = recover() }()
					_, _, errGot = CompileShowWhen(head.DSL, "field")
				}()
				if recovered == nil && errGot == nil {
					t.Errorf("expected reject (panic or error) for %s, got neither", head.Name)
				}
				return
			}

			var v struct {
				DSL                string         `json:"dsl"`
				ExpectedJSONSchema map[string]any `json:"expected_json_schema"`
				Context            struct {
					FieldUnderIf string `json:"field_under_if"`
					ArrayContext bool   `json:"array_context"`
				} `json:"context"`
			}
			if err := json.Unmarshal(raw, &v); err != nil {
				t.Fatalf("parse vector %s: %v", head.Name, err)
			}
			if v.Context.ArrayContext {
				t.Skipf("array_context 场景由 reflect.go 负责包装，CompileShowWhen 不直接产出此形状")
				return
			}

			gotIfThen, _, err := CompileShowWhen(v.DSL, v.Context.FieldUnderIf)
			if err != nil {
				t.Fatalf("CompileShowWhen(%q) err: %v", v.DSL, err)
			}
			gotBytes, err := json.Marshal(gotIfThen)
			if err != nil {
				t.Fatalf("marshal got: %v", err)
			}
			wantBytes, err := json.Marshal(v.ExpectedJSONSchema)
			if err != nil {
				t.Fatalf("marshal want: %v", err)
			}
			if string(gotBytes) != string(wantBytes) {
				t.Errorf("mismatch for %s\ndsl:  %s\ngot:  %s\nwant: %s",
					head.Name, v.DSL, gotBytes, wantBytes)
			}
		})
	}
}
