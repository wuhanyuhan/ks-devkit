package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	kstypes "github.com/wuhanyuhan/ks-types"

	"github.com/wuhanyuhan/ks-devkit/internal/hub"
)

func TestRunPublishFallback_JSONModeSkips(t *testing.T) {
	spec := &kstypes.AppSpec{ID: "x", Name: "x", Version: "0.1.0", Type: "skill"}
	changed, err := runPublishFallback(
		context.Background(), nil, strings.NewReader(""), &bytes.Buffer{},
		spec, t.TempDir(), filepath.Join(t.TempDir(), "manifest.yaml"), true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("JSON mode should skip fallback chain")
	}
}

func TestRunPublishFallback_NoMissingNoOp(t *testing.T) {
	tmpDir := t.TempDir()
	spec := &kstypes.AppSpec{
		ID: "x", Name: "x", Version: "0.1.0", Type: "skill",
		Summary:     kstypes.LocalizedString{"": "ok"},
		Description: kstypes.LocalizedString{"": "ok"},
		Category:    "x",
		Tags:        kstypes.LocalizedTags{"": []string{"a"}},
		Changelog:   "ok",
	}
	changed, err := runPublishFallback(
		context.Background(), nil, strings.NewReader(""), &bytes.Buffer{},
		spec, tmpDir, filepath.Join(tmpDir, "manifest.yaml"), false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("no missing fields → should not change")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "manifest.yaml")); err == nil {
		t.Error("should not write manifest.yaml when no change")
	}
}

func TestRunPublishFallback_LLMUnavailableInlineWritesBack(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer mock.Close()
	c := hub.NewClient(mock.URL, "x")

	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "manifest.yaml")
	spec := &kstypes.AppSpec{
		ID: "skill-tdd", Name: "skill-tdd", Version: "0.1.0", Type: "skill",
	}
	// 顺序：summary / description::end / category / tags / changelog::end
	in := strings.NewReader("我的摘要\n我的描述\n::end\n开发流程\nt1, t2\n### Added\n- x\n::end\n")

	changed, err := runPublishFallback(
		context.Background(), c, in, &bytes.Buffer{},
		spec, tmpDir, manifestPath, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("expected manifest changed")
	}
	if spec.Summary.Get("zh-CN") != "我的摘要" {
		t.Errorf("spec.Summary = %q", spec.Summary.Get("zh-CN"))
	}

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "summary: 我的摘要") {
		t.Errorf("manifest.yaml should contain summary in single-string form, got:\n%s", data)
	}
	if !strings.Contains(string(data), "tags:") || !strings.Contains(string(data), "- t1") {
		t.Errorf("manifest.yaml should contain tags list, got:\n%s", data)
	}
}

func TestRunPublishFallback_SkillMdReadIsOptional(t *testing.T) {
	// 仓库无 SKILL.md → 不报错；fallback 仍能跑（LLM 可能返较弱建议但不影响调用）
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"suggestions":{"summary":"AI"}}}`))
	}))
	defer mock.Close()
	c := hub.NewClient(mock.URL, "x")

	tmpDir := t.TempDir()
	spec := &kstypes.AppSpec{
		ID: "x", Name: "x", Version: "0.1.0", Type: "skill",
		Description: kstypes.LocalizedString{"": "ok"},
		Category:    "ok",
		Tags:        kstypes.LocalizedTags{"": []string{"a"}},
		Changelog:   "ok",
	}
	in := strings.NewReader("a\n") // [a] 采纳 LLM summary

	changed, err := runPublishFallback(
		context.Background(), c, in, &bytes.Buffer{},
		spec, tmpDir, filepath.Join(tmpDir, "manifest.yaml"), false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("expected manifest changed")
	}
	if spec.Summary.Get("zh-CN") != "AI" {
		t.Errorf("Summary = %q", spec.Summary.Get("zh-CN"))
	}
}
