package ksapp

import (
	"context"
	"testing"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/mcpproto"
)

// TestContextDelegatesToMcpproto_AllFields 验证 ksapp 的上下文读取函数正确委托到 mcpproto 包：
// 通过 mcpproto.WithMeta 设置的值能被 ksapp 的 ResourceScope 等函数正确读取。
func TestContextDelegatesToMcpproto_AllFields(t *testing.T) {
	meta := map[string]any{
		"ks_resource_scope": "instance_123",
		"ks_execution_id":   "exec_456",
		"ks_task_id":        "task_789",
		"ks_task_name":      "日报生成",
		"ks_trigger_type":   "cron",
	}
	ctx := mcpproto.WithMeta(context.Background(), meta)

	if got := ResourceScope(ctx); got != "instance_123" {
		t.Errorf("ResourceScope = %q", got)
	}
	if got := ExecutionID(ctx); got != "exec_456" {
		t.Errorf("ExecutionID = %q", got)
	}
	if got := TaskID(ctx); got != "task_789" {
		t.Errorf("TaskID = %q", got)
	}
	if got := TaskName(ctx); got != "日报生成" {
		t.Errorf("TaskName = %q", got)
	}
	if got := TriggerType(ctx); got != "cron" {
		t.Errorf("TriggerType = %q", got)
	}
}

// TestContextDelegatesToMcpproto_EmptyContext 验证空 context 时所有读取函数安全返回空字符串。
func TestContextDelegatesToMcpproto_EmptyContext(t *testing.T) {
	ctx := context.Background()

	if got := ResourceScope(ctx); got != "" {
		t.Errorf("空 context 时 ResourceScope 应为空，got %q", got)
	}
	if got := ExecutionID(ctx); got != "" {
		t.Errorf("空 context 时 ExecutionID 应为空，got %q", got)
	}
	if got := TaskID(ctx); got != "" {
		t.Errorf("空 context 时 TaskID 应为空，got %q", got)
	}
	if got := TaskName(ctx); got != "" {
		t.Errorf("空 context 时 TaskName 应为空，got %q", got)
	}
	if got := TriggerType(ctx); got != "" {
		t.Errorf("空 context 时 TriggerType 应为空，got %q", got)
	}
}

// TestContextDelegatesToMcpproto_NonStringIgnored 验证非 string 类型的 meta 值被安全忽略。
func TestContextDelegatesToMcpproto_NonStringIgnored(t *testing.T) {
	meta := map[string]any{
		"ks_resource_scope": 123,              // int
		"ks_execution_id":   map[string]any{}, // 嵌套 map
		"ks_task_id":        nil,              // nil
		"ks_task_name":      "real_name",      // 同批次合法 string
	}
	ctx := mcpproto.WithMeta(context.Background(), meta)

	if got := ResourceScope(ctx); got != "" {
		t.Errorf("int 值应被忽略，got %q", got)
	}
	if got := ExecutionID(ctx); got != "" {
		t.Errorf("map 值应被忽略，got %q", got)
	}
	if got := TaskID(ctx); got != "" {
		t.Errorf("nil 值应被忽略，got %q", got)
	}
	if got := TaskName(ctx); got != "real_name" {
		t.Errorf("同批次的合法 string 应正常写入，got %q", got)
	}
}

// TestReusedToolReadsCallerFromCtx 锁复用降级：复用普通
// app.Tool 作为 mcp_tool capability backend 时，被复用的 handler 拿不到
// CapabilityContext，但能经 ctx helper（CallerID/ChainID）从 _meta.ks_* 取 caller 上下文。
func TestReusedToolReadsCallerFromCtx(t *testing.T) {
	ctx := context.Background()
	ctx = mcpproto.WithMeta(ctx, map[string]any{
		"ks_caller_id": "app-7", "ks_chain_id": "chn_1",
	})
	if got := CallerID(ctx); got != "app-7" {
		t.Fatalf("CallerID=%q want app-7", got)
	}
	if got := ChainID(ctx); got != "chn_1" {
		t.Fatalf("ChainID=%q want chn_1", got)
	}
}
