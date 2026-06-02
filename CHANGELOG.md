# Changelog

本仓库遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 风格，
版本号遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/spec/v2.0.0.html)。

## [v0.15.0] - 2026-05-31

manifest 声明层 clean-break 落到开发者侧（S3）：升 ks-types v0.30.0、重写脚手架为 correct-by-default 四类型模板、从 ks-types 注释生成 JSON Schema 喂 IDE、补本地能力网格联调命令、新增决策指南与最佳实践文档。

### Added

- **8 个 clean-break 模板**：`ks init --type=app|squad --lang=go|python|ts` + `--type=agent|skill`（langless）。app/squad 为外部 MCP service，演示 capability mesh（`RegisterCapability` / `@app.capability` / `registerCapability` + 复用四象限）；agent/skill 运行在 keystone 内（`runtime.mode: none`）。生成的 manifest 用裸名 `provides`、`auth.mode: keystone_jwks`、内联 `input_schema`/`output_schema`、`side_effect_level`，过 `ks test` 静态检查。
- **`ks schema`**：打印 / `--write` 写出内嵌 manifest JSON Schema（由 ks-types 字段注释生成，单一真值源 + 反漂移测试守门）。`ks init` 自动写 `.ks/manifest.schema.json` + `.vscode/settings.json` + manifest 顶部 modeline，VS Code 装 Red Hat YAML 扩展即得校验/补全。
- **`ks register` / `ks refresh-meta`**：读本地 manifest，以 `external_endpoint` 模式注册进本地 keystone-dev 做能力网格联调（app 容器自己跑）；`refresh-meta` 幂等重同步（先卸载再重装），免 `docker compose down -v`。
- **`ks doctor`** 增 Node + bun 检查（TS 模板前置）。
- 新增 `docs/decision-guide.md`（app/squad/agent/skill 怎么选 + tool vs capability + 复用四象限）与 `docs/best-practices.md`（六条元原则 + 诚实标 enforcement 状态）。

### Changed

- CLI 升级 ks-types `v0.27.0` → `v0.30.0`，自身 manifest 解析/校验切到 clean-break 契约。
- 模板 SDK 依赖 pin：Go `sdk/go v0.13.0`、Python `ks-app-sdk==0.9.0`、TS `@wuhanyuhan/ks-app@0.5.0`。
- 重写 `docs/developer-guide.md` 到四类型 + clean-break manifest + register/refresh-meta loop + IDE 小节；更新 `cli-reference.md` / `template-guide.md` / `sdk-api-reference.md`。

### BREAKING

- 删除旧模板 `service-{go,python}` / `extension-{go,python}` / `assistant` / `skill`，改为 `app/squad × {go,python,ts}` + `agent` / `skill`。`ks init --type` 枚举改为 `app|squad|agent|skill`（默认 `app`），不再接受 `service`/`extension`/`assistant`。
- 生成 manifest 移除已砍字段（`canonical_name` 前缀 / `cost_hint` / `typical_latency_ms` / `intent_summary` / `input_nl` / `output_nl` / `runtime.port` / `mount.service.*` / `install.yaml` 等）。
- capability mesh：mcp_tool capability 声明却既无 handler 也无同名 tool → 启动期报错（orphan→error）。

### Notes

- `ks register` / `ks refresh-meta` 的能力网格注册需本地 keystone-dev 为 clean-break 镜像；当前 `ks dev` 默认镜像（`keystone-dev:0.3.0`）若为 pre-clean-break，注册语义尚未在本地生效（见 docker-compose 内 TODO，`TestRegister_E2E` 默认 skip）。

## [v0.14.0] - 2026-05-29

### Added

- `ks init` / `ks publish` 支持 manifest **Store 展示字段**与 **assistant 助手模板**：新增 `internal/manifest/store_quality` 字段质量校验；`ks init` 增加 assistant 模板，service / extension 模板补齐 Store 展示字段（summary / description / category / tags 等）。
- `ks init` 生成项目的 SDK 依赖版本更新至最新：Go `sdk/go v0.11.2`、Python `ks-app-sdk>=0.7.0`（原模板停在 v0.4.0 / v0.3.0 / >=0.3.0）。

### Fixed

- `ks app list` / `ks publisher list` / `ks app status`：按服务端 `{items}` 信封格式解析 list 响应，修复列表与状态展示。
- `ks publish --dry-run`：校验 Store 展示字段、JSON 输出 manifest、降低高熵字符串（疑似密钥）误报、保留能力画像声明字段。

> **CHANGELOG 欠账说明**：本文件自 v0.6.0 起未逐版记录，v0.6.1–v0.13.0 区间的变更以 git history 与各 `sdk/*/CHANGELOG.md` 为准。本条目聚焦 v0.13.0 → v0.14.0 的 `ks` CLI（root module）实际变更。

## [v0.6.0] - 2026-05-05

### Added

- `ks publish` 引入 manifest fallback chain：缺字段时引导式补齐，不再直接 reject
  - 层 1：调 ks-hub `/v1/developer/devkit/manifest/suggest` 拿 LLM 建议
    （summary / description / category / tags），作者三选 `[a]采纳` / `[e]编辑` / `[s]跳过`
  - 层 2：仓库根有 CHANGELOG.md 时尝试本地正则（Keep-a-Changelog `## [x.y.z]` 格式）
    抽取目标版本 section；本地未命中调 `/v1/developer/devkit/changelog/parse` 兜底
  - 层 3：inline editor 交互式补字段（纯 `io.Reader/Writer` 接口，不依赖 terminal 检测）
- `internal/manifest` 新建子包，含 `ComputeMissingFields` / `RunFallbackChain` /
  `WriteManifestYAML` 等导出 API
- `internal/hub.Client` 新增 `SuggestManifest` / `ParseChangelog` 端点客户端
  + 完整 httptest mock 测试覆盖

### Changed

- 升级 `github.com/wuhanyuhan/ks-types` 到 v0.9.0
  - `Summary` / `Description` 升 `LocalizedString`，`Tags` 升 `LocalizedTags`
  - 新增 `Changelog` 字段
  - `Validate()` 不再校验 summary / description / category / tags 等可缺字段
- 写回 manifest.yaml 时单 `zh-CN` locale 形态压缩为单 string / list 形态，
  避免 git diff 出现 i18n map churn
- `internal/cmd/publish.go` runPublish 在 build → cfg+client 之后插入 fallback chain；
  JSON 模式（`--json`）天然跳过；dry-run（`--dry-run`）已在前面 return 不触发

### Behavior

- ks-skills 当前仓库无 CHANGELOG.md，层 2 在该场景下静默 miss → 走层 3
  inline editor。CLI 文案不暗示"chain 失败"。
- LLM 端点不可用（5xx / 业务码 / 网络错）时静默降级到层 3 inline editor，
  不重试、不报错。
- CI / 自动化场景请用 `ks publish --json` 跳过引导（NDJSON 模式不与 stdin prompt 混用）。

### Deviations from plan

- plan task 5.1 让加 `github.com/AlecAivazis/survey/v2` 依赖；实施时 `go mod tidy`
  自动清掉了未被 import 的 survey 包。inline editor 走纯 `io.Reader/Writer` 接口
  完全够用，与"不强依赖 survey terminal 检测"的偏好一致。后续如要 TUI 增强
  （如 category 多选），再单独引入。
