# ksapp Conformance Claimant

**Claims compliance with: conformance-v1.0.0**

## 这是什么

ksapp Go SDK（`ks-devkit/sdk/go/ksapp/`）对
`../../conformance/auth/` 定义的契约的最小声明实现。

## 本地运行

```bash
cd ks-devkit/conformance/auth
./orchestrate.sh \
    --claimant-cmd="cd ../../sdk/go/conformance-claimant && go run ." \
    --claimant-port=8080
```

## 契约守护的不变量

- service name = `conformance-claimant`
- version = `conformance-v1.0.0`
- auth_mode = `keystone_jwks`
- 只注册一个名为 `echo` 的 MCP 工具
- `echo` schema = `{"type":"object","properties":{"message":{"type":"string"}}}`
- `echo` 返回 `{"echoed": args["message"]}`

**不要修改这些**。如果契约升级到 v1.1.0+，先评估升级后本 claimant 是否
通过所有 case，再更新本 README 顶部的声明行。

## 和 templates/service-go 的区别

`templates/service-go/` 是开发者的起点模板，会跟着"开发体验"改进。
本目录是 conformance 测试的稳定被测对象，**行为不可变**。
