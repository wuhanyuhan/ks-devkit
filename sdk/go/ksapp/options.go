package ksapp

import (
	"os"

	kstypes "github.com/wuhanyuhan/ks-types"
)

// Option 是 New(id, opts...) 的配置函数。
// Option 之间后设覆盖先设，允许链式 "开启然后关闭"（WithKeystoneAuth + WithoutAuth）。
type Option func(*App)

// WithKeystoneAuth 开启 keystone_jwks 鉴权模式。
// 从 KEYSTONE_JWKS_URL env 读取 JWKS 端点。
// 若 strict-by-default 触发（URL 空且未设置 KS_APP_AUTH_MODE=insecure），
// 在 Run() / Mux() 阶段 panic。
func WithKeystoneAuth() Option {
	return func(a *App) {
		a.authMode = kstypes.AuthModeKeystoneJWKS
		a.jwksURL = os.Getenv("KEYSTONE_JWKS_URL")
	}
}

// WithoutAuth 显式关闭鉴权（覆盖任何之前的 WithKeystoneAuth 或 manifest 声明）。
// 用于本地开发或测试环境。
func WithoutAuth() Option {
	return func(a *App) {
		a.authMode = kstypes.AuthModeNone
		a.jwksURL = ""
	}
}

// WithVersion 设置应用版本号（反映在 /meta 端点）。
// 未设置时默认 "0.1.0"。
func WithVersion(v string) Option {
	return func(a *App) {
		a.version = v
	}
}

// WithManifest 指定 manifest.yaml 的路径（默认 "./manifest.yaml"）。
// SDK 在 New() 时读取 manifest 的 mount.service.auth_mode 作为默认鉴权模式，
// 但代码 Option（WithKeystoneAuth/WithoutAuth）始终优先。
func WithManifest(path string) Option {
	return func(a *App) {
		a.manifestPath = path
	}
}
