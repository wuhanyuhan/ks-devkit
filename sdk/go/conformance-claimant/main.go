// Package main 是 ksapp Go SDK 的 conformance claimant。
//
// 它声称遵守 ks-devkit/conformance/auth/ v1.0.0 契约。
// 除 echo 工具外不做任何业务，行为被 conformance 测试冻结。
//
// 不要修改 echo 的名字、schema 或返回值——否则 conformance case 16/17 会失败。
package main

import (
	"context"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp"
)

func main() {
	ksapp.New("conformance-claimant",
		ksapp.WithKeystoneAuth(),
		ksapp.WithVersion("conformance-v1.0.0")).
		ToolWithSchema("echo", "Echo message as-is (conformance test tool)",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message": map[string]any{"type": "string"},
				},
			},
			func(ctx context.Context, args map[string]any) (any, error) {
				return map[string]any{"echoed": args["message"]}, nil
			}).
		Run()
}
