// config-demo — ks-devkit/sdk/go config-schema 示例。
//
// 演示类型化配置（install.yaml 退役后的官方替代）：
//   - 用 ksconfig struct tag 声明字段约束 + UI 元信息（SDK 反射出 JSON Schema + UI Schema）
//   - OnValidate 做连接测试（安装向导点"测试连接"时触发）
//   - OnApply 应用新配置（热切换，失败内存 + 磁盘双回滚）
//   - 业务里用 cfg.Get() 读当前快照（不再读 env）
//
// 完整说明见 docs/config-schema.md。
//
// Usage:
//
//	cd ks-devkit/sdk/go/examples/config-demo
//	go run .
package main

import (
	"context"
	"errors"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp"
)

// Config 用 ksconfig struct tag 声明字段约束与 UI 元信息。
// tag 文法见 docs/config-schema.md「字段声明文法」一节。
type Config struct {
	APIKey     string `ksconfig:"required,type:password,label_zh:API 密钥,label_en:API Key,hint:从控制台获取"`
	Region     string `ksconfig:"enum:cn|us|eu,default:cn,label:区域"`
	MaxRetries int    `ksconfig:"default:3,min:1,max:10,label:最大重试次数"`
}

func main() {
	app := ksapp.New("config-demo",
		ksapp.WithKeystoneAuth(),
		ksapp.WithVersion("0.1.0"),
	)

	cfg := ksapp.NewConfigOn(app, ksapp.ConfigSpec[Config]{
		OnValidate: func(ctx context.Context, c *Config) error {
			// 连接测试：用 c.APIKey 探活下游服务；返回 error → 安装向导显示校验失败。
			if c.APIKey == "" {
				return errors.New("API 密钥不能为空")
			}
			return nil
		},
		OnApply: func(ctx context.Context, c *Config) error {
			// 应用新配置（如重建下游 client）；失败会内存 + 磁盘双回滚。
			return nil
		},
	})

	app.RegisterCapability("whoami", func(ctx ksapp.CapabilityContext, args map[string]any) (any, error) {
		// 业务里读当前配置快照；未配置时为 nil。
		c := cfg.Get()
		if c == nil {
			return map[string]any{"configured": false}, nil
		}
		return map[string]any{"configured": true, "region": c.Region}, nil
	})

	app.Run()
}
