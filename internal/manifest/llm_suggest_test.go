package manifest

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kstypes "github.com/wuhanyuhan/ks-types"

	"github.com/wuhanyuhan/ks-devkit/internal/hub"
)

// hubMockServer 起一个返指定 JSON body 的 httptest server，handler 把请求路径写入 *gotPath（如非 nil）。
func hubMockServer(t *testing.T, body string, status int, gotPath *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotPath != nil {
			*gotPath = r.URL.Path
		}
		if status != 0 && status != 200 {
			w.WriteHeader(status)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
}

func TestSuggestAndPrompt_Adopt(t *testing.T) {
	srv := hubMockServer(t, `{"code":0,"data":{
		"suggestions":{"summary":"AI 摘要","tags":["a","b","c"]},
		"rationale":"based on README","confidence":0.85,
		"llm_model":"claude-haiku-4-5","prompt_version":"v1"
	}}`, 200, nil)
	defer srv.Close()
	c := hub.NewClient(srv.URL, "x")

	in := strings.NewReader("a\n")
	out := &bytes.Buffer{}
	result, err := SuggestAndPrompt(context.Background(), c, in, out,
		kstypes.AppSpec{ID: "skill-tdd", Type: "skill"},
		"# skill-tdd\nTDD helper.", []string{"summary", "tags"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "AI 摘要" {
		t.Errorf("Summary = %q", result.Summary)
	}
	if len(result.Tags) != 3 {
		t.Errorf("Tags = %v", result.Tags)
	}
	if !strings.Contains(out.String(), "AI 摘要") {
		t.Errorf("output should show suggestion preview: %q", out.String())
	}
	if !strings.Contains(out.String(), "[a]采纳") {
		t.Errorf("output should show [a/e/s] hint")
	}
}

func TestSuggestAndPrompt_Skip(t *testing.T) {
	srv := hubMockServer(t, `{"code":0,"data":{
		"suggestions":{"summary":"AI 摘要","tags":["a","b"]},"confidence":0.7
	}}`, 200, nil)
	defer srv.Close()
	c := hub.NewClient(srv.URL, "x")

	in := strings.NewReader("s\n")
	result, err := SuggestAndPrompt(context.Background(), c, in, &bytes.Buffer{},
		kstypes.AppSpec{ID: "x", Type: "skill"}, "md", []string{"summary", "tags"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "" || len(result.Tags) != 0 {
		t.Errorf("[s] should yield empty result, got %+v", result)
	}
}

func TestSuggestAndPrompt_EditOverridesSummary(t *testing.T) {
	srv := hubMockServer(t, `{"code":0,"data":{
		"suggestions":{"summary":"AI 摘要","tags":["a","b","c"]},"confidence":0.7
	}}`, 200, nil)
	defer srv.Close()
	c := hub.NewClient(srv.URL, "x")

	// 输入：[e]编辑 → summary 改为 "我的摘要" → tags 直接回车采纳 default
	in := strings.NewReader("e\n我的摘要\n\n")
	result, err := SuggestAndPrompt(context.Background(), c, in, &bytes.Buffer{},
		kstypes.AppSpec{ID: "x", Type: "skill"}, "md", []string{"summary", "tags"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "我的摘要" {
		t.Errorf("Summary = %q", result.Summary)
	}
	if len(result.Tags) != 3 {
		t.Errorf("expected default tags retained, got %v", result.Tags)
	}
}

func TestSuggestAndPrompt_LLMUnavailable_FallsBackToInline(t *testing.T) {
	srv := hubMockServer(t, "", 503, nil)
	defer srv.Close()
	c := hub.NewClient(srv.URL, "x")

	in := strings.NewReader("我的摘要\ntdd, 测试\n")
	out := &bytes.Buffer{}
	result, err := SuggestAndPrompt(context.Background(), c, in, out,
		kstypes.AppSpec{ID: "x", Type: "skill"}, "md", []string{"summary", "tags"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "我的摘要" {
		t.Errorf("Summary = %q", result.Summary)
	}
	if len(result.Tags) != 2 || result.Tags[0] != "tdd" || result.Tags[1] != "测试" {
		t.Errorf("Tags = %v", result.Tags)
	}
	// 文案不暗示 chain 失败，但应有 "AI 建议暂不可用" 提示
	if !strings.Contains(out.String(), "AI 建议暂不可用") {
		t.Errorf("output should contain LLM unavailable hint: %q", out.String())
	}
}

func TestSuggestAndPrompt_PathAndPayload(t *testing.T) {
	var gotPath string
	srv := hubMockServer(t, `{"code":0,"data":{"suggestions":{}}}`, 200, &gotPath)
	defer srv.Close()
	c := hub.NewClient(srv.URL, "x")

	in := strings.NewReader("s\n")
	_, _ = SuggestAndPrompt(context.Background(), c, in, &bytes.Buffer{},
		kstypes.AppSpec{ID: "skill-tdd", Type: "skill"}, "md", []string{"summary"})
	if gotPath != "/v1/developer/devkit/manifest/suggest" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestSuggestAndPrompt_TagsParsedFromCommaList(t *testing.T) {
	srv := hubMockServer(t, `{"code":0,"data":{"suggestions":{}}}`, 200, nil)
	defer srv.Close()
	c := hub.NewClient(srv.URL, "x")

	// 编辑模式：tags 输入逗号分隔列表 → result.Tags 应正确解析
	in := strings.NewReader("e\n手工 tag, 另一个\n")
	result, _ := SuggestAndPrompt(context.Background(), c, in, &bytes.Buffer{},
		kstypes.AppSpec{ID: "x", Type: "skill"}, "md", []string{"tags"})
	if len(result.Tags) != 2 || result.Tags[0] != "手工 tag" || result.Tags[1] != "另一个" {
		t.Errorf("Tags = %v", result.Tags)
	}
}
