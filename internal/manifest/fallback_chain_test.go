package manifest

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	kstypes "github.com/wuhanyuhan/ks-types"

	"github.com/wuhanyuhan/ks-devkit/internal/hub"
)

func newSpecFull() kstypes.AppSpec {
	return kstypes.AppSpec{
		ID:          "skill-tdd",
		Name:        "skill-tdd",
		Version:     "0.1.0",
		Type:        "skill",
		Summary:     kstypes.LocalizedString{"": "TDD"},
		Description: kstypes.LocalizedString{"": "long desc"},
		Category:    "开发流程",
		Tags:        kstypes.LocalizedTags{"": []string{"tdd"}},
		Store: kstypes.StoreSpec{
			Audience:   []string{"开发者"},
			Highlights: []string{"提供 TDD 开发流程"},
			TryPrompts: []string{"帮我用 TDD 开发这个功能"},
		},
		Changelog: "### Added\n- init",
	}
}

func TestRunFallbackChain_NoMissing_NoOp(t *testing.T) {
	spec := newSpecFull()
	got, err := RunFallbackChain(context.Background(), nil, strings.NewReader(""), &bytes.Buffer{}, FallbackInputs{Spec: spec})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, spec) {
		t.Errorf("expected unchanged spec, got %+v", got)
	}
}

func TestRunFallbackChain_LLMSuggestsThenInlineChangelog(t *testing.T) {
	// 缺 summary+tags+changelog；LLM 处理 summary/tags（[a]采纳），changelog 走 inline editor
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"suggestions":{"summary":"AI 摘要","tags":["a","b"]},"confidence":0.8}}`))
	}))
	defer mock.Close()
	c := hub.NewClient(mock.URL, "x")

	spec := kstypes.AppSpec{
		ID: "skill-tdd", Name: "skill-tdd", Version: "0.1.0", Type: "skill",
		Description: kstypes.LocalizedString{"": "ok"},
		Category:    "开发流程",
	}

	tmpDir := t.TempDir() // 无 CHANGELOG.md
	in := strings.NewReader("a\n开发者\n提供 TDD 开发流程\n帮我用 TDD 开发这个功能\n### Added\n- 新内容\n::end\n")

	got, err := RunFallbackChain(context.Background(), c, in, &bytes.Buffer{}, FallbackInputs{
		Spec:        spec,
		SkillMdText: "# skill-tdd",
		RepoDir:     tmpDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary.Get("") != "AI 摘要" {
		t.Errorf("Summary = %q", got.Summary.Get(""))
	}
	if !reflect.DeepEqual(got.Tags.Get(""), []string{"a", "b"}) {
		t.Errorf("Tags = %v", got.Tags.Get(""))
	}
	if !strings.Contains(got.Changelog, "新内容") {
		t.Errorf("Changelog = %q", got.Changelog)
	}
	if !reflect.DeepEqual(got.Store.Audience, []string{"开发者"}) {
		t.Errorf("Store.Audience = %v", got.Store.Audience)
	}
}

func TestRunFallbackChain_ChangelogFromLocalCHANGELOG(t *testing.T) {
	// 仅 changelog 缺；LLM 不该被调（hub == nil 也不 panic 因层 1 跳过）；本地 CHANGELOG.md 命中
	tmpDir := t.TempDir()
	cl := "## [0.3.0]\n- 新功能\n## [0.2.0]\n- old\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "CHANGELOG.md"), []byte(cl), 0644); err != nil {
		t.Fatal(err)
	}

	spec := kstypes.AppSpec{
		ID: "x", Name: "x", Version: "0.3.0", Type: "skill",
		Summary:     kstypes.LocalizedString{"": "ok"},
		Description: kstypes.LocalizedString{"": "ok"},
		Category:    "开发流程",
		Tags:        kstypes.LocalizedTags{"": []string{"a"}},
		Store: kstypes.StoreSpec{
			Audience:   []string{"开发者"},
			Highlights: []string{"提供 TDD 开发流程"},
			TryPrompts: []string{"帮我用 TDD 开发这个功能"},
		},
	}

	got, err := RunFallbackChain(context.Background(), nil, strings.NewReader(""), &bytes.Buffer{}, FallbackInputs{
		Spec:    spec,
		RepoDir: tmpDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Changelog, "新功能") {
		t.Errorf("Changelog = %q", got.Changelog)
	}
	if strings.Contains(got.Changelog, "old") {
		t.Errorf("should not include 0.2.0 content: %q", got.Changelog)
	}
}

func TestRunFallbackChain_ChangelogHubFallback(t *testing.T) {
	// 本地 CHANGELOG.md 存在但本地正则没命中目标版本 → 调 hub.ParseChangelog 兜底成功
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/developer/devkit/changelog/parse" {
			_, _ = w.Write([]byte(`{"code":0,"data":{"parsed":{"found":true,"version_section":"hub 兜底抽到的内容"}}}`))
			return
		}
		t.Errorf("unexpected path: %s", r.URL.Path)
	}))
	defer mock.Close()
	c := hub.NewClient(mock.URL, "x")

	tmpDir := t.TempDir()
	// 用一个本地正则识别不出来的格式（无 [ ] 包围）
	_ = os.WriteFile(filepath.Join(tmpDir, "CHANGELOG.md"), []byte("## v0.3.0\n- 内容\n"), 0644)

	spec := kstypes.AppSpec{
		ID: "x", Name: "x", Version: "0.3.0", Type: "skill",
		Summary:     kstypes.LocalizedString{"": "ok"},
		Description: kstypes.LocalizedString{"": "ok"},
		Category:    "x",
		Tags:        kstypes.LocalizedTags{"": []string{"a"}},
		Store: kstypes.StoreSpec{
			Audience:   []string{"开发者"},
			Highlights: []string{"提供 TDD 开发流程"},
			TryPrompts: []string{"帮我用 TDD 开发这个功能"},
		},
	}

	got, err := RunFallbackChain(context.Background(), c, strings.NewReader(""), &bytes.Buffer{}, FallbackInputs{
		Spec:    spec,
		RepoDir: tmpDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Changelog, "hub 兜底") {
		t.Errorf("expected hub fallback content, got %q", got.Changelog)
	}
}

func TestRunFallbackChain_LLMUnavailable_NoChangelogMd_AllInline(t *testing.T) {
	// hub 503 + 无 CHANGELOG.md → 所有缺字段都走 inline editor
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer mock.Close()
	c := hub.NewClient(mock.URL, "x")

	spec := kstypes.AppSpec{ID: "x", Name: "x", Version: "0.1.0", Type: "skill"}

	// 顺序：summary / description (多行::end) / category / tags / store.audience / store.highlights / store.try_prompts / changelog
	in := strings.NewReader("我的摘要\n我的描述\n::end\n开发流程\nt1, t2\n开发者\n提供 TDD 开发流程\n帮我用 TDD 开发这个功能\n### Added\n- x\n::end\n")
	got, err := RunFallbackChain(context.Background(), c, in, &bytes.Buffer{}, FallbackInputs{
		Spec:    spec,
		RepoDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary.Get("zh-CN") != "我的摘要" {
		t.Errorf("Summary = %q", got.Summary.Get("zh-CN"))
	}
	if got.Description.Get("zh-CN") != "我的描述" {
		t.Errorf("Description = %q", got.Description.Get("zh-CN"))
	}
	if got.Category != "开发流程" {
		t.Errorf("Category = %q", got.Category)
	}
	if !reflect.DeepEqual(got.Tags.Get("zh-CN"), []string{"t1", "t2"}) {
		t.Errorf("Tags = %v", got.Tags.Get("zh-CN"))
	}
	if !reflect.DeepEqual(got.Store.Highlights, []string{"提供 TDD 开发流程"}) {
		t.Errorf("Store.Highlights = %v", got.Store.Highlights)
	}
	if !strings.Contains(got.Changelog, "Added") {
		t.Errorf("Changelog = %q", got.Changelog)
	}
}

func TestRunFallbackChain_AuthorSkipsLLM_StillInlineForChangelog(t *testing.T) {
	// LLM 给了建议但作者 [s] 跳过 → summary/tags 仍空 → 进 inline editor 补；changelog 也进 inline editor
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"suggestions":{"summary":"AI","tags":["x"]}}}`))
	}))
	defer mock.Close()
	c := hub.NewClient(mock.URL, "x")

	spec := kstypes.AppSpec{
		ID: "x", Name: "x", Version: "0.1.0", Type: "skill",
		Description: kstypes.LocalizedString{"": "ok"},
		Category:    "ok",
	}

	// 输入：[s]跳过 LLM → 进入 inline editor 补 summary, tags, store 字段, changelog
	in := strings.NewReader("s\n手填摘要\nh1, h2\n开发者\n亮点 1, 亮点 2\n试试这样问\n### Added\n- y\n::end\n")
	got, err := RunFallbackChain(context.Background(), c, in, &bytes.Buffer{}, FallbackInputs{
		Spec: spec, RepoDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary.Get("zh-CN") != "手填摘要" {
		t.Errorf("Summary = %q", got.Summary.Get("zh-CN"))
	}
	if !reflect.DeepEqual(got.Tags.Get("zh-CN"), []string{"h1", "h2"}) {
		t.Errorf("Tags = %v", got.Tags.Get("zh-CN"))
	}
	if !reflect.DeepEqual(got.Store.Highlights, []string{"亮点 1", "亮点 2"}) {
		t.Errorf("Store.Highlights = %v", got.Store.Highlights)
	}
}

func TestFilterStrings(t *testing.T) {
	got := filterStrings([]string{"a", "b", "c", "d"}, []string{"a", "c", "z"})
	want := []string{"a", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
