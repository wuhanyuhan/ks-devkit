package ksapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	kstypes "github.com/wuhanyuhan/ks-types"
)

const (
	headerCallChain = "X-Keystone-Call-Chain"
	headerChainID   = "X-Keystone-Chain-Id"
)

// CapabilityHandler 是 capability handler 签名。
// args 是入参 JSON 反序列化的 map；返回值会被 SDK 序列化为 dispatcher 期望的 result 形态。
type CapabilityHandler func(ctx CapabilityContext, args map[string]any) (any, error)

// capabilityEntry 是单个 capability 的内部注册项。
type capabilityEntry struct {
	CanonicalName   string
	Handler         CapabilityHandler
	BackendKind     string // mcp_tool | http_endpoint
	BackendToolName string
	BackendPath     string
	BackendMethod   string
	ExecutionMode   string
	TimeoutMs       int
	InputSchema     map[string]any // manifest.input_schema 内联 schema（mcp_tool 透传到 ToolDef.InputSchema）
}

// RegisterCapability 注册一个 capability handler（作者写裸名 name）。
// SDK 内部用 Canonical(app_id, name) 派生全名做 key + entry.CanonicalName。
// 重复注册 panic（与 App.Tool 行为一致，配置期错误快速失败）。
func (a *App) RegisterCapability(name string, handler CapabilityHandler) *App {
	canonical := kstypes.Canonical(a.id, name)
	if _, exists := a.capabilities[canonical]; exists {
		panic(fmt.Sprintf("ksapp: capability %q already registered", canonical))
	}
	a.capabilities[canonical] = &capabilityEntry{
		CanonicalName: canonical,
		Handler:       handler,
	}
	return a
}

// finalizeCapabilities 启动期把 manifest 元信息注入注册表 + 校验。
// 由 Mux() / Run() 在 server 起来前调一次（幂等，受 ensureCapabilityFinalized 保护）。
//
// 失败模式：
//   - manifest 缺失或解析失败：返回 wrap 错误
//   - 已注册 capability 不在 manifest.provides.capabilities：ErrManifestMismatch
//   - manifest 声明但无 handler：不在此裁决，交给 wireMCPToolBackend 四象限
func (a *App) finalizeCapabilities() error {
	specs, err := a.loadManifestCapabilities()
	if err != nil {
		return err
	}
	// 缓存 specs 供 wireMCPToolBackend 四象限遍历（含无 handler 的复用项）。
	a.manifestCaps = specs
	manifestNames := make([]string, 0, len(specs))
	byName := make(map[string]int, len(specs))
	for i, s := range specs {
		canonical := kstypes.Canonical(a.id, s.Name)
		manifestNames = append(manifestNames, canonical)
		byName[canonical] = i
	}
	for name, entry := range a.capabilities {
		idx, ok := byName[name]
		if !ok {
			return NewManifestMismatch(name, manifestNames)
		}
		spec := specs[idx]
		entry.BackendKind = spec.Backend.Kind
		entry.BackendToolName = spec.Backend.ToolName
		entry.BackendPath = spec.Backend.Path
		entry.BackendMethod = spec.Backend.Method
		entry.ExecutionMode = spec.ExecutionMode
		entry.TimeoutMs = spec.TimeoutMs
		entry.InputSchema = spec.InputSchema
	}
	// manifest 声明但无 handler 的 capability 不在此报错/告警——交给
	// wireMCPToolBackend 四象限裁决（可能是「复用已有 app.tool」的合法复用项）。
	return nil
}

// wireMCPToolBackend 对 manifest 声明的每个 mcp_tool capability 做复用四象限判定
// （两语言统一）。必须在 finalizeCapabilities 之后调。
func (a *App) wireMCPToolBackend() error {
	// 快照 wire 前已存在的 tool（仅 app.Tool 显式注册的），用于复用判定，
	// 避免本轮生成的 tool 干扰「命中已有 app.tool」判断。
	existingTools := make(map[string]struct{}, len(a.toolNames))
	for k := range a.toolNames {
		existingTools[k] = struct{}{}
	}
	for _, spec := range a.manifestCaps {
		if spec.Backend.Kind != "mcp_tool" {
			continue
		}
		canonical := kstypes.Canonical(a.id, spec.Name)
		toolName := spec.Backend.ToolName
		if toolName == "" {
			return fmt.Errorf("%w: capability %q backend.tool_name empty", ErrManifestMismatch, canonical)
		}
		entry, hasHandler := a.capabilities[canonical]
		_, toolExists := existingTools[toolName]

		switch {
		case hasHandler && toolExists:
			// 真冲突：既提供独立 handler 又撞已有 app.tool。
			return fmt.Errorf(
				"capability %q backend.tool_name=%q collides with existing @app.tool registration",
				canonical, toolName,
			)
		case hasHandler && !toolExists:
			// 生成新 MCP tool（capability-first，现状行为）。
			captured := entry
			appPtr := a
			handler := func(ctx context.Context, params map[string]any) (any, error) {
				capCtx := buildCapabilityContextFromGoCtx(ctx, captured, appPtr)
				return captured.Handler(capCtx, params)
			}
			a.tools = append(a.tools, ToolDef{
				Name:        toolName,
				Description: fmt.Sprintf("capability %s", canonical),
				InputSchema: captured.InputSchema,
				Handler:     handler,
			})
			a.toolNames[toolName] = struct{}{}
		case !hasHandler && toolExists:
			// 复用已有 app.tool 作为 backend（join 而非生成）。
			// keystone 透明：dispatcher 只要求目标 /mcp 上有此 tool 名。
			// 复用降级：被复用的 tool handler 拿不到 CapabilityContext；
			// caller 上下文经 args._meta.ks_* + context helper（CallerID/ChainID 等）提取。
			continue
		default:
			// !hasHandler && !toolExists：声明了能力却无承载。
			return fmt.Errorf(
				"%w: capability %q backend.tool_name=%q 既无已注册 @app.tool 也无 RegisterCapability handler",
				ErrManifestMismatch, canonical, toolName,
			)
		}
	}
	return nil
}

// buildCapabilityContextFromGoCtx 从 Go ctx（携带 MCP _meta.ks_*）构造 CapabilityContext。
// _meta → ctx 的字段透传由 mcpproto.WithMeta 完成；此函数仅读取。
func buildCapabilityContextFromGoCtx(ctx context.Context, entry *capabilityEntry, a *App) CapabilityContext {
	// caller_id / caller_kind / chain_id 优先取 capability mesh 透传值；
	// 旧 keystone 路径（v0.4.0）只透传 agent_id，此时 fallback 取它做 CallerID。
	appID := CallerID(ctx)
	if appID == "" {
		appID = AgentID(ctx)
	}
	callerKind := CallerKind(ctx)
	if callerKind == "" {
		callerKind = "app"
	}
	taskID := TaskID(ctx)
	return newCapabilityContext(capabilityContextInit{
		Ctx:           ctx,
		UserID:        UserID(ctx),
		CallerID:      appID,
		CallerKind:    callerKind,
		ChainID:       ChainID(ctx),
		ChainHeader:   ChainSnapshot(ctx),
		TaskID:        taskID,
		RequestID:     RequestID(ctx),
		CanonicalName: entry.CanonicalName,
		TimeoutMs:     entry.TimeoutMs,
		ReportFn:      buildProgressReportFn(a, taskID),
	})
}

// buildProgressReportFn 给定 task_id 闭包出真实的 progress 上报函数。
// task_id="" 或 DispatcherClient 不可用 → no-op（caller-side 未配置时不应让业务失败）。
func buildProgressReportFn(a *App, taskID string) ProgressReportFn {
	return func(rctx context.Context, stage string, percent *int) error {
		if taskID == "" {
			return nil
		}
		d := a.getDispatcherClient()
		if d == nil {
			return nil
		}
		return d.ReportProgress(rctx, taskID, stage, percent)
	}
}

// ensureCapabilityFinalized 幂等执行 finalize + mcp_tool wiring（Mux 入口调一次）。
// http_endpoint wiring 单独由 Mux 调（需要返回 routes 给 ScopedJWTMiddleware）。
// 失败缓存在 finalizeErr 上，多次调返回同一错误，避免重复 build 重注册 tool。
func (a *App) ensureCapabilityFinalized() error {
	a.finalizeOnce.Do(func() {
		if err := a.finalizeCapabilities(); err != nil {
			a.finalizeErr = err
			return
		}
		if err := a.wireMCPToolBackend(); err != nil {
			a.finalizeErr = err
			return
		}
	})
	return a.finalizeErr
}

// wireHTTPEndpointBackend 把 backend.kind=http_endpoint 的 capability 包装成 net/http handler。
// 返回 path → handler 与 path → canonical_name（后者给 ScopedJWTMiddleware 做 aud 校验）。
// 必须在 finalizeCapabilities 之后调；与 mcp_tool wiring 并行。
func (a *App) wireHTTPEndpointBackend() (httpRoutes map[string]http.Handler, pathToName map[string]string, err error) {
	httpRoutes = make(map[string]http.Handler)
	pathToName = make(map[string]string)
	for capName, entry := range a.capabilities {
		if entry.BackendKind != "http_endpoint" {
			continue
		}
		if entry.BackendPath == "" {
			return nil, nil, fmt.Errorf("%w: capability %q backend.path empty",
				ErrManifestMismatch, capName)
		}
		pathToName[entry.BackendPath] = capName
		captured := entry
		method := strings.ToUpper(entry.BackendMethod)
		if method == "" {
			method = http.MethodPost
		}
		httpRoutes[entry.BackendPath] = makeHTTPCapabilityHandler(a, captured, method)
	}
	return httpRoutes, pathToName, nil
}

// makeHTTPCapabilityHandler 构造单个 http_endpoint capability 的 HTTP handler。
//
// 流程：method 校验 → 取 ScopedClaims（middleware 注入）→ 解 body 入参 → 构造
// CapabilityContext（claims 直接做字段源，不再从 ctx 取）→ 调 handler → 序列化返回。
func makeHTTPCapabilityHandler(a *App, entry *capabilityEntry, method string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		claims, ok := scopedClaimsFromRequest(r)
		if !ok {
			http.Error(w, "scoped claims missing (middleware not wired?)", http.StatusInternalServerError)
			return
		}
		var args map[string]any
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
				args = map[string]any{}
			}
		}
		if args == nil {
			args = map[string]any{}
		}
		callerKind := claims.CallerKind
		if callerKind == "" {
			callerKind = "app"
		}
		// http_endpoint 路径 task_id 由 dispatcher 写在 args.task_id（long_running 调用时）；
		// sync 调用 args 里无 task_id → progress 自然 no-op。
		taskID := ""
		if tid, ok := args["task_id"].(string); ok {
			taskID = tid
		}
		capCtx := newCapabilityContext(capabilityContextInit{
			Ctx:           r.Context(),
			UserID:        claims.UserID,
			CallerID:      claims.CallerID,
			CallerKind:    callerKind,
			ChainID:       claims.ChainID,
			ChainHeader:   r.Header.Get(headerCallChain),
			TaskID:        taskID,
			RequestID:     claims.RequestID,
			CanonicalName: entry.CanonicalName,
			TimeoutMs:     entry.TimeoutMs,
			ReportFn:      buildProgressReportFn(a, taskID),
		})
		result, err := entry.Handler(capCtx, args)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})
}
