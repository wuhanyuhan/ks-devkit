// capability-writer-demo — ks-devkit/sdk/go/ksapp v0.8.0 capability mesh 示例。
//
// 演示两条 backend 路径：
//   - mcp_tool：list_articles 在 manifest 声明 backend.kind=mcp_tool，SDK 自动注册成同名 MCP tool
//   - http_endpoint：create_article 声明 backend.kind=http_endpoint，SDK 挂 ScopedJWT 保护的 HTTP route
//
// Usage:
//
//	cd ks-devkit/sdk/go/examples/capability-writer-demo
//	export KS_APP_TOKEN=fake-token        # 可选，仅 caller-side 才需要
//	export KS_GATEWAY_URL=http://localhost:8080
//	go run .
package main

import (
	"context"
	"net/http"

	"github.com/wuhanyuhan/ks-devkit/sdk/go/ksapp"
)

func main() {
	app := ksapp.New("writer-demo-go",
		ksapp.WithManifest("manifest.yaml"),
		ksapp.WithVersion("0.1.0"),
	)

	// mcp_tool backend：sync 查询
	app.RegisterCapability("list_articles",
		func(ctx ksapp.CapabilityContext, args map[string]any) (any, error) {
			page := 1
			if p, ok := args["page"].(float64); ok {
				page = int(p)
			}
			items := make([]map[string]any, 0, 5)
			for i := 0; i < 5; i++ {
				items = append(items, map[string]any{
					"id":    (page-1)*5 + i,
					"title": "示例文章",
					"owner": ctx.UserID(),
				})
			}
			return map[string]any{"page": page, "items": items}, nil
		},
	)

	// http_endpoint backend：long_running 创作，演示 progress 上报
	app.RegisterCapability("create_article",
		func(ctx ksapp.CapabilityContext, args map[string]any) (any, error) {
			topic, _ := args["topic"].(string)
			if topic == "" {
				topic = "AI"
			}
			pct := 10
			_ = ctx.Progress(context.Background(), "正在搜索热点...", &pct)
			pct = 50
			_ = ctx.Progress(context.Background(), "正在生成正文...", &pct)
			return map[string]any{
				"topic":    topic,
				"body":     "关于 " + topic + " 的示例文章",
				"owner":    ctx.UserID(),
				"caller":   ctx.CallerID(),
				"chain_id": ctx.ChainID(),
			}, nil
		},
	)

	if err := http.ListenAndServe(":8000", app.Mux()); err != nil {
		panic(err)
	}
}
