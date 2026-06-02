# AGENTS.md

面向本仓库维护者的简要约定。这里不记录内部路线图、私有仓路径或未公开的跨仓计划；公开开发者契约以 `README.md`、`docs/`、`sdk/*/CHANGELOG.md` 和代码测试为准。

## 项目边界

`ks-devkit` 是 Keystone 生态的公开开发者工具链，包含：

| 目录 | 说明 |
|------|------|
| `cmd/ks/`、`internal/` | `ks` 命令行（`cli/` 是构建产物目录） |
| `sdk/go/` | Go SDK，包名 `ksapp` |
| `sdk/python/` | Python SDK，包名 `ks_app` |
| `sdk/typescript/` | TypeScript SDK |
| `conformance/` | 协议一致性测试套件 |
| `internal/resources/runtime/` | 本地开发 runtime 资源 |
| `internal/resources/templates/` | `ks init` 项目模板 |
| `docs/` | 公开开发者文档 |

## 常用验证

```bash
bash conformance/docs-lint/run.sh   # 公开文档防漂移（非法类型词汇 / 死链 / 签名 / 相对链接）
go test ./...            # CLI（仓库根模块 github.com/wuhanyuhan/ks-devkit）
cd sdk/go && go test ./...
cd sdk/python && pytest
```

前端或 TypeScript SDK 改动按对应 package 的 `package.json` 脚本执行测试和构建。

## 代码约定

- Go 代码使用 `gofmt`，错误包装优先 `fmt.Errorf("...: %w", err)`。
- Python 代码保持类型清晰，测试使用 `pytest`。
- 对外协议、SDK API、CLI 参数变更必须同步更新公开文档和 changelog。
- 不在公开仓记录私有部署细节、内部项目路径、密钥、Token、未公开路线图或跨仓执行计划。
- 测试 fixture 中如需私钥，只能使用明确标注为测试用途的低权限样例密钥。
