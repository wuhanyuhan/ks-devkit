package resources

import (
	"encoding/json"
	"testing"
)

func TestEmbeddedSchemaIsValidJSON(t *testing.T) {
	data, err := SchemaFS.ReadFile("schema/manifest.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("embedded schema 非合法 JSON: %v", err)
	}
	if _, ok := m["$defs"]; !ok {
		t.Error("schema 缺 $defs")
	}
}
