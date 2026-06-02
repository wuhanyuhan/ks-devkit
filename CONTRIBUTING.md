# 贡献指南

面向 ks-devkit 贡献者。维护者私有约定见 [AGENTS.md](AGENTS.md)。

## 开发环境

`ks doctor` 一键自检（Go / Python / Node / bun / Docker / 模板目录 / 登录凭证）。要求：

- Go 1.26+
- Python 3.11+（Python SDK）
- bun / Node（TypeScript SDK）
- Docker（本地联调 / 运行时探测）

## 构建与测试

| 包 | 命令 |
|---|---|
| `ks` CLI（仓库根模块） | `go build ./... && go test ./...` |
| Go SDK | `cd sdk/go && go build ./... && go test ./...` |
| Python SDK | `cd sdk/python && pytest` |
| TypeScript SDK | `cd sdk/typescript && bun install && bun run build && bun run test` |

> CLI 是仓库根模块 `github.com/wuhanyuhan/ks-devkit`，从根目录跑 `go test ./...`（不是 `cd cli`——`cli/` 仅是构建输出目录）。

## conformance 一致性套件

`conformance/` 是独立 Go module，锁定跨语言 wire-compat 与协议行为：

| 套件 | 跑法 |
|---|---|
| auth（scoped JWT / JWKS） | `conformance/auth/run.sh --target=<app> --jwks=<url> …`（集成，需 running target） |
| config-schema（信封加密 / tag 反射） | `conformance/config-schema/run.sh` |
| managed-resources / PAT | `cd conformance && go test ./...` |

改了对外协议 / 加密格式 / wire 字段，务必跑相应套件。

## 添加新模板

1. 在 `internal/resources/templates/` 下建目录，命名 `{type}-{lang}` 或 `{type}`（langless）。
2. 加 `.tmpl` 文件；manifest 必须能过 `ks-types` 的 `ParseAppSpec` + `Validate`（见 `init_test.go` 全 8 组合集成测试）。
3. 跑 `ks init test --type=xxx [--lang=yyy]` 验证生成物可解析。

详见 [docs/template-guide.md](docs/template-guide.md)。

## PR / commit 约定

- commit message 用**中文 + conventional prefix**（`docs:` / `fix(cli):` / `feat(sdk):` …）。
- **禁止任何 Claude `Co-Authored-By` trailer**（或任何形式的 AI 署名）。
- 一个 PR 聚焦一件事；commit 粒度尽量可单独 cherry-pick。

## 文档纪律

**对外协议、SDK API、CLI 参数、manifest schema 的任何变更，必须同步更新公开文档 + changelog。** 文档与代码漂移是 bug。

- **API 示例只存 `docs/sdk-api-reference.md`（三语言并列权威契约）；各 SDK README 只放该语言的安装 + 惯用上手，能力示例链到 reference，不复制。** 复制必然各自漂移——TS `searchText` 一度凭空多出的 `mode` 选项，就是从 README 漂出 reference 的活教训。
- 公开开发者契约以 `README.md`、`docs/`、`sdk/*/CHANGELOG.md` 和代码测试为准。
- 不在公开仓记录私有部署细节、内部路径、密钥 / Token、未公开路线图或跨仓执行计划。
- 测试 fixture 如需私钥，只用明确标注测试用途的低权限样例密钥。
