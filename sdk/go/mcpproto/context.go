package mcpproto

import "context"

type contextKey string

const (
	keyResourceScope contextKey = "ks_resource_scope"
	keyExecutionID   contextKey = "ks_execution_id"
	keyTaskID        contextKey = "ks_task_id"
	keyTaskName      contextKey = "ks_task_name"
	keyTriggerType   contextKey = "ks_trigger_type"
	// v0.4.0：keystone 调工具时通过 _meta 透传，
	// 用于被调应用写审计日志 / 链路追踪。
	keyAgentID   contextKey = "ks_agent_id"
	keyUserID    contextKey = "ks_user_id"
	keyRequestID contextKey = "ks_request_id"
	// v0.6.0 Capability Mesh：dispatcher 调 capability backend.kind=mcp_tool 时
	// 通过 _meta 透传 caller 身份 + chain trace；capability handler 经
	// CapabilityContext 读，跨应用调用链路得以贯通（与 Python ks_app/context.py 对齐）。
	keyCallerID   contextKey = "ks_caller_id"
	keyCallerKind contextKey = "ks_caller_kind"
	keyChainID    contextKey = "ks_chain_id"
	// capability mesh 调用链快照（X-Keystone-Call-Chain 的 _meta 透传形态）。
	// keystone mcptool executor 配套往 _meta 放 ks_chain_snapshot 后，mcp_tool 路径的
	// 被调 capability 才能拿到非空 ChainHeader 继续透传。
	keyChainSnapshot contextKey = "ks_chain_snapshot"
)

// WithMeta 从 MCP _meta 字段构建携带 Keystone 上下文信息的 context。
// 上层包（如 ksapp）可在测试中使用此函数构造带元数据的 context。
func WithMeta(parent context.Context, meta map[string]any) context.Context {
	ctx := parent
	stringKeys := []struct {
		metaKey string
		ctxKey  contextKey
	}{
		{"ks_resource_scope", keyResourceScope},
		{"ks_execution_id", keyExecutionID},
		{"ks_task_id", keyTaskID},
		{"ks_task_name", keyTaskName},
		{"ks_trigger_type", keyTriggerType},
		{"ks_agent_id", keyAgentID},
		{"ks_user_id", keyUserID},
		{"ks_request_id", keyRequestID},
		{"ks_caller_id", keyCallerID},
		{"ks_caller_kind", keyCallerKind},
		{"ks_chain_id", keyChainID},
		{"ks_chain_snapshot", keyChainSnapshot},
	}
	for _, k := range stringKeys {
		if v, ok := meta[k.metaKey].(string); ok {
			ctx = context.WithValue(ctx, k.ctxKey, v)
		}
	}
	return ctx
}

// ResourceScope 从 context 中提取 Keystone 注入的资源作用域。
// 用于多租户隔离：不同实例调用同一工具时，通过此值区分数据边界。
func ResourceScope(ctx context.Context) string {
	v, _ := ctx.Value(keyResourceScope).(string)
	return v
}

// ExecutionID 从 context 中提取当前执行 ID。
func ExecutionID(ctx context.Context) string {
	v, _ := ctx.Value(keyExecutionID).(string)
	return v
}

// TaskID 从 context 中提取当前任务 ID。
func TaskID(ctx context.Context) string {
	v, _ := ctx.Value(keyTaskID).(string)
	return v
}

// TaskName 从 context 中提取当前任务名称。
func TaskName(ctx context.Context) string {
	v, _ := ctx.Value(keyTaskName).(string)
	return v
}

// TriggerType 从 context 中提取触发类型（manual/cron/webhook/event）。
func TriggerType(ctx context.Context) string {
	v, _ := ctx.Value(keyTriggerType).(string)
	return v
}

// AgentID 从 context 中提取调用 keystone agent ID（v0.4.0 起 _meta 透传）。
func AgentID(ctx context.Context) string {
	v, _ := ctx.Value(keyAgentID).(string)
	return v
}

// UserID 从 context 中提取调用 user ID。
func UserID(ctx context.Context) string {
	v, _ := ctx.Value(keyUserID).(string)
	return v
}

// RequestID 从 context 中提取链路追踪请求 ID。
func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(keyRequestID).(string)
	return v
}

// CallerID 从 context 中提取 capability mesh caller_id（v0.6.0 起 dispatcher 透传）。
// mcp_tool backend 路径下与 AgentID 同源；http_endpoint 路径不走此 ctx 而走 ScopedJWT。
func CallerID(ctx context.Context) string {
	v, _ := ctx.Value(keyCallerID).(string)
	return v
}

// CallerKind 从 context 中提取 capability mesh caller_kind（user / app / agent / dispatcher）。
func CallerKind(ctx context.Context) string {
	v, _ := ctx.Value(keyCallerKind).(string)
	return v
}

// ChainID 从 context 中提取 capability mesh 跨应用调用链 ID。
func ChainID(ctx context.Context) string {
	v, _ := ctx.Value(keyChainID).(string)
	return v
}

// ChainSnapshot 从 context 提取 capability mesh 调用链快照（X-Keystone-Call-Chain
// 的 _meta 透传形态；keystone mcptool executor 配套透传后生效）。
func ChainSnapshot(ctx context.Context) string {
	v, _ := ctx.Value(keyChainSnapshot).(string)
	return v
}
