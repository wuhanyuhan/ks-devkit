package ksapp

import (
	"fmt"
	"os"

	kstypes "github.com/wuhanyuhan/ks-types"
	"gopkg.in/yaml.v3"
)

// resolveAuth 按三层优先级决定 effective auth mode 和 JWKS URL：
//
//  1. 代码 Option（WithKeystoneAuth / WithoutAuth）已经写入 a.authMode，
//     此函数视其为最高优先级。但默认值 AuthModeNone 在无 Option 时等价于"未设置"，
//     此时 fallback 到第 2 层。
//  2. manifest.yaml 的 mount.service.auth_mode 或 mount.extension.auth_mode
//     （legacy 字段；v0.19.0 ks-types schema 已剥离 service / extension，SDK 用
//     低层 yaml dict 拆字段保留 backward compat，与 Python `load_manifest_auth_mode` 对齐）。
//  3. 默认 AuthModeNone。
//
// KS_APP_AUTH_MODE=insecure env 是全局逃生，最终 effective 强制降级为 AuthModeNone。
//
// strict-by-default：最终 effective=keystone_jwks 且 jwksURL="" → panic。
//
// manifest 不存在不是错误（返回 nil）；manifest 存在但解析失败返回 error。
func resolveAuth(a *App) (effective kstypes.AuthMode, jwksURL string, err error) {
	effective = a.authMode
	jwksURL = a.jwksURL

	// 代码 Option 未设置（authMode 仍是默认 none 且 jwksURL 为空） → 读 manifest
	if effective == kstypes.AuthModeNone && a.jwksURL == "" {
		if data, readErr := os.ReadFile(a.manifestPath); readErr == nil {
			am, parseErr := loadManifestAuthMode(data)
			if parseErr != nil {
				return "", "", fmt.Errorf("解析 %s 失败: %w", a.manifestPath, parseErr)
			}
			if am != "" {
				mode := kstypes.AuthMode(am)
				if !mode.Valid() {
					return "", "", fmt.Errorf("校验 %s 失败: auth_mode=%q 非法（合法值：none/keystone_jwks）", a.manifestPath, am)
				}
				m := mode.Default()
				if m != kstypes.AuthModeNone {
					effective = m
					if m == kstypes.AuthModeKeystoneJWKS {
						jwksURL = os.Getenv("KEYSTONE_JWKS_URL")
					}
				}
			}
		}
		// manifest 不存在不是错误（本地开发或测试可能无 manifest）
	}

	// 全局逃生
	if os.Getenv("KS_APP_AUTH_MODE") == "insecure" {
		return kstypes.AuthModeNone, "", nil
	}

	// strict-by-default
	if effective == kstypes.AuthModeKeystoneJWKS && jwksURL == "" {
		panic("ksapp: auth_mode=keystone_jwks 但 KEYSTONE_JWKS_URL 未配置；" +
			"生产必须设置此 env，或本地开发用 KS_APP_AUTH_MODE=insecure 降级")
	}

	return effective, jwksURL, nil
}

// loadManifestAuthMode 从 manifest.yaml 原始字节读取 mount.service.auth_mode，
// 找不到则回退 mount.extension.auth_mode。两者均缺失返回 ""（不是错误）。
//
// v0.19.0 ks-types 已删除 MountSpec.Service / Extension 字段，但应用 manifest
// 仍可能含这些 legacy 字段；SDK 用低层 yaml dict 解析维持 backward compat，
// 与 Python sdk/python/src/ks_app/manifest.py:load_manifest_auth_mode 对齐。
func loadManifestAuthMode(data []byte) (string, error) {
	var raw struct {
		Mount struct {
			Service   map[string]any `yaml:"service"`
			Extension map[string]any `yaml:"extension"`
		} `yaml:"mount"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return "", err
	}
	if v, ok := raw.Mount.Service["auth_mode"].(string); ok && v != "" {
		return v, nil
	}
	if v, ok := raw.Mount.Extension["auth_mode"].(string); ok && v != "" {
		return v, nil
	}
	return "", nil
}
