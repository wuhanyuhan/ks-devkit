package ksapp

import (
	"context"
	"log/slog"
	"os"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp/keystoneclient"
)

// maybeFetchKeystoneManagedEnv 启动期一次性从 keystone 拉取本应用被分配的
// 托管资源凭证并堆到 os.Environ。
//
// 触发条件：KS_APP_TOKEN + KS_GATEWAY_URL 同时非空字符串。任一缺失则跳过——
// 生产容器化场景没有 KS_APP_TOKEN，走 runtime.env 路径不变。
//
// 注入策略：仅当 key 未在 os.Environ 中时写入（等价 Python 的 setdefault），
// 本地 .env.local 手填值优先于 keystone 拉取值，调试友好。
//
// 失败不 panic：网络/HTTP/解析任何错误都只打 slog.Warn，让 application 层
// 在校验必填字段时报更具体的错。
func maybeFetchKeystoneManagedEnv() {
	token := os.Getenv("KS_APP_TOKEN")
	gateway := os.Getenv("KS_GATEWAY_URL")
	if token == "" || gateway == "" {
		return
	}

	client := keystoneclient.New(gateway, token)
	env, err := client.FetchEnv(context.Background())
	if err != nil {
		slog.Warn("ksapp: fetch keystone managed env failed", "error", err)
		return
	}

	injected := 0
	for k, v := range env {
		if _, exists := os.LookupEnv(k); exists {
			continue
		}
		if err := os.Setenv(k, v); err != nil {
			slog.Warn("ksapp: setenv failed", "key", k, "error", err)
			continue
		}
		injected++
	}
	if injected > 0 {
		slog.Info("ksapp: injected env vars from keystone", "count", injected)
	}
}
