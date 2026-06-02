# ks-devkit 文档索引

Keystone 生态开发者文档。按"先理解 → 动手做 → 查 API → 运维"组织。

## 概念（先理解）

- [architecture.md](architecture.md) — 架构总览：调用链 / manifest 生命周期 / canonical 派生 / 复用四象限
- [decision-guide.md](decision-guide.md) — 怎么选四类型（app/squad/agent/skill）、tool vs capability
- [auth-and-security.md](auth-and-security.md) — 鉴权与安全模型：两层鉴权、scoped JWT、enforcement 诚实状态

## 上手（动手做）

- [quickstart.md](quickstart.md) — 5 分钟跑通第一个 app（已端到端实测）
- [developer-guide.md](developer-guide.md) — 完整开发指南
- [best-practices.md](best-practices.md) — 最佳实践 / 别踩的坑
- [config-schema.md](config-schema.md) — 类型化配置（API Key / OAuth 等管理员要填的值）
- [squad-tool-ui-quickstart.md](squad-tool-ui-quickstart.md) — squad / agent 工具返回结构化 UI widget

## 参考（查 API）

- [sdk-api-reference.md](sdk-api-reference.md) — 三语言 SDK 权威 API（含能力 × 语言矩阵）
- [cli-reference.md](cli-reference.md) — `ks` CLI 命令参考
- [keystore-and-crypto.md](keystore-and-crypto.md) — keystore / crypto 底层 API（Go + Python，TS 无）
- [template-guide.md](template-guide.md) — 项目模板机制 / 如何加模板

## 运维 / 迁移

- [compatibility.md](compatibility.md) — 版本兼容矩阵
- [migration-checklist.md](migration-checklist.md) — 旧 manifest → clean-break 迁移
- [troubleshooting.md](troubleshooting.md) — 故障排查（症状 → 根因 → 修复）
- [homebrew-tap-setup.md](homebrew-tap-setup.md) — Homebrew tap 配置（发布维护）

## 示例代码

- [capability-writer-demo（Go）](../sdk/go/examples/capability-writer-demo) — capability mesh：`mcp_tool` + `http_endpoint` 两条 backend
- [config-demo（Go）](../sdk/go/examples/config-demo) — config-schema：`NewConfigOn` + OnValidate / OnApply
- [capability_writer_demo（Python）](../sdk/python/examples/capability_writer_demo) — capability mesh（Python 版）

## 贡献

- [../CONTRIBUTING.md](../CONTRIBUTING.md) — 构建 / 测试 / conformance / 加模板 / PR 约定
