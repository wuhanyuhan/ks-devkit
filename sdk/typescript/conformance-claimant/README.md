# ks-app-ts Conformance Claimant

Claims compliance with: **conformance-v1.0.0**

这是 `@wuhanyuhan/ks-app` SDK 的最小 conformance claimant，用于证明
SDK 遵守 `ks-devkit/conformance/auth/` v1.0.0 契约。

## 怎么跑

```bash
# 从 ks-devkit 根目录：
cd conformance/auth
./orchestrate.sh \
  --claimant-cmd="cd ../../sdk/typescript/conformance-claimant && bun run start" \
  --claimant-port=9971
```

全绿即合规。

## 不要做的事

- 修改 `echo` 工具的名字、schema 或返回值 —— conformance case 16/17 会失败
- 改动 `auth` 设定（必须是 `keystone_jwks` 以覆盖所有 JWT 相关 case）
- 改版本号 `conformance-v1.0.0`
