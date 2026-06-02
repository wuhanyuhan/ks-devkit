package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalExtractChangelog_KeepAChangelogFormat(t *testing.T) {
	md := `# Changelog

## [0.3.0] - 2026-05-02
### Added
- examples 抽取

## [0.2.0] - 2026-04-30
- early
`
	got, ok := LocalExtractChangelogSection(md, "0.3.0")
	if !ok {
		t.Fatal("expected found")
	}
	if !strings.Contains(got, "examples 抽取") {
		t.Errorf("section: %q", got)
	}
	if strings.Contains(got, "early") {
		t.Errorf("should not include 0.2.0 content")
	}
}

func TestLocalExtractChangelog_LastVersion(t *testing.T) {
	md := "## [0.1.0]\n- init only\n"
	got, ok := LocalExtractChangelogSection(md, "0.1.0")
	if !ok {
		t.Fatalf("expected found, got %q", got)
	}
	if !strings.Contains(got, "init only") {
		t.Errorf("got %q", got)
	}
}

func TestLocalExtractChangelog_NotFound(t *testing.T) {
	md := "## [0.1.0]\n- x"
	if _, ok := LocalExtractChangelogSection(md, "9.9.9"); ok {
		t.Error("should not find")
	}
}

func TestLocalExtractChangelog_VersionWithoutDate(t *testing.T) {
	md := "## [0.3.0]\n- changes\n## [0.2.0]\n- old"
	got, ok := LocalExtractChangelogSection(md, "0.3.0")
	if !ok || !strings.Contains(got, "changes") {
		t.Errorf("got %q", got)
	}
	if strings.Contains(got, "old") {
		t.Errorf("should not include 0.2.0 content: %q", got)
	}
}

func TestLocalExtractChangelog_VersionWithPrerelease(t *testing.T) {
	md := "## [0.3.0-beta.1]\n- pre\n## [0.2.0]\n- old"
	got, ok := LocalExtractChangelogSection(md, "0.3.0-beta.1")
	if !ok || !strings.Contains(got, "pre") {
		t.Errorf("prerelease should match: got %q ok=%v", got, ok)
	}
}

func TestLocalExtractChangelog_TrimsWhitespace(t *testing.T) {
	md := "## [0.3.0]\n\n  changes  \n\n## [0.2.0]\n- old"
	got, _ := LocalExtractChangelogSection(md, "0.3.0")
	if strings.HasPrefix(got, "\n") || strings.HasSuffix(got, "\n") {
		t.Errorf("section should be trimmed: %q", got)
	}
}

func TestFindChangelogPath_Found(t *testing.T) {
	tmpDir := t.TempDir()
	expected := filepath.Join(tmpDir, "CHANGELOG.md")
	if err := os.WriteFile(expected, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	got, ok := FindChangelogPath(tmpDir)
	if !ok {
		t.Fatal("expected found")
	}
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestFindChangelogPath_LowercaseFallback(t *testing.T) {
	tmpDir := t.TempDir()
	expected := filepath.Join(tmpDir, "changelog.md")
	_ = os.WriteFile(expected, []byte("x"), 0644)
	got, ok := FindChangelogPath(tmpDir)
	if !ok {
		t.Fatal("expected found")
	}
	// macOS / Windows 大小写不敏感文件系统会让 CHANGELOG.md 也命中 changelog.md，
	// 所以只需断言找到了一个存在的文件即可
	if _, err := os.Stat(got); err != nil {
		t.Errorf("returned path should exist: %q err=%v", got, err)
	}
}

func TestFindChangelogPath_NotExist(t *testing.T) {
	if _, ok := FindChangelogPath(t.TempDir()); ok {
		t.Error("should not find in empty dir")
	}
}
