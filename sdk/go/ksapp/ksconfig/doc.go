// Package ksconfig 提供 MCP 配置字段的 tag 解析、反射生成 JSON Schema / UI Schema、
// show_when DSL 编译等能力，是类型化配置的 Go SDK 配套实现。
//
// 主要 API：
//   - ParseTag(tagStr string) (TagSpec, error)       — 解析单条字段 tag
//   - ReflectConfigSchema[T any]() (JSONSchema, UISchema, error) — 反射生成整个 T 的 Schema
//   - CompileShowWhen(expr string, fieldName string) (...) — show_when DSL 编译
//
// tag 语法 / 端点契约 / AAD / show_when / 指纹规范源：docs/config-schema.md
package ksconfig
