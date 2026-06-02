module github.com/wuhanyuhan/ks-devkit/sdk/go

go 1.26.1

require (
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/gorilla/websocket v1.5.1
	github.com/wuhanyuhan/ks-devkit/conformance v1.0.0
	github.com/wuhanyuhan/ks-types v0.31.0
	gopkg.in/yaml.v3 v3.0.1
)

require golang.org/x/net v0.51.0 // indirect

// 本地开发让 conformance 改动即时生效（replace 不会传递到下游消费者）。
replace github.com/wuhanyuhan/ks-devkit/conformance => ../../conformance
