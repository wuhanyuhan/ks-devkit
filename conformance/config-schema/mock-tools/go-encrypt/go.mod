module github.com/wuhanyuhan/ks-devkit/conformance/config-schema/mock-tools/go-encrypt

go 1.26.1

replace github.com/wuhanyuhan/ks-devkit/sdk/go => ../../../../sdk/go

require (
	github.com/wuhanyuhan/ks-devkit/sdk/go v0.0.0-00010101000000-000000000000
	github.com/wuhanyuhan/ks-types v0.31.0
)

require (
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
