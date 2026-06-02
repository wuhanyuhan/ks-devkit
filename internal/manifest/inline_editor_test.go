package manifest

import (
	"bytes"
	"strings"
	"testing"
)

func TestInlineEditor_PromptSummary(t *testing.T) {
	in := strings.NewReader("测试 TDD\n")
	out := &bytes.Buffer{}
	got, err := PromptForField(in, out, "summary", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "测试 TDD" {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(out.String(), "summary") && !strings.Contains(out.String(), "摘要") {
		t.Errorf("output should contain field hint: %q", out.String())
	}
}

func TestInlineEditor_PromptTagsCommaSeparated(t *testing.T) {
	in := strings.NewReader("tdd, 测试, 重构\n")
	got, err := PromptForField(in, &bytes.Buffer{}, "tags", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "tdd, 测试, 重构" {
		t.Errorf("got %q", got)
	}
}

func TestInlineEditor_PromptCategorySingle(t *testing.T) {
	in := strings.NewReader("开发流程\n")
	got, _ := PromptForField(in, &bytes.Buffer{}, "category", "")
	if got != "开发流程" {
		t.Errorf("got %q", got)
	}
}

func TestInlineEditor_SkipOnEmptyInputNoDefault(t *testing.T) {
	in := strings.NewReader("\n")
	got, _ := PromptForField(in, &bytes.Buffer{}, "summary", "")
	if got != "" {
		t.Errorf("expected skip (empty), got %q", got)
	}
}

func TestInlineEditor_PrefillDefault(t *testing.T) {
	in := strings.NewReader("\n")
	got, _ := PromptForField(in, &bytes.Buffer{}, "summary", "default 摘要")
	if got != "default 摘要" {
		t.Errorf("got %q", got)
	}
}

func TestInlineEditor_PrefillShownInPrompt(t *testing.T) {
	in := strings.NewReader("\n")
	out := &bytes.Buffer{}
	_, _ = PromptForField(in, out, "summary", "default 摘要")
	if !strings.Contains(out.String(), "default 摘要") {
		t.Errorf("default value should be shown in prompt: %q", out.String())
	}
}

func TestInlineEditor_ChangelogMultilineEndedByEnd(t *testing.T) {
	in := strings.NewReader("### Added\n- TDD\n- 重构\n::end\n")
	got, err := PromptForField(in, &bytes.Buffer{}, "changelog", "")
	if err != nil {
		t.Fatal(err)
	}
	want := "### Added\n- TDD\n- 重构"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInlineEditor_DescriptionMultilineEndedByEOF(t *testing.T) {
	in := strings.NewReader("第一行\n第二行")
	got, _ := PromptForField(in, &bytes.Buffer{}, "description", "")
	want := "第一行\n第二行"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInlineEditor_ChangelogEmptyUsesDefault(t *testing.T) {
	in := strings.NewReader("::end\n")
	got, _ := PromptForField(in, &bytes.Buffer{}, "changelog", "default changelog")
	if got != "default changelog" {
		t.Errorf("got %q", got)
	}
}

func TestInlineEditor_ChangelogEmptyNoDefault(t *testing.T) {
	in := strings.NewReader("::end\n")
	got, _ := PromptForField(in, &bytes.Buffer{}, "changelog", "")
	if got != "" {
		t.Errorf("got %q", got)
	}
}

func TestInlineEditor_ChainedCallsReuseReader(t *testing.T) {
	// fallback chain 需求：多次调用 PromptForField 共用同一 io.Reader 不丢字节
	in := strings.NewReader("first\nsecond\nthird\n")
	a, _ := PromptForField(in, &bytes.Buffer{}, "summary", "")
	b, _ := PromptForField(in, &bytes.Buffer{}, "category", "")
	c, _ := PromptForField(in, &bytes.Buffer{}, "tags", "")
	if a != "first" || b != "second" || c != "third" {
		t.Errorf("got %q/%q/%q, want first/second/third", a, b, c)
	}
}

func TestInlineEditor_ChainedSingleAfterMultilineReader(t *testing.T) {
	// 多行字段读完 ::end 后，剩余字节仍可被下一次 PromptForField 读到
	in := strings.NewReader("### Added\n- x\n::end\nnext-line\n")
	cl, _ := PromptForField(in, &bytes.Buffer{}, "changelog", "")
	nx, _ := PromptForField(in, &bytes.Buffer{}, "summary", "")
	if cl != "### Added\n- x" {
		t.Errorf("changelog = %q", cl)
	}
	if nx != "next-line" {
		t.Errorf("next single line = %q", nx)
	}
}

func TestInlineEditor_UnknownFieldUsesFieldNameAsLabel(t *testing.T) {
	in := strings.NewReader("value\n")
	out := &bytes.Buffer{}
	got, _ := PromptForField(in, out, "weird_unknown_field", "")
	if got != "value" {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(out.String(), "weird_unknown_field") {
		t.Errorf("output should fall back to field name: %q", out.String())
	}
}
