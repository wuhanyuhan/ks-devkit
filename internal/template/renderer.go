// Package template 提供项目模板渲染能力。
package template

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// RenderFromFS 从 fs.FS 中读取模板并渲染到 outputDir。
// fsys 可以是 embed.FS 或 os.DirFS。
// templateDir 是 fsys 内的子目录路径（如 "templates/service-go"）。
func RenderFromFS(fsys fs.FS, templateDir, outputDir string, data map[string]string) error {
	return fs.WalkDir(fsys, templateDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".tmpl") {
			return nil
		}

		rel, _ := filepath.Rel(templateDir, path)
		outPath := filepath.Join(outputDir, strings.TrimSuffix(rel, ".tmpl"))

		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return err
		}

		content, err := fs.ReadFile(fsys, path)
		if err != nil {
			return fmt.Errorf("read template %s: %w", rel, err)
		}

		tmpl, err := template.New(filepath.Base(path)).
			Option("missingkey=error").
			Parse(string(content))
		if err != nil {
			return fmt.Errorf("parse template %s: %w", rel, err)
		}

		f, err := os.Create(outPath)
		if err != nil {
			return err
		}
		defer f.Close()

		return tmpl.Execute(f, data)
	})
}

// Render 从磁盘目录渲染模板（兼容旧调用方）。
func Render(templateDir, outputDir string, data map[string]string) error {
	absDir, err := filepath.Abs(templateDir)
	if err != nil {
		return err
	}
	parent := filepath.Dir(absDir)
	base := filepath.Base(absDir)
	return RenderFromFS(os.DirFS(parent), base, outputDir, data)
}
