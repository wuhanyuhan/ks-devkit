package template

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestRender(t *testing.T) {
	tmplDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmplDir, "main.go.tmpl"),
		[]byte("package main\n// App: {{.Name}}\n"), 0644)

	outDir := t.TempDir()
	err := Render(tmplDir, outDir, map[string]string{
		"Name": "my-app",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	content, _ := os.ReadFile(filepath.Join(outDir, "main.go"))
	if string(content) != "package main\n// App: my-app\n" {
		t.Errorf("content: %q", string(content))
	}
}

func TestRender_MissingKey(t *testing.T) {
	tmplDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmplDir, "foo.txt.tmpl"),
		[]byte("hello {{.Undefined}}\n"), 0644)

	outDir := t.TempDir()
	err := Render(tmplDir, outDir, map[string]string{
		"Name": "world",
	})
	if err == nil {
		t.Fatal("expected error for missing key, got nil")
	}
}

func TestRender_NestedPath(t *testing.T) {
	tmplDir := t.TempDir()
	nested := filepath.Join(tmplDir, "cmd", "app")
	_ = os.MkdirAll(nested, 0755)
	_ = os.WriteFile(filepath.Join(nested, "main.go.tmpl"),
		[]byte("// {{.AppID}}\n"), 0644)

	outDir := t.TempDir()
	if err := Render(tmplDir, outDir, map[string]string{"AppID": "foo"}); err != nil {
		t.Fatalf("render: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(outDir, "cmd", "app", "main.go"))
	if string(got) != "// foo\n" {
		t.Errorf("nested content: %q", string(got))
	}
}

func TestRenderFromFS_EmbedFS(t *testing.T) {
	mockFS := fstest.MapFS{
		"tpl/hello.txt.tmpl": &fstest.MapFile{Data: []byte("Hi {{.Name}}!")},
	}

	outDir := t.TempDir()
	err := RenderFromFS(mockFS, "tpl", outDir, map[string]string{"Name": "Alice"})
	if err != nil {
		t.Fatalf("renderFromFS: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(outDir, "hello.txt"))
	if string(got) != "Hi Alice!" {
		t.Errorf("content: %q", string(got))
	}
}
