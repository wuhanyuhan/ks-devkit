# 版本兼容矩阵

下面是**当前互相兼容的一套版本**（同期 clean-break 发布，2026-05-31）。

| 组件 | 版本 | 来源 |
|---|---|---|
| `ks` CLI | v0.15.0 | [CHANGELOG.md](../CHANGELOG.md) |
| Go SDK（`ksapp`） | v0.13.0 | [sdk/go/CHANGELOG.md](../sdk/go/CHANGELOG.md) |
| Python SDK（`ks_app`） | v0.9.0 | [sdk/python/CHANGELOG.md](../sdk/python/CHANGELOG.md) |
| TypeScript SDK（`@wuhanyuhan/ks-app`） | v0.5.0 | [sdk/typescript/CHANGELOG.md](../sdk/typescript/CHANGELOG.md) |
| keystone 生产平台 | `>=1.0.0` | 模板 `compatibility.keystone` |
| keystone-dev 本地镜像 | `0.3.0` | `internal/resources/runtime/docker-compose.yaml` |

> 三语言 SDK 版本号各自独立递增（不强制同号），同期发布的这套保证 wire-compat。升级时取各 CHANGELOG 最新、且发布日期相近的一组。

## 两条 keystone 轨道：`>=1.0.0` vs `0.3.0`

模板生成的 manifest 写 `compatibility.keystone: ">=1.0.0"`，但 `ks dev` 自带镜像是 `keystone-dev:0.3.0`——这**不是矛盾**，是两条独立的轨道：

| | `compatibility.keystone: ">=1.0.0"` | `keystone-dev:0.3.0` |
|---|---|---|
| 指什么 | 你的 app 上架要求的**生产 keystone 平台**版本 | `ks dev` 起的**本地联调镜像** |
| 作用 | 平台 install 时做版本门禁 | 本地调试 |
| 现状 | clean-break 生产平台 | pre-clean-break，能力网格本地注册不生效 |

后果：模板默认产物（capability mesh）在自带本地环境里，**能力网格注册暂不生效**——本地用裸 MCP tool 路径调试即可（见 [quickstart.md](quickstart.md)）。详见 [developer-guide.md](developer-guide.md) 第 6 节告警与 [troubleshooting.md](troubleshooting.md)。待 keystone 侧发布 clean-break 版 keystone-dev 镜像后，`docker-compose.yaml` 的 tag 会一并 bump，本地能力网格联调即打通。
