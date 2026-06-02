package manifest

import (
	"reflect"
	"testing"

	kstypes "github.com/wuhanyuhan/ks-types"
)

func TestComputeMissingFields_AllPresent(t *testing.T) {
	spec := kstypes.AppSpec{
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
	got := ComputeMissingFields(spec)
	if len(got) != 0 {
		t.Errorf("expected no missing, got %v", got)
	}
}

func TestComputeMissingFields_AllMissing(t *testing.T) {
	spec := kstypes.AppSpec{
		ID: "x", Name: "x", Version: "0.1.0", Type: "skill",
	}
	got := ComputeMissingFields(spec)
	want := []string{"summary", "description", "category", "tags", "store.audience", "store.highlights", "store.try_prompts", "changelog"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestComputeMissingFields_PartialEmpty(t *testing.T) {
	spec := kstypes.AppSpec{
		ID: "x", Name: "x", Version: "0.1.0", Type: "skill",
		Summary: kstypes.LocalizedString{"": "ok"},
	}
	got := ComputeMissingFields(spec)
	if containsString(got, "summary") {
		t.Errorf("summary should not be missing: %v", got)
	}
	if !containsString(got, "tags") {
		t.Errorf("tags should be missing: %v", got)
	}
	if !containsString(got, "store.highlights") {
		t.Errorf("store.highlights should be missing: %v", got)
	}
}

func TestComputeMissingFields_LocalizedSingleStringCounts(t *testing.T) {
	// 单 string 形态有内容算 present
	spec := kstypes.AppSpec{Summary: kstypes.LocalizedString{"": "x"}}
	if containsString(ComputeMissingFields(spec), "summary") {
		t.Error("single-form non-empty should be present")
	}
}

func TestComputeMissingFields_ZhCNOnlyCountsAsPresent(t *testing.T) {
	// 仅填了 zh-CN locale 也算 present
	spec := kstypes.AppSpec{
		Summary: kstypes.LocalizedString{"zh-CN": "中文摘要"},
		Tags:    kstypes.LocalizedTags{"zh-CN": []string{"标签"}},
	}
	got := ComputeMissingFields(spec)
	if containsString(got, "summary") {
		t.Errorf("zh-CN summary should be present, got missing %v", got)
	}
	if containsString(got, "tags") {
		t.Errorf("zh-CN tags should be present, got missing %v", got)
	}
}

func TestComputeMissingFields_EmptyMapTreatedAsMissing(t *testing.T) {
	// LocalizedString{"": ""} 应当算 missing
	spec := kstypes.AppSpec{
		Summary: kstypes.LocalizedString{"": ""},
		Tags:    kstypes.LocalizedTags{"": {}},
	}
	got := ComputeMissingFields(spec)
	if !containsString(got, "summary") {
		t.Error("empty-string summary should be missing")
	}
	if !containsString(got, "tags") {
		t.Error("empty-slice tags should be missing")
	}
}
