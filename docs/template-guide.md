# 模板开发指南

## 8 个内置模板

`ks init` 按 `(type, lang)` 选模板。模板位于 `internal/resources/templates/`（内嵌进 `ks` 二进制），按 `{type}-{lang}` 或 `{type}`（langless）命名：

| 模板 | 类型 | 用途 | `ks init` 命令 |
|------|------|------|----------------|
| `app-go` / `app-python` / `app-ts` | app | 外部 MCP service，对外供给一批能力 | `ks init x --type=app --lang=go\|python\|ts` |
| `squad-go` / `squad-python` / `squad-ts` | squad | 有 LLM 负责人编排的专家团队（外部 service） | `ks init x --type=squad --lang=go\|python\|ts` |
| `agent` | agent | 平台内角色助手（runtime none，langless） | `ks init x --type=agent` |
| `skill` | skill | 可被语义召回的技能资源（挂载，langless） | `ks init x --type=skill` |

- **app/squad 是外部 service**：有 runtime/语言，出 `main.{go,py}` / `index.ts` + `go.mod`/`requirements.txt`/`package.json` + Dockerfile。manifest 与语言无关（app-go/py/ts 三份逐字一致）。
- **agent/skill 是 keystone 内实体**（runtime none、无独立进程）：langless，只出 `manifest.yaml` + `AGENT.md`/`SKILL.md`，忽略 `--lang`。
- 每个 app/squad 模板还写出 `.ks/manifest.schema.json` + `.vscode/settings.json`（IDE 接线）。

怎么选类型见 [decision-guide.md](decision-guide.md)。

## 模板变量

所有 `.tmpl` 文件使用 Go `text/template` 语法。可用变量：

| 变量 | 说明 | 示例 |
|------|------|------|
| {{.AppID}} | 应用 ID | my-app |
| {{.Name}} | 显示名称 | my-app |
| {{.Summary}} | 简介 | my-app 应用 |
| {{.Publisher}} | Publisher slug | my-team |
| {{.Type}} | 应用类型 | app |
| {{.Language}} | 编程语言 | go |

## 渲染规则

- 文件名以 `.tmpl` 结尾的会被渲染，输出时去掉 `.tmpl` 后缀
- 使用 `missingkey=error`：引用未定义变量会报错
- 目录结构原样保持

## 添加新模板

1. 在 `internal/resources/templates/` 下创建目录，命名 `{type}-{lang}` 或 `{type}`（langless）
2. 添加 `.tmpl` 文件；manifest 必须能过 `ks-types` 的 `ParseAppSpec` + `Validate`（见 init_test.go 的全 8 组合集成测试）
3. 运行 `ks init test --type=xxx [--lang=yyy]` 验证生成物可解析
