# Manifest clean-break 迁移指南

把一个旧 manifest 迁移到 clean-break 声明形态：四类型（`app`/`agent`/`skill`/`squad`）、`provides` 去前缀、砍废字段、`auth` 上提到顶层、`managed_resources` 瘦身、`install.yaml` 配置职责退役。

> **术语：clean-break** = 一次不向后兼容的 manifest 契约重构——类型词汇、能力命名、配置机制一次性换新，不保留旧字段的兼容层。

大部分机械变换由 `ks migrate` 自动完成；少数需人工判断的（依赖映射、配置归位、SDK 升级连带）在下面列清。

## 单仓标准流程

1. **bump 依赖到已发版契约**
   - Go：`go get github.com/wuhanyuhan/ks-types@latest && go get github.com/wuhanyuhan/ks-devkit/sdk/go@latest`；若仓库 vendored 再 `go mod vendor`
   - Python：`ks-app-sdk` 升到对应版本
   - TypeScript：`@wuhanyuhan/ks-app` 升到对应版本
2. **先跑红**：`go build ./...` / 既有测试，确认旧 manifest 在新契约下解析失败、或代码引用了已删 SDK 符号
3. **迁移 manifest**：`ks migrate manifest.yaml --write`
   - **squad 仓必加 `--type=squad`**（`store.team` 自动判定不可靠，需显式声明）
   - 默认 dry-run 打印到 stdout（用 `git diff` 审阅）；`--write` 写回
4. **处理 `ks migrate` 的告警**（打到 stderr，见下「需人工处理」）
5. **修 SDK 升级连带**（非 manifest 面，但同 commit 修掉，见下「SDK 升级连带」）
6. **验证绿**：`go build ./...` + 既有测试 + `ks test --skip-runtime`（manifest 解析/格式/权限静态检查）
7. **commit**

## `ks migrate` 自动做的变换

| 变换 | 说明 |
|------|------|
| `type` 改名 | `service`/`extension`→`app`、`assistant`→`agent`、`skill` 不变 |
| `provides` 去前缀 | `canonical_name: <id>.foo` → `name: foo`（作者只写裸名，全名由 keystone 注册期派生） |
| 合并 `description` | `intent_summary` + `natural_description` → 一个 `description` |
| 砍 per-capability 废字段 | `cost_hint`/`typical_latency_ms`/`input_nl`/`output_nl`/`requires_approval`/`allowed_callers`/`default_grant`/`compose_with`/`*_schema_ref` |
| `auth` 上提 | `mount.*.auth_mode` → 顶层 `auth.mode` |
| 删 `mount.service`/`mount.extension` | `app`/`squad` 走 `provides`，`mount` 只保留 `agent`/`skill`（`auto_register_mcp`/`mcp_endpoint`/`llm_mode`/`config_ui` 已退役） |
| `mount.assistant`→`mount.agent` | agent 类挂载改名 + 清 profile 旧字段 |
| `managed_resources` 瘦身 | 砍 `required`（块存在即=需要）、值为 `auto` 的分配键、`cache.provider` |
| 旧 `dependencies` 退役 | `conflicts`→顶层 `conflicts.apps`（丢 version）；`requires` 留告警（见下） |
| 砍顶层废段 | `license`/`a2a`/`routing_plan`/`protection`/`store.media`/`runtime.port` |

转换基于 `yaml.Node`，**保留作者注释与字段顺序**；幂等（迁移结果再迁不变）。

## 需人工处理（`ks migrate` 只告警，不自动改）

### 1. 旧 `dependencies.requires`

旧的 app 级 `requires: [{id: other-app}]` 无法自动映射到 capability 级。逐条判断：

- 依赖的是对方某个**静态声明的 capability** → 改写成 `requires.capabilities[].canonical_name`（全名）
- 依赖的是对方**运行时动态暴露的 MCP 工具**（对方无静态 `provides`）→ 不是声明级依赖，删除，运行时按需调

> 能力契约「名字即身份」，`requires` 永不带 version。

### 2. `install.yaml` 配置职责退役 → 按性质归位

`install.yaml` 的 `config_fields`/`secret_fields` 是旧的「管理员配 app」机制，与 config-schema 重叠（一个职责一个机制）。**不是无脑全塞进 config-schema**——按字段真实性质归位：

| 字段性质 | 归宿 |
|----------|------|
| 管理员密钥/凭证（OAuth App Secret、SMTP/IMAP 密码、服务账号…） | **config-schema**（端到端加密下发、运行时可改、带连接测试） |
| 管理员须设的运维配置（自托管服务 URL、OAuth 回调基址…） | **config-schema** |
| 平台注入（托管资源/平台服务地址） | `managed_resources` / `platform_services` 的 `inject`（非 app 配置） |
| 跨 app 依赖地址 | `requires`（能力网格）/ 平台路由 |
| 纯内部调参（有合理默认、管理员通常不碰，如分块大小、超时） | **app 代码 env-default**（app 私有实现细节，不进平台配置机制） |

config-schema 用法：定义带 `ksconfig:"..."` tag 的配置结构体，用 `ksapp.NewConfigOn[T]` 注册（Go 参照 SDK 内 `ConfigSpec` + `OnValidate`/`OnApply`；app 从 config handle 读，不再读 env）。配置职责清空后删除 `install.yaml`。完整用法见 [config-schema.md](config-schema.md)。

### 3. SDK 升级连带（非 manifest 面，同 commit 修）

clean-break 同时收敛了 SDK 契约，bump 后可能撞到：

- `ksapp.CapabilityContext` 接口对齐：`AppID()` 改名 `CallerID()`、新增 `ChainHeader()`。更新 handler 调用点与单测里的 `CapabilityContext` stub/fake。
- manifest smoke 测试里硬编码 `type == "service"` 的断言 → 改 `"app"`；断言 `mount.service.auto_register_mcp` 的 → 删（`mount.service` 已退役，app 无 `provides` 即由平台动态发现 MCP 工具）。

## 验证门

- `go build ./...`（或对应语言的构建/类型检查）通过新 SDK 编译
- 既有测试通过
- `ks test --skip-runtime`：`manifest.yaml` 解析 + 格式 + 权限声明三项静态检查绿（「高风险权限」是 advisory，按 app 实际权限诚实标注，非迁移回归）
