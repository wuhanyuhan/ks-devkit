module wire-compat-helper

go 1.26.1

require (
	github.com/wuhanyuhan/ks-devkit/sdk/go v0.0.0-00010101000000-000000000000
	github.com/wuhanyuhan/ks-types v0.43.0
)

require (
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/gorilla/websocket v1.5.1 // indirect
	golang.org/x/net v0.51.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// 本 helper 在 monorepo 内 replace 到相邻 sdk/go 源码 + 同机 ks-types 仓。
// 跨 module 引用：sdk/go 路径下的代码（含其 transitive 引用 ks-devkit 仓根
// internal 包）必须用本地 replace，否则 go run 拒绝拉远程伪版本。
replace (
	github.com/wuhanyuhan/ks-devkit => ../../../../..
	github.com/wuhanyuhan/ks-devkit/conformance => ../../../../../conformance
	github.com/wuhanyuhan/ks-devkit/sdk/go => ../../../../go
	github.com/wuhanyuhan/ks-types => ../../../../../../ks-types
)
