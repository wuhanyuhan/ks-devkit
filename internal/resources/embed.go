// Package resources 提供嵌入的模板和运行时文件。
package resources

import "embed"

//go:embed all:templates
var TemplatesFS embed.FS

//go:embed all:runtime
var RuntimeFS embed.FS

//go:embed schema/manifest.schema.json
var SchemaFS embed.FS
