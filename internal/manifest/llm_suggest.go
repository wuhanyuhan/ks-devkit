package manifest

import (
	"context"
	"fmt"
	"io"
	"strings"

	kstypes "github.com/wuhanyuhan/ks-types"
	"gopkg.in/yaml.v3"

	"github.com/wuhanyuhan/ks-devkit/internal/hub"
)

// Suggestions 是 fallback chain 第 1/3 级返给上层的"已收到的字段值"集合。
// 字段为空表示作者跳过该字段（fallback chain 不再尝试覆盖）。
//
// 与 hub.ManifestSuggestions 字段一致但语义不同：hub 那个是 wire format（LLM 原始建议），
// 这个是 CLI 流程结束后拿到的最终值（可能是 LLM 原值、作者修改值、或纯手工填的值）。
type Suggestions struct {
	Summary     string
	Description string
	Category    string
	Tags        []string
}

// SuggestAndPrompt 是 fallback chain 第 1 级核心：
//  1. 调 hub /v1/developer/devkit/manifest/suggest 拿 LLM 建议
//  2. 展示 yaml-style 预览 + 让作者三选 [a]采纳 / [e]编辑 / [s]跳过
//  3. LLM 不可用（5xx / 业务码 / 网络错）静默降级到 inline editor
//
// ctx 当前仅作上层 cancel 占位，不透传给 hub.Client（client 自带 30s timeout）。
// missing 应只含 LLM 处理的字段（summary/description/category/tags）；
// changelog 由 fallback chain 高层单独处理，不应进 missing。
//
// 返回 *Suggestions 永不为 nil，调用方据 .Summary == "" 等判定字段是否被作者填了。
func SuggestAndPrompt(
	ctx context.Context,
	client *hub.Client,
	in io.Reader,
	out io.Writer,
	spec kstypes.AppSpec,
	skillMd string,
	missing []string,
) (*Suggestions, error) {
	_ = ctx // hub.Client 不接 ctx；ctx 占位便于上层加 timeout/cancel

	resp, err := client.SuggestManifest(hub.SuggestManifestRequest{
		AppID:           spec.ID,
		SkillMdText:     skillMd,
		CurrentManifest: manifestToMap(spec),
		MissingFields:   missing,
	})
	if err != nil {
		// 静默降级：文案不暗示 chain 失败
		fmt.Fprintln(out, "（AI 建议暂不可用，请手动补充）")
		return promptByFields(in, out, missing, hub.ManifestSuggestions{})
	}

	fmt.Fprintf(out, "\nAI 建议（confidence %.2f", resp.Confidence)
	if resp.LLMModel != "" {
		fmt.Fprintf(out, ", model %s", resp.LLMModel)
	}
	fmt.Fprintln(out, "）：")
	printSuggestionPreview(out, resp.Suggestions, missing)
	if resp.Rationale != "" {
		fmt.Fprintf(out, "理由：%s\n", resp.Rationale)
	}
	fmt.Fprintln(out, "\n[a]采纳 / [e]编辑 / [s]跳过")
	fmt.Fprint(out, "> ")

	choice, err := readLineNoBuffer(in)
	if err != nil {
		return nil, err
	}
	switch strings.TrimSpace(strings.ToLower(choice)) {
	case "s":
		return &Suggestions{}, nil
	case "e":
		return promptByFields(in, out, missing, resp.Suggestions)
	case "a", "":
		fallthrough
	default:
		return suggestionsFromHub(resp.Suggestions), nil
	}
}

// printSuggestionPreview 把 LLM 建议按 missing 字段顺序打成 yaml-ish 预览。
// 简化版：不真正做 diff（缺字段本来就没原值可对），仅展示建议值。
// description 截断展示前 60 字以免刷屏，作者编辑时仍可看到完整值。
func printSuggestionPreview(out io.Writer, sugg hub.ManifestSuggestions, missing []string) {
	for _, f := range missing {
		switch f {
		case "summary":
			if sugg.Summary != "" {
				fmt.Fprintf(out, "  summary: %s\n", sugg.Summary)
			}
		case "description":
			if sugg.Description != "" {
				fmt.Fprintf(out, "  description: %s\n", abbrev(sugg.Description, 60))
			}
		case "category":
			if sugg.Category != "" {
				fmt.Fprintf(out, "  category: %s\n", sugg.Category)
			}
		case "tags":
			if len(sugg.Tags) > 0 {
				fmt.Fprintf(out, "  tags: [%s]\n", strings.Join(sugg.Tags, ", "))
			}
		}
	}
}

// promptByFields 串行调 PromptForField 收集每个 missing 字段。
// defaults 提供 LLM 建议作为各字段的 default：用户直接回车 = 采纳建议（[e]编辑流程）；
// defaults 全空时 = 纯手工补字段（LLM 不可用降级路径）。
func promptByFields(in io.Reader, out io.Writer, missing []string, defaults hub.ManifestSuggestions) (*Suggestions, error) {
	res := &Suggestions{}
	for _, f := range missing {
		def := defaultForField(defaults, f)
		v, err := PromptForField(in, out, f, def)
		if err != nil {
			return res, err
		}
		if v == "" {
			continue
		}
		applyToSuggestions(res, f, v)
	}
	return res, nil
}

// defaultForField 把 hub.ManifestSuggestions 里对应字段值转成 PromptForField 的 default 字符串。
// tags 用 ", " 拼，与 PromptForField 提示语一致；空值返回 ""（PromptForField 不显示 default 提示）。
func defaultForField(s hub.ManifestSuggestions, field string) string {
	switch field {
	case "summary":
		return s.Summary
	case "description":
		return s.Description
	case "category":
		return s.Category
	case "tags":
		if len(s.Tags) == 0 {
			return ""
		}
		return strings.Join(s.Tags, ", ")
	default:
		// changelog 由高层单独处理，这里不该被调
		return ""
	}
}

// applyToSuggestions 把单字段值写入 *Suggestions。
// tags 字段做逗号分隔解析。changelog 字段忽略（高层处理）。
func applyToSuggestions(s *Suggestions, field, value string) {
	switch field {
	case "summary":
		s.Summary = value
	case "description":
		s.Description = value
	case "category":
		s.Category = value
	case "tags":
		for _, t := range strings.Split(value, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				s.Tags = append(s.Tags, t)
			}
		}
	case "changelog":
		// 忽略，由 fallback chain 高层处理
	}
}

// suggestionsFromHub 把 hub LLM 建议直接转成 Suggestions（[a]采纳路径）。
func suggestionsFromHub(h hub.ManifestSuggestions) *Suggestions {
	out := &Suggestions{
		Summary:     h.Summary,
		Description: h.Description,
		Category:    h.Category,
	}
	if len(h.Tags) > 0 {
		out.Tags = append(out.Tags, h.Tags...)
	}
	return out
}

// manifestToMap 把 AppSpec 序列化为 map，作为 SuggestManifestRequest.CurrentManifest 入参。
// LLM 用此参考已填字段（如同 publisher 的 tags 风格），避免重复建议。
//
// 用 yaml round-trip 而非反射：保留 yaml tag 语义、忽略 ",omitempty" 字段。
// 失败返 nil（hub 端容忍空入参）。
func manifestToMap(spec kstypes.AppSpec) map[string]any {
	data, err := yaml.Marshal(spec)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil
	}
	return m
}

// abbrev 把长字符串截断到 max 字符并加省略号。
// max <= 0 或 s 短于 max 时直接返原值。
func abbrev(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}
