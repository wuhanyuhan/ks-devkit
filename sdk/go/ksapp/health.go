package ksapp

import (
	"encoding/json"
	"net/http"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/meta"
	kstypes "github.com/wuhanyuhan/ks-types"
)

// registerHealthEndpoints 注册 /healthz 与 /readyz 存活/就绪探针端点。
// healthChecks 为用户通过 App.HealthCheck() 注册的自定义检查项；
// 若有任一检查失败，/healthz 返回 503 并列出失败项。
func registerHealthEndpoints(mux *http.ServeMux, appID string, healthChecks []HealthChecker) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if len(healthChecks) == 0 {
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			return
		}

		failures := make(map[string]string)
		for _, hc := range healthChecks {
			if err := hc.Check(); err != nil {
				failures[hc.Name] = err.Error()
			}
		}
		if len(failures) > 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "unhealthy",
				"checks": failures,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
}

// registerMetaEndpoint 注册 GET /meta 端点，返回符合 kstypes.MetaResponse 协议的响应。
// 把 App.DeclareNav / DeclarePermission / SetConfigMode /
// SetConfigStatus / SetProtocolVersion / DeclareConfigUI 的声明反射到响应里。
// 传 nil / 空串时对应字段因 kstypes.MetaResponse omitempty 而不出现在 JSON 中。
func registerMetaEndpoint(
	mux *http.ServeMux,
	appID, version string,
	authMode kstypes.AuthMode,
	tools []ToolDef,
	nav *meta.NavDecl,
	permissions []meta.PermissionDecl,
	configMode, configStatus, protocolVersion string,
	configUI *meta.ConfigUIInfo,
) {
	mux.HandleFunc("GET /meta", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		toolInfos := make([]kstypes.ToolInfo, len(tools))
		// hasUIBinding 任一工具携带 widget binding 时翻 true，
		// 用来声明 /meta.capabilities.ui.enabled。
		hasUIBinding := false
		for i, t := range tools {
			info := kstypes.ToolInfo{
				Name:        t.Name,
				Description: t.Description,
			}
			if t.UIBinding != nil {
				// widgets-protocol-v1 (v0.6.0)：把 ToolBuilder.WithToolUI 声明的
				// binding 注入到 /meta.tools[]._meta.ui，供 keystone 平台
				// mount 时回写 tool UI 注册表。
				info.Meta = &kstypes.ToolInfoMeta{UI: t.UIBinding}
				hasUIBinding = true
			}
			toolInfos[i] = info
		}
		resp := kstypes.MetaResponse{
			Name:            appID,
			Version:         version,
			AuthMode:        authMode,
			Tools:           toolInfos,
			ConfigMode:      configMode,
			ProtocolVersion: protocolVersion,
			ConfigStatus:    configStatus,
		}
		if hasUIBinding {
			resp.Capabilities = &kstypes.MetaCapabilities{
				UI: &kstypes.CapabilitiesUI{Enabled: true},
			}
		}
		if nav != nil {
			resp.Nav = convertNavDecl(*nav)
		}
		if len(permissions) > 0 {
			resp.Permissions = convertPermDecls(permissions)
		}
		if configUI != nil {
			resp.ConfigUI = convertConfigUI(*configUI)
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
}

// convertNavDecl 把 ksapp/meta.NavDecl 字段逐一拷到 kstypes.MetaNavDecl。
// 两个类型字段命名与类型完全对齐（v0.5.0），但显式 copy 避免将来任一侧单方面加字段时静默失误。
func convertNavDecl(n meta.NavDecl) *kstypes.MetaNavDecl {
	return &kstypes.MetaNavDecl{
		Label:         n.Label,
		Icon:          n.Icon,
		Category:      n.Category,
		Order:         n.Order,
		OpenMode:      n.OpenMode,
		EntryPath:     n.EntryPath,
		RequiredPerms: n.RequiredPerms,
	}
}

// convertPermDecls 批量转换 PermissionDecl 切片。
func convertPermDecls(ps []meta.PermissionDecl) []kstypes.MetaPermissionDecl {
	out := make([]kstypes.MetaPermissionDecl, len(ps))
	for i, p := range ps {
		out[i] = kstypes.MetaPermissionDecl{
			Code:         p.Code,
			Label:        p.Label,
			DefaultRoles: p.DefaultRoles,
		}
	}
	return out
}

// convertConfigUI 把 ksapp/meta.ConfigUIInfo 转到 kstypes.ConfigUIInfo。
func convertConfigUI(u meta.ConfigUIInfo) *kstypes.ConfigUIInfo {
	return &kstypes.ConfigUIInfo{
		Enabled: u.Enabled,
		URL:     u.URL,
	}
}
