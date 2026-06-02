module github.com/wuhanyuhan/ks-devkit/sdk/go/conformance-claimant

go 1.26.2

replace github.com/wuhanyuhan/ks-devkit/sdk/go => ../

require github.com/wuhanyuhan/ks-devkit/sdk/go v0.0.0-00010101000000-000000000000

require (
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/gorilla/websocket v1.5.1 // indirect
	github.com/wuhanyuhan/ks-types v0.31.0 // indirect
	golang.org/x/net v0.51.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
