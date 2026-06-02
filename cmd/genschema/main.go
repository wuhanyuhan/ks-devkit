// Command genschema 由 go:generate 调用，把 ks-types 结构体生成 JSON Schema 写入 -o。
// dev-time 专用（依赖 golang.org/x/tools），产物 committed + embed 进 ks 二进制，
// 故运行时零 go/packages 依赖。
package main

import (
	"flag"
	"os"
	"path/filepath"

	"github.com/wuhanyuhan/ks-devkit/internal/schemagen"
)

func main() {
	out := flag.String("o", "internal/resources/schema/manifest.schema.json", "输出路径")
	flag.Parse()
	data, err := schemagen.Generate("github.com/wuhanyuhan/ks-types", "AppSpec")
	if err != nil {
		panic(err)
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0644); err != nil {
		panic(err)
	}
}
