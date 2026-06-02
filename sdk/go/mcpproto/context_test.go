package mcpproto

import (
	"context"
	"testing"
)

func TestWithMeta_AllKeys(t *testing.T) {
	meta := map[string]any{
		"ks_resource_scope": "scope_123",
		"ks_execution_id":   "exec_456",
		"ks_task_id":        "task_789",
		"ks_task_name":      "my_task",
		"ks_trigger_type":   "manual",
	}
	ctx := WithMeta(context.Background(), meta)

	if v := ResourceScope(ctx); v != "scope_123" {
		t.Errorf("ResourceScope = %q", v)
	}
	if v := ExecutionID(ctx); v != "exec_456" {
		t.Errorf("ExecutionID = %q", v)
	}
	if v := TaskID(ctx); v != "task_789" {
		t.Errorf("TaskID = %q", v)
	}
	if v := TaskName(ctx); v != "my_task" {
		t.Errorf("TaskName = %q", v)
	}
	if v := TriggerType(ctx); v != "manual" {
		t.Errorf("TriggerType = %q", v)
	}
}

func TestWithMeta_NilMeta(t *testing.T) {
	ctx := WithMeta(context.Background(), nil)
	if v := ResourceScope(ctx); v != "" {
		t.Errorf("ResourceScope = %q，期望空字符串", v)
	}
}

func TestWithMeta_PartialKeys(t *testing.T) {
	meta := map[string]any{
		"ks_resource_scope": "scope_only",
	}
	ctx := WithMeta(context.Background(), meta)

	if v := ResourceScope(ctx); v != "scope_only" {
		t.Errorf("ResourceScope = %q", v)
	}
	if v := ExecutionID(ctx); v != "" {
		t.Errorf("ExecutionID = %q，期望空字符串", v)
	}
}

func TestWithMeta_NonStringValue(t *testing.T) {
	meta := map[string]any{
		"ks_resource_scope": 42, // 非 string 类型应被忽略
	}
	ctx := WithMeta(context.Background(), meta)
	if v := ResourceScope(ctx); v != "" {
		t.Errorf("ResourceScope = %q，期望空字符串（非 string 值应被忽略）", v)
	}
}
