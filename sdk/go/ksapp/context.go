package ksapp

import (
	"context"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/mcpproto"
)

// ResourceScope 从 context 中提取 Keystone 注入的资源作用域。
// 用于多租户隔离：不同实例调用同一工具时，通过此值区分数据边界。
// 委托给 mcpproto 包，确保与 MCP handler 设置的上下文值一致。
func ResourceScope(ctx context.Context) string {
	return mcpproto.ResourceScope(ctx)
}

// ExecutionID 从 context 中提取当前执行 ID。
func ExecutionID(ctx context.Context) string {
	return mcpproto.ExecutionID(ctx)
}

// TaskID 从 context 中提取当前任务 ID。
func TaskID(ctx context.Context) string {
	return mcpproto.TaskID(ctx)
}

// TaskName 从 context 中提取当前任务名称。
func TaskName(ctx context.Context) string {
	return mcpproto.TaskName(ctx)
}

// TriggerType 从 context 中提取触发类型（manual/cron/webhook/event）。
func TriggerType(ctx context.Context) string {
	return mcpproto.TriggerType(ctx)
}

// AgentID 从 context 中提取调用 keystone agent ID（v0.4.0 起 _meta 透传，用于审计）。
func AgentID(ctx context.Context) string {
	return mcpproto.AgentID(ctx)
}

// UserID 从 context 中提取调用 user ID。
func UserID(ctx context.Context) string {
	return mcpproto.UserID(ctx)
}

// RequestID 从 context 中提取链路追踪请求 ID。
func RequestID(ctx context.Context) string {
	return mcpproto.RequestID(ctx)
}

// CallerID 从 context 中提取 capability mesh caller_id（v0.6.0 起 dispatcher 透传）。
func CallerID(ctx context.Context) string {
	return mcpproto.CallerID(ctx)
}

// CallerKind 从 context 中提取 capability mesh caller_kind（user / app / agent / dispatcher）。
func CallerKind(ctx context.Context) string {
	return mcpproto.CallerKind(ctx)
}

// ChainID 从 context 中提取 capability mesh 跨应用调用链 ID。
func ChainID(ctx context.Context) string {
	return mcpproto.ChainID(ctx)
}

// ChainSnapshot 从 context 提取 capability mesh 调用链快照（mcp_tool 路径的
// chain header 来源；委托 mcpproto）。
func ChainSnapshot(ctx context.Context) string {
	return mcpproto.ChainSnapshot(ctx)
}
