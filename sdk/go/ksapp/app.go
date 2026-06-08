package ksapp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/auth"
	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/keystore"
	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/meta"
	"github.com/wuhanyuhan/ks-devkit/sdk/go/mcpproto"
	kstypes "github.com/wuhanyuhan/ks-types"
)

// Middleware 是标准 Go HTTP 中间件签名：接受一个 Handler 返回一个包装后的 Handler。
type Middleware func(http.Handler) http.Handler

// HealthChecker 是自定义健康检查函数。返回 nil 表示健康，返回 error 表示不健康。
type HealthChecker struct {
	Name  string
	Check func() error
}

// customRoute 存储用户通过 Handle 注册的自定义路由。
type customRoute struct {
	pattern string
	handler http.Handler
}

// App 表示一个 Keystone MCP Service 应用实例。
type App struct {
	id           string
	tools        []ToolDef
	toolNames    map[string]struct{} // 用于检测重复注册
	config       *AppConfig
	llm          *LLMClient
	embedding    *EmbeddingClient
	middlewares  []Middleware
	healthChecks []HealthChecker
	routes       []customRoute
	// 鉴权与元信息（v0.4.0 新增）
	authMode     kstypes.AuthMode
	version      string
	jwksURL      string
	manifestPath string
	// /meta 声明字段（对齐 Python/TS SDK chain declare API）
	nav             *meta.NavDecl
	permissions     []meta.PermissionDecl
	configMode      string // "schema" / "iframe" / "none"
	configStatus    string // "unconfigured" / "via_frontend" / "via_cli" / "mixed"
	protocolVersion string // SemVer "MAJOR.MINOR"
	configUI        *meta.ConfigUIInfo
	// sharedVerifier 缓存 Mux() 和 ConfigUIMiddleware() 共用的 JWKSVerifier，
	// 避免重复拉取 JWKS；仅 keystone_jwks 模式下懒初始化，其他模式为 nil。
	sharedVerifier *auth.JWKSVerifier
	// Config handles（NewConfigOn 注册；Mux() 挂 /config-* 端点）
	configHandles     []anyConfigHandle
	configHandleTypes map[string]struct{} // 用于 NewConfigOn 幂等检测
	// keystore 懒加载 + Bootstrap 幂等保护
	// keystore：/config-pubkey handler 按需加载；handleSave/loadPersisted 不直接用
	// （走 c.dek 字段，由 Bootstrap 注入）。
	keystore      *keystore.Keystore
	keystoreOnce  sync.Once // 保护 getOrLoadKeystore 只加载一次
	bootstrapOnce sync.Once // 保护 Bootstrap 幂等：重复调只执行一次
	// 业务前端 dist 挂到 MCP 根 "/"，仅 config_mode="none"
	// + open_mode="fullpage" 场景用；与用户自行 Handle("/", ...) 冲突时 ServeMux panic。
	staticRootDir string
	// Capability Mesh：本应用 provides 的 capability 注册表。
	capabilities map[string]*capabilityEntry
	// finalize 期缓存的 manifest capability specs（供 wireMCPToolBackend 四象限遍历）。
	manifestCaps []kstypes.CapabilitySpec
	// finalize 路径幂等保护：Mux 入口幂等调，重复 build 不重注册 tool。
	finalizeOnce sync.Once
	finalizeErr  error
	// http_endpoint backend 的 ScopedJWT 验签器；nil 时 Mux 启用前懒建。
	scopedVerifier *ScopedJWTVerifier
	// caller-side：CallCapability lazy 构造（依赖 KS_APP_TOKEN + KS_GATEWAY_URL env）。
	dispatcherClient *DispatcherClient
	eventsClient     *EventsClient
}

// New 创建一个新的 App。id 为应用唯一标识（如 "my-app"）。
func New(id string, opts ...Option) *App {
	// 启动期一次性从 keystone 拉取托管资源凭证（仅 KS_APP_TOKEN+KS_GATEWAY_URL 都存在时）。
	// 必须在下面 loadAppConfig / newLLMClient 读 env 之前调用，这样它们才能读到注入值。
	// 失败仅 slog.Warn，不让 New panic。
	maybeFetchKeystoneManagedEnv()

	app := &App{
		id:                id,
		toolNames:         make(map[string]struct{}),
		config:            loadAppConfig(),
		llm:               newLLMClient(),
		embedding:         newEmbeddingClient(),
		authMode:          kstypes.AuthModeNone,
		version:           "0.1.0",
		manifestPath:      "manifest.yaml",
		protocolVersion:   "1.0", // 对齐 meta.Declare 默认值
		configHandleTypes: make(map[string]struct{}),
		capabilities:      make(map[string]*capabilityEntry),
	}
	for _, opt := range opts {
		opt(app)
	}
	// manifest 作为 fallback 鉴权来源（代码 Option 未设时生效）
	// 在 finalize 阶段完成，此处仅应用 Option。
	return app
}

// Tool 注册一个工具到 App。返回 *App 以支持链式调用。
// 若同名工具已注册，panic（与 Python SDK 保持一致，配置期错误快速失败）。
func (a *App) Tool(name, description string, handler ToolHandler) *App {
	if _, exists := a.toolNames[name]; exists {
		panic(fmt.Sprintf("tool %q 已经注册过了，禁止重复注册", name))
	}
	a.toolNames[name] = struct{}{}
	a.tools = append(a.tools, ToolDef{
		Name:        name,
		Description: description,
		Handler:     handler,
	})
	return a
}

// ToolWithSchema 注册一个工具并显式指定 inputSchema（JSON Schema）。
// 当 handler 参数结构无法从代码自动推断时使用；否则用 Tool() 足矣。
// 复用 Tool 的重复检测与注册逻辑，返回 *App 以支持链式调用。
func (a *App) ToolWithSchema(name, description string, schema map[string]any, handler ToolHandler) *App {
	a.Tool(name, description, handler)
	a.tools[len(a.tools)-1].InputSchema = schema
	return a
}

// RegisterTool 通过 ToolBuilder 注册工具（widgets-protocol-v1 推荐方式）。
// 与 App.Tool / App.ToolWithSchema 共享同一份 a.tools / a.toolNames 注册表，
// 重复名称 panic（与既有 Tool() 一致，配置期错误快速失败）。
//
// 如果 builder 设置了 WithToolUI，/meta 输出会自动声明 capabilities.ui.enabled=true
// 且 tools[]._meta.ui 包含 widget binding；详见 health.go:registerMetaEndpoint。
func (a *App) RegisterTool(b *ToolBuilder) *App {
	def := b.Build()
	if _, exists := a.toolNames[def.Name]; exists {
		panic(fmt.Sprintf("tool %q 已经注册过了，禁止重复注册", def.Name))
	}
	a.toolNames[def.Name] = struct{}{}
	a.tools = append(a.tools, def)
	return a
}

// Handle 注册一个自定义 HTTP 路由（如 REST API、静态文件服务）。
// pattern 遵循 Go 1.22+ 的 ServeMux 模式语法（如 "GET /api/v1/items"）。
// 返回 *App 以支持链式调用。
func (a *App) Handle(pattern string, handler http.Handler) *App {
	a.routes = append(a.routes, customRoute{pattern: pattern, handler: handler})
	return a
}

// HandleFunc 注册自定义路由的便捷方法，接受函数而非 http.Handler。
func (a *App) HandleFunc(pattern string, handler http.HandlerFunc) *App {
	return a.Handle(pattern, handler)
}

// Use 注册一个 HTTP 中间件，按注册顺序从外到内包装。
// 中间件会应用到所有路由（含 SDK 内置的 MCP、health 端点）。
// 返回 *App 以支持链式调用。
func (a *App) Use(mw Middleware) *App {
	a.middlewares = append(a.middlewares, mw)
	return a
}

// HealthCheck 注册一个自定义健康检查项。/healthz 端点会聚合所有注册的检查项，
// 任一失败则整体返回 503。name 用于在响应中标识具体失败的检查项。
// 返回 *App 以支持链式调用。
func (a *App) HealthCheck(name string, check func() error) *App {
	a.healthChecks = append(a.healthChecks, HealthChecker{Name: name, Check: check})
	return a
}

// DeclareNav 声明 MCP 在 Keystone 后台左侧菜单的导航项。
// 对齐 ks-types v0.5.0 MetaNavDecl 与 Python/TS SDK declareNav。
// 重复调用以最后一次为准（与 TS SDK 语义一致）。
func (a *App) DeclareNav(nav meta.NavDecl) *App {
	a.nav = &nav
	return a
}

// DeclarePermission 追加一条权限码目录条目。
// 对齐 ks-types v0.5.0 MetaPermissionDecl。
// code 格式应为 mcp.{mcp_id}.{action}。多次调用累加。
func (a *App) DeclarePermission(perm meta.PermissionDecl) *App {
	a.permissions = append(a.permissions, perm)
	return a
}

// SetConfigMode 设置配置模式分类（schema / iframe / none）。
// 与 DeclareConfigUI 的共存约定见 ks-types meta.go:52 起：
//   - "iframe" 必须同时调 DeclareConfigUI 填 URL
//   - "schema" 与 "none" 不应调 DeclareConfigUI
//
// 非法 mode 立即 panic（与 Python SDK ValueError 对齐，启动期快速失败）。
func (a *App) SetConfigMode(mode string) *App {
	switch mode {
	case "schema", "iframe", "none":
	default:
		panic(fmt.Sprintf("ksapp: config_mode 必须是 schema/iframe/none，收到 %q", mode))
	}
	a.configMode = mode
	return a
}

// SetConfigStatus 设置 MCP 配置状态。
// 枚举：unconfigured / via_frontend / via_cli / mixed。
// 非法枚举立即 panic。
func (a *App) SetConfigStatus(status string) *App {
	switch status {
	case "unconfigured", "via_frontend", "via_cli", "mixed":
	default:
		panic(fmt.Sprintf("ksapp: config_status 枚举越界: %q", status))
	}
	a.configStatus = status
	return a
}

// SetProtocolVersion 设置 MCP 协议版本（SemVer MAJOR.MINOR，MVP "1.0"）。
// 不调用时默认 "1.0"（与 meta.Declare 默认一致）。
func (a *App) SetProtocolVersion(version string) *App {
	a.protocolVersion = version
	return a
}

// DeclareConfigUI 设置 iframe 模式的渲染信息（URL / enabled）。
// 仅当 config_mode == "iframe" 时有意义；其他模式不应调用。
func (a *App) DeclareConfigUI(ui meta.ConfigUIInfo) *App {
	a.configUI = &ui
	return a
}

// MountStaticRoot 挂载业务前端 dist 为 MCP 根路径 "/" 的静态文件服务。
//
// 仅 config_mode=="none" 时允许调用；用于 open_mode=="fullpage" 场景（业务主界面
// 由 keystone 前端通过反代承载）。底层挂 http.FileServer(http.Dir(dir)) + SPA fallback
// （未匹配文件时返回 index.html，对齐 Python Starlette html=True 的 SPA 扩展语义）。
//
// 调用顺序：必须先 SetConfigMode("none") 再调本方法。
//
// 冲突：若用户已通过 Handle("/", ...) 注册根路径处理，Mux() 构造时 ServeMux 会 panic。
//
// panic：
//   - config_mode 未设或非 "none"
//   - 重复调用（staticRootDir 已写入）
func (a *App) MountStaticRoot(dir string) *App {
	if a.configMode != "none" {
		panic(fmt.Sprintf("ksapp: MountStaticRoot 只能在 config_mode=\"none\" 时调用，当前为 %q", a.configMode))
	}
	if a.staticRootDir != "" {
		panic("ksapp: MountStaticRoot 已被调用，不允许重复")
	}
	a.staticRootDir = dir
	return a
}

// ConfigUIMiddleware 返回 RequireConfigUIJWT middleware 用于保护 /config-* 端点。
// 与 App.Mux() 共享同一个 JWKSVerifier（懒初始化，避免重复拉 JWKS）。
// 鉴权模式非 keystone_jwks 时返回 no-op（pass-through），便于本地开发。
func (a *App) ConfigUIMiddleware() Middleware {
	v := a.ensureVerifier()
	if v == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	return auth.RequireConfigUIJWT(v)
}

// ensureVerifier 懒初始化并缓存 sharedVerifier。
// keystone_jwks 模式下首次调用创建实例；其他模式返回 nil。
// Mux() 和 ConfigUIMiddleware() 均通过此方法获取 verifier，确保共享同一实例。
func (a *App) ensureVerifier() *auth.JWKSVerifier {
	if a.sharedVerifier != nil {
		return a.sharedVerifier
	}
	effMode, jwksURL, err := resolveAuth(a)
	if err != nil {
		panic(fmt.Sprintf("ksapp: 鉴权解析失败: %v", err))
	}
	if effMode != kstypes.AuthModeKeystoneJWKS {
		return nil
	}
	a.sharedVerifier = auth.NewJWKSVerifier(jwksURL)
	return a.sharedVerifier
}

// LLM 返回 Keystone LLM Relay 客户端（已在 New 中预初始化，线程安全）。
// 要求 manifest 声明 permissions.llm: host_proxy，Keystone 会注入 KS_RELAY_TOKEN
// 环境变量。本地开发时开发者需自行设置 KS_RELAY_TOKEN。
func (a *App) LLM() *LLMClient {
	return a.llm
}

// Embedding 返回 Keystone 托管 embedding 客户端。
func (a *App) Embedding() *EmbeddingClient {
	return a.embedding
}

// VectorStore 返回指定业务 collection 的向量库客户端。
func (a *App) VectorStore(collection string) *VectorStoreClient {
	return newVectorStoreClient(a.embedding, collection)
}

// mcpToolDefs 把 ksapp 的 []ToolDef 转成 mcpproto 的 []mcpproto.ToolDef（去掉 Handler）。
func (a *App) mcpToolDefs() []mcpproto.ToolDef {
	defs := make([]mcpproto.ToolDef, len(a.tools))
	for i, t := range a.tools {
		defs[i] = mcpproto.ToolDef{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
			Annotations: t.Annotations,
		}
	}
	return defs
}

// callTool 按 name 路由到注册的 handler 并调用。
func (a *App) callTool(ctx context.Context, name string, args map[string]any) (any, error) {
	for _, t := range a.tools {
		if t.Name == name {
			return t.Handler(ctx, args)
		}
	}
	return nil, fmt.Errorf("工具 %q 未找到", name)
}

// Mux 构建并返回配置好所有路由和中间件的 http.Handler。
// 供需要自行管理 HTTP 服务器生命周期的高级场景使用。
func (a *App) Mux() http.Handler {
	// Capability Mesh：把 capability 注册表与 manifest 关联 + 自动注册 mcp_tool backend
	// 包装出的 MCP tool。必须先于 mcpToolDefs 调，否则 capability 包装出的 tool 看不到。
	if err := a.ensureCapabilityFinalized(); err != nil {
		panic(fmt.Sprintf("ksapp: capability finalize 失败: %v", err))
	}
	// http_endpoint wiring 与 ScopedJWTMiddleware 挂载（在主 mux 路由注册前算好 routes）。
	httpCapRoutes, pathToCapName, err := a.wireHTTPEndpointBackend()
	if err != nil {
		panic(fmt.Sprintf("ksapp: http_endpoint wiring 失败: %v", err))
	}
	// 懒初始化 verifier（ensureVerifier 内部做 resolveAuth + panic 失败门）。
	// 必须先拿 verifier 再取 effectiveMode：ensureVerifier 返回非 nil 即 keystone_jwks 模式。
	verifier := a.ensureVerifier()
	effectiveMode := a.authMode
	if verifier != nil {
		effectiveMode = kstypes.AuthModeKeystoneJWKS
	} else {
		// 非 keystone_jwks 分支也要走 resolveAuth 以触发 manifest fallback / 逃生 env 逻辑。
		// 语义保留：resolveAuth 报错 → panic；成功 → 用返回的 effectiveMode。
		m, _, err := resolveAuth(a)
		if err != nil {
			panic(fmt.Sprintf("ksapp: 鉴权解析失败: %v", err))
		}
		effectiveMode = m
	}

	mux := http.NewServeMux()
	registerHealthEndpoints(mux, a.id, a.healthChecks)
	registerMetaEndpoint(mux, a.id, a.version, effectiveMode, a.tools,
		a.nav, a.permissions, a.configMode, a.configStatus, a.protocolVersion, a.configUI)

	toolDefs := a.mcpToolDefs()

	// MCP 路由组：/mcp (POST) + /mcp/tools/call (POST) + /mcp/tools/list (GET)
	// 根据 effectiveMode 决定是否叠加 auth middleware
	mcpHandler := map[string]http.Handler{
		"POST /mcp":            mcpproto.NewStreamableHTTPHandler(a.id, a.version, toolDefs, a.callTool),
		"POST /mcp/tools/call": mcpproto.NewLegacyCallHandler(toolDefs, a.callTool),
		"GET /mcp/tools/list":  mcpproto.NewLegacyListHandler(toolDefs),
	}
	if verifier != nil {
		jwtMW := auth.RequireJWT(verifier)
		for pat, h := range mcpHandler {
			mux.Handle(pat, jwtMW(h))
		}
	} else {
		for pat, h := range mcpHandler {
			mux.Handle(pat, h)
		}
	}

	for _, r := range a.routes {
		mux.Handle(r.pattern, r.handler)
	}

	// http_endpoint capability：每个 path 套 ScopedJWTMiddleware（aud=canonical_name）。
	if len(httpCapRoutes) > 0 {
		if a.scopedVerifier == nil {
			a.scopedVerifier = NewScopedJWTVerifier(a.jwksURL)
		}
		scopedMW := ScopedJWTMiddleware(a.scopedVerifier, pathToCapName)
		for path, h := range httpCapRoutes {
			mux.Handle(path, scopedMW(h))
		}
	}

	// 有 Config handle 时自动挂 /config-* + /ks-config/* 端点，
	// 统一套 ConfigUIMiddleware（非 keystone_jwks 模式是 pass-through，本地/测试友好）。
	if len(a.configHandles) > 0 {
		cfgMW := a.ConfigUIMiddleware()
		mux.Handle("GET /config-schema", cfgMW(a.configSchemaHandler()))
		mux.Handle("GET /config-pubkey", cfgMW(a.configPubkeyHandler()))
		mux.Handle("GET /ks-config/current", cfgMW(a.configCurrentHandler()))
		mux.Handle("POST /ks-config/save", cfgMW(a.configSaveHandler()))
		mux.Handle("POST /ks-config/validate", cfgMW(a.configValidateHandler()))
	}

	// 业务前端 dist 挂到根 "/"，放在所有具体路由之后
	// （ServeMux 的 "/" 是兜底模式，具体路径+方法模式会优先命中）。
	if a.staticRootDir != "" {
		mux.Handle("/", spaStaticHandler(a.staticRootDir))
	}

	var handler http.Handler = mux
	for i := len(a.middlewares) - 1; i >= 0; i-- {
		handler = a.middlewares[i](handler)
	}
	return handler
}

// Run 启动 HTTP 服务器，监听配置端口，阻塞直到收到 SIGINT/SIGTERM。
// 这是 RunWithContext 的便捷方法，使用信号监听的 context。
func (a *App) Run() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	a.RunWithContext(ctx)
}

// deriveNavState 从 a.nav 推 (navState, openMode)，供 validateConfigConsistency 调 ks-types 矩阵。
// nav 未声明→NavAbsent；声明但字段不齐/枚举错→NavInvalid；否则→NavValid + openMode。
func (a *App) deriveNavState() (kstypes.NavState, string) {
	if a.nav == nil {
		return kstypes.NavAbsent, ""
	}
	if a.nav.Label == "" || a.nav.Category == "" || a.nav.OpenMode == "" {
		return kstypes.NavInvalid, ""
	}
	switch a.nav.OpenMode {
	case "dialog", "fullpage", "tab":
		return kstypes.NavValid, a.nav.OpenMode
	default:
		return kstypes.NavInvalid, ""
	}
}

// validateConfigConsistency 启动期对 nav/config_mode/config_ui 组合做一致性终检（A6）。
// 不一致即 panic（fail-fast），矩阵 + reason 来自 ks-types 单一事实源（与 keystone 摄入诊断同一张矩阵）。
func (a *App) validateConfigConsistency() {
	navState, openMode := a.deriveNavState()
	hasConfigUI := a.configUI != nil && a.configUI.Enabled
	if reason, ok := kstypes.CheckNavConfigConsistency(navState, openMode, a.configMode, hasConfigUI); !ok {
		panic(fmt.Sprintf("ksapp: 配置组合不一致，应用入口无法工作: %s", reason))
	}
}

// RunWithContext 启动 HTTP 服务器，阻塞直到 ctx 被取消。
// 适用于应用需要自行管理生命周期的场景（如后台 goroutine 协调退出）。
func (a *App) RunWithContext(ctx context.Context) {
	// 启动期组合一致性终检（A6）：nav/config_mode 不一致即 fail-fast，
	// 与 keystone 摄入诊断共用 ks-types 同一张矩阵。
	a.validateConfigConsistency()
	// 启动期先 Bootstrap 注入 DEK，再构建 Mux。
	// 目的：把"NewConfigOn 注册但 Bootstrap 漏调"的暴露从"首次 save panic"提前到
	// "启动完成前 panic"，fail-fast。
	a.Bootstrap()
	handler := a.Mux()

	port := a.config.Port
	addr := fmt.Sprintf(":%d", port)
	srv := &http.Server{Addr: addr, Handler: handler}

	go func() {
		slog.Info("starting app", "id", a.id, "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down", "id", a.id)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
}

// registerConfigHandleSlot 在 NewConfigOn 注册时调用；同 T 二次注册 panic。
func (a *App) registerConfigHandleSlot(typeName string) {
	if _, ok := a.configHandleTypes[typeName]; ok {
		panic(fmt.Sprintf("ksapp: 同一 App 的 NewConfigOn[%s] 重复调用", typeName))
	}
	a.configHandleTypes[typeName] = struct{}{}
}

// bootstrapConfigDir 是 Bootstrap 注入 persist/dek 文件所用的默认目录。
// 与 keystore 默认 fallback 目录（config/.mcp-key）保持一致，方便一处部署。
const bootstrapConfigDir = "config"

func bootstrapConfigDirPath() string {
	if dir := os.Getenv("KS_APP_CONFIG_DIR"); dir != "" {
		return dir
	}
	return bootstrapConfigDir
}

// Bootstrap 完成启动期的一次性初始化工作：
//
//  1. 若未注册 Config handle → 直接 return（无 handle 无需加载 keystore）
//  2. 懒加载 keystore（失败 panic — 部署错配）
//  3. 加载或首次生成 DEK（.local-dek）
//  4. 把 persistPath / dekPath / dek 注入每个 Config handle
//  5. 逐个校验 handle.hasDEK() == true（dek 未注入 = bug，fail-fast panic）
//
// 幂等：bootstrapOnce 保证重复调用只执行一次。RunWithContext 会自动调 Bootstrap，
// 高级场景（如用户自己管 HTTP server）可主动调一次，多次调用不会引入副作用。
//
// Export 理由：SDK 用户可能不走 RunWithContext（例如测试里直接用 Mux() + net/http），
// 但仍需要依赖 Bootstrap 的"dek 注入完成"语义前置；Bootstrap 暴露为公开方法让用户
// 显式调用，语义清晰。
func (a *App) Bootstrap() {
	a.bootstrapOnce.Do(func() {
		if len(a.configHandles) == 0 {
			return
		}
		// 1) 加载 keystore（失败 panic — 部署错配，programmer-error fail-fast）
		_ = a.getOrLoadKeystore()

		// 2) 加载或首次生成独立 DEK
		configDir := bootstrapConfigDirPath()
		persistPath := filepath.Join(configDir, "mcp-config.enc")
		dekPath := filepath.Join(configDir, ".local-dek")
		dek, err := keystore.LoadOrGenerateDEK(dekPath)
		if err != nil {
			panic(fmt.Sprintf("ksapp.Bootstrap: LoadOrGenerateDEK(%s) 失败 — 部署错配（config/ 目录不可写？）: %v", dekPath, err))
		}

		// 3) 注入每个 Config handle
		for _, h := range a.configHandles {
			h.bootstrapPersistence(persistPath, dekPath, dek)
		}

		// 4) 校验：注入后每个 handle 必须 hasDEK()（否则是 bootstrapPersistence 实现 bug）
		a.verifyConfigHandlesHaveDEK()

		for _, h := range a.configHandles {
			if restored, err := h.restorePersisted(context.Background()); err != nil {
				slog.Warn("ksapp: 恢复持久化配置失败", "type", h.typeName(), "error", err)
			} else if restored {
				slog.Info("ksapp: 已恢复持久化配置", "type", h.typeName())
			}
		}
	})
}

// verifyConfigHandlesHaveDEK 遍历所有 Config handle，任一 hasDEK()=false 即 panic。
//
// 这是独立校验入口：既用于 Bootstrap 内部注入后自检，也可被测试单独调用
// 来验证"注入漏了"时的 fail-fast 行为。
//
// 失败信息指出首个漏注入的 handle typeName，便于定位 bug。
func (a *App) verifyConfigHandlesHaveDEK() {
	for _, h := range a.configHandles {
		if !h.hasDEK() {
			panic(fmt.Sprintf("ksapp: Config handle %q 的 dek 未注入（Bootstrap 缺陷 — bootstrapPersistence 未生效）", h.typeName()))
		}
	}
}

// SetScopedJWTTestKey 注入静态 RSA 公钥用作 ScopedJWT 验签（跳过 JWKS 拉取）。
// 主要给单测 / 受信环境（启动期注入 keystone 公钥）用。
// 多次调用会累积到同一 verifier。
func (a *App) SetScopedJWTTestKey(kid, pubPEM string) error {
	if a.scopedVerifier == nil {
		a.scopedVerifier = NewScopedJWTVerifier("")
	}
	return a.scopedVerifier.SetStaticKey(kid, pubPEM)
}

// CallCapability 返回一个 CapabilityCall 构造器（caller-side）。
//
// 命名说明：spec 写的是 App.Capability(name)，但 ksapp 中 RegisterCapability 已是
// provides-side 注册入口；这里 caller-side 用 CallCapability 区分（与 Python
// app.call_capability 命名对齐）。
//
// 依赖 env：KS_APP_TOKEN + KS_GATEWAY_URL；缺一返 panic（caller-side 不可用）。
func (a *App) CallCapability(name string) *CapabilityCall {
	d := a.getDispatcherClient()
	if d == nil {
		panic("ksapp: KS_APP_TOKEN + KS_GATEWAY_URL 未配置；CallCapability 不可用")
	}
	return &CapabilityCall{
		canonicalName: name,
		dispatcher:    d,
		events:        a.getEventsClient(),
	}
}

// SetDispatcherClient 测试钩子：注入定制 DispatcherClient（绕过 env 探测）。
func (a *App) SetDispatcherClient(d *DispatcherClient) {
	a.dispatcherClient = d
}

func (a *App) getDispatcherClient() *DispatcherClient {
	if a.dispatcherClient != nil {
		return a.dispatcherClient
	}
	token := os.Getenv("KS_APP_TOKEN")
	gw := os.Getenv("KS_GATEWAY_URL")
	if token == "" || gw == "" {
		return nil
	}
	a.dispatcherClient = NewDispatcherClient(gw, token)
	return a.dispatcherClient
}

// getEventsClient lazy 启动 inbound 事件通道。
// 缺 KS_APP_TOKEN / KS_GATEWAY_URL 返 nil → Task.Events 退回 snapshot 模式。
// KS_EVENTS_MODE=polling 切到 polling，默认 WS。
func (a *App) getEventsClient() *EventsClient {
	if a.eventsClient != nil {
		return a.eventsClient
	}
	token := os.Getenv("KS_APP_TOKEN")
	gw := os.Getenv("KS_GATEWAY_URL")
	if token == "" || gw == "" {
		return nil
	}
	mode := EventsModeWS
	if os.Getenv("KS_EVENTS_MODE") == "polling" {
		mode = EventsModePolling
	}
	a.eventsClient = NewEventsClient(gw, token, mode)
	a.eventsClient.Start(context.Background())
	return a.eventsClient
}

// SetEventsClient 测试钩子：注入定制 EventsClient（绕过 env 探测，不自动 Start）。
func (a *App) SetEventsClient(e *EventsClient) {
	a.eventsClient = e
}

// loadManifestCapabilities 读取本地 manifest.yaml 解析 provides.capabilities[]。
// 利用 ks-types v0.19.0 的 ParseAppSpec —— 单一信息源，与后端一致。
//
// manifest 不存在返回 (nil, nil)，不视为错误（本地开发或测试可能无 manifest）。
func (a *App) loadManifestCapabilities() ([]kstypes.CapabilitySpec, error) {
	if a.manifestPath == "" {
		return nil, nil
	}
	data, err := os.ReadFile(a.manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read manifest %s: %w", a.manifestPath, err)
	}
	spec, err := kstypes.ParseAppSpec(data)
	if err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", a.manifestPath, err)
	}
	return spec.Provides.Capabilities, nil
}
