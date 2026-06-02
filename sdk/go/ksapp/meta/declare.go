// Package meta helps MCP SDK users declare /meta.nav + .permissions + .config_mode.
package meta

// NavDecl 是 MCP 自声明的左侧菜单项（JSON 等价于 ks-types MetaNavDecl）。
type NavDecl struct {
	Label         string   `json:"label"`
	Icon          string   `json:"icon,omitempty"`
	Category      string   `json:"category"`
	Order         int      `json:"order,omitempty"`
	OpenMode      string   `json:"open_mode"`
	EntryPath     string   `json:"entry_path,omitempty"`
	RequiredPerms []string `json:"required_perms,omitempty"`
}

// PermissionDecl 是 MCP 自声明的权限码目录条目（JSON 等价于 ks-types MetaPermissionDecl）。
type PermissionDecl struct {
	Code         string   `json:"code"`
	Label        string   `json:"label"`
	DefaultRoles []string `json:"default_roles,omitempty"`
}

// ConfigUIInfo 描述 service 带界面接入（iframe 嵌入 keystone 后台）的信息
// （JSON 等价于 ks-types ConfigUIInfo）。
//   - Enabled：是否提供配置界面（无 omitempty，零值会序列化为 "enabled":false，与 ks-types 一致）
//   - URL：界面基础路径；相对路径基于服务自身地址解析，也可绝对 URL
//
// 共存约定（ks-types meta.go:52 起）：
//   - ConfigMode == "iframe" → 必须同时填 ConfigUI.URL（启动校验）
//   - ConfigMode == "schema" → ConfigUI 应为 nil
//   - ConfigMode == "none"   → ConfigUI 为 nil 或 Enabled=false
type ConfigUIInfo struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url,omitempty"`
}

// Response 描述一份完整 /meta 响应（供 MCP 序列化用）。
// JSON 形态与 ks-types MetaResponse 保持等价（v0.5.0 起含
// nav/permissions/config_mode/config_ui/protocol_version/config_status）。
//
// 使用约定：
//
// 本类型仅是响应体 DTO，调用方需自行挂 /meta 路由吐此 JSON。
// 当前 ksapp App.Serve() 独占 /meta 且未对接 meta.Declare，App 级接入
// （WithMeta AppOption 等）后续在 Go SDK 落地。
// 在此之前请用裸 net/http 或 gin 自挂路由，示例：
//
//	resp := meta.Declare("doc", "1.0.0", meta.WithNav(...))
//	http.HandleFunc("/meta", func(w http.ResponseWriter, r *http.Request) {
//	    w.Header().Set("Content-Type", "application/json")
//	    _ = json.NewEncoder(w).Encode(resp)
//	})
//
// 注意：请始终通过 Declare() 构造 Response；裸 Response{} 零值初始化会
// 序列化出 "auth_mode":""，与 ks-types MetaResponse omitempty 语义不一致。
type Response struct {
	Name            string           `json:"name"`
	Version         string           `json:"version"`
	AuthMode        string           `json:"auth_mode"`
	Tools           []map[string]any `json:"tools,omitempty"`
	Nav             *NavDecl         `json:"nav,omitempty"`
	Permissions     []PermissionDecl `json:"permissions,omitempty"`
	ConfigMode      string           `json:"config_mode,omitempty"`      // schema / iframe / none
	ConfigUI        *ConfigUIInfo    `json:"config_ui,omitempty"`        // iframe 模式的接入信息
	ProtocolVersion string           `json:"protocol_version,omitempty"` // "1.0"
	ConfigStatus    string           `json:"config_status,omitempty"`    // unconfigured / via_frontend / via_cli / mixed
}

// Declare 是便捷构造器：避免 MCP 侧手写字段错。
// 具体使用约定与 App.Serve() 对接现状见 Response struct 的 doc。
func Declare(name, version string, opts ...Option) *Response {
	r := &Response{
		Name:            name,
		Version:         version,
		AuthMode:        "keystone_jwks",
		ProtocolVersion: "1.0",
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Option 是 Declare 的函数式选项。
type Option func(*Response)

// WithNav 设置 /meta.nav 左侧菜单声明。
func WithNav(nav NavDecl) Option { return func(r *Response) { r.Nav = &nav } }

// WithPermissions 批量设置权限码目录。
func WithPermissions(perms ...PermissionDecl) Option {
	return func(r *Response) { r.Permissions = perms }
}

// WithConfigMode 设置配置模式分类（schema / iframe / none）。
func WithConfigMode(mode string) Option { return func(r *Response) { r.ConfigMode = mode } }

// WithConfigUI 设置 iframe 模式的渲染 URL 与启用位。
// 仅当 ConfigMode == "iframe" 时有意义；此时 keystone 后端会做 ConfigUI.URL
// 非空校验（启动校验）。
func WithConfigUI(ui ConfigUIInfo) Option { return func(r *Response) { r.ConfigUI = &ui } }

// WithConfigStatus 设置配置状态（unconfigured / via_frontend / via_cli / mixed）。
func WithConfigStatus(s string) Option { return func(r *Response) { r.ConfigStatus = s } }

// WithTools 设置 tools 概览清单。
func WithTools(tools ...map[string]any) Option { return func(r *Response) { r.Tools = tools } }
