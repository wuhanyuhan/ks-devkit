package mcpproto

import "testing"

func TestToolDefFields(t *testing.T) {
	td := ToolDef{Name: "greet", Description: "打招呼"}
	if td.Name != "greet" {
		t.Errorf("name = %q", td.Name)
	}
}
