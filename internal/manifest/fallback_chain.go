package manifest

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	kstypes "github.com/wuhanyuhan/ks-types"

	"github.com/wuhanyuhan/ks-devkit/internal/hub"
)

// FallbackInputs 是 RunFallbackChain 的入参。
//
//	Spec:        当前 manifest 解析结果（缺字段也照样传进来，由本包计算 missing）
//	SkillMdText: SKILL.md / README 全文，给 LLM 作主输入
//	RepoDir:     项目根目录，用来在层 2 找 CHANGELOG.md（与 manifest.yaml 同级）
type FallbackInputs struct {
	Spec        kstypes.AppSpec
	SkillMdText string
	RepoDir     string
}

// RunFallbackChain 是 ks publish 缺字段引导式补齐的总编排：
//
//  1. 计算 missing fields；空则直接返原 spec（最常见路径，不交互）
//  2. 层 1 LLM suggest：处理 summary/description/category/tags
//     hub 端不可用时静默降级到层 3 inline editor（仅这部分字段）
//  3. 层 2 CHANGELOG.md：仅 changelog 字段
//     仓库根（RepoDir）有 CHANGELOG.md 时尝试本地正则抽取目标版本 section
//     本地未命中调 hub.ParseChangelog 兜底
//     无 CHANGELOG.md（如 ks-skills 当前现状）时静默跳过，文案不暗示 chain 失败
//  4. 层 3 inline editor：上述层处理后仍缺的字段全部 PromptForField
//
// 错误返回时 result 已包含截至错误前已收到的所有字段（部分进度），便于调用方回写 manifest.yaml。
func RunFallbackChain(
	ctx context.Context,
	hubC *hub.Client,
	in io.Reader,
	out io.Writer,
	inp FallbackInputs,
) (kstypes.AppSpec, error) {
	missing := ComputeMissingFields(inp.Spec)
	if len(missing) == 0 {
		return inp.Spec, nil
	}
	result := inp.Spec

	// 层 1：LLM suggest（不处理 changelog）
	llmFields := filterStrings(missing, []string{"summary", "description", "category", "tags"})
	if len(llmFields) > 0 && hubC != nil {
		sugg, err := SuggestAndPrompt(ctx, hubC, in, out, inp.Spec, inp.SkillMdText, llmFields)
		if err != nil {
			return result, err
		}
		applySuggestionsToSpec(&result, sugg)
		missing = ComputeMissingFields(result)
	}

	// 层 2：CHANGELOG.md（仅 changelog 字段）
	if containsString(missing, "changelog") {
		if path, ok := FindChangelogPath(inp.RepoDir); ok {
			data, _ := os.ReadFile(path)
			if section, found := LocalExtractChangelogSection(string(data), inp.Spec.Version); found {
				result.Changelog = section
				fmt.Fprintf(out, "已从 %s 抽取 v%s 的 changelog\n", filepath.Base(path), inp.Spec.Version)
			} else if hubC != nil {
				// 本地未命中：调 hub 端兜底（hub 解析规则更宽容）
				resp, herr := hubC.ParseChangelog(hub.ParseChangelogRequest{
					AppID:           inp.Spec.ID,
					Version:         inp.Spec.Version,
					ChangelogMDText: string(data),
				})
				if herr == nil && resp.Parsed.Found {
					result.Changelog = resp.Parsed.VersionSection
					fmt.Fprintf(out, "已从 %s（hub 兜底解析）抽取 v%s 的 changelog\n", filepath.Base(path), inp.Spec.Version)
				}
			}
			missing = ComputeMissingFields(result)
		}
		// 找不到 CHANGELOG.md：静默跳过；下面层 3 会顺势处理 changelog 字段
	}

	// 层 3：inline editor（剩下字段全靠手填）
	for _, field := range missing {
		v, err := PromptForField(in, out, field, "")
		if err != nil {
			return result, err
		}
		if v == "" {
			continue
		}
		applyFieldToSpec(&result, field, v)
	}

	return result, nil
}

// applySuggestionsToSpec 把 SuggestAndPrompt 返回的 *Suggestions 写回 AppSpec。
// 仅在建议非空时覆盖原字段；保留作者已填部分（虽然 ComputeMissingFields 已只让 LLM 处理缺字段，
// 这里再次防御以保防作者 race）。
//
// LocalizedString / LocalizedTags 用 zh-CN locale 写入：作者后续可手工改为 i18n map 形态。
func applySuggestionsToSpec(spec *kstypes.AppSpec, sugg *Suggestions) {
	if sugg == nil {
		return
	}
	if sugg.Summary != "" {
		spec.Summary = kstypes.LocalizedString{"zh-CN": sugg.Summary}
	}
	if sugg.Description != "" {
		spec.Description = kstypes.LocalizedString{"zh-CN": sugg.Description}
	}
	if sugg.Category != "" {
		spec.Category = sugg.Category
	}
	if len(sugg.Tags) > 0 {
		spec.Tags = kstypes.LocalizedTags{"zh-CN": sugg.Tags}
	}
}

// applyFieldToSpec 把 inline editor 单字段输入写回 AppSpec。
// tags 做逗号分隔解析；其他字段直接赋值。
func applyFieldToSpec(spec *kstypes.AppSpec, field, value string) {
	switch field {
	case "summary":
		spec.Summary = kstypes.LocalizedString{"zh-CN": value}
	case "description":
		spec.Description = kstypes.LocalizedString{"zh-CN": value}
	case "category":
		spec.Category = value
	case "tags":
		tags := splitCommaList(value)
		if len(tags) > 0 {
			spec.Tags = kstypes.LocalizedTags{"zh-CN": tags}
		}
	case "store.audience":
		spec.Store.Audience = splitCommaList(value)
	case "store.highlights":
		spec.Store.Highlights = splitCommaList(value)
	case "store.try_prompts":
		spec.Store.TryPrompts = splitCommaList(value)
	case "changelog":
		spec.Changelog = value
	}
}

func splitCommaList(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			values = append(values, item)
		}
	}
	return values
}
