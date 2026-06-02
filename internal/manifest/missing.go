// Package manifest 提供 ks publish 流程中 manifest fallback chain 的辅助函数。
//
// fallback chain 三层：
//  1. 调 hub LLM suggest 端点拿建议（summary/description/category/tags），作者 [a]采纳/[e]编辑/[s]跳过
//  2. 从仓库根 / skill 目录的 CHANGELOG.md 抽取目标版本 section（仅处理 changelog 字段）
//  3. inline editor 交互式补字段（io.Reader/Writer 接口便于 mock）
//
// 入口在 fallback_chain.go 的 RunFallbackChain。
package manifest

import (
	kstypes "github.com/wuhanyuhan/ks-types"
)

// containsString 返回 s 是否包含 target。fallback chain 编排与单测共用。
func containsString(s []string, target string) bool {
	for _, v := range s {
		if v == target {
			return true
		}
	}
	return false
}

// filterStrings 返回 s 中仅保留出现在 allowed 中的元素，顺序与 s 一致。
// LLM 层只处理 summary/description/category/tags，用此过滤排除 changelog。
func filterStrings(s, allowed []string) []string {
	set := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		set[a] = true
	}
	out := make([]string, 0, len(s))
	for _, x := range s {
		if set[x] {
			out = append(out, x)
		}
	}
	return out
}

// ComputeMissingFields 返回 manifest 中应该补齐但当前为空的字段名。
//
// 字段名与 hub /v1/developer/devkit/manifest/suggest 的 missing_fields 入参对齐：
// summary / description / category / tags / changelog。返回顺序固定，便于测试断言
// 与 UI 上展示的稳定顺序。
//
// 判定规则：
//   - LocalizedString / LocalizedTags：用 .Get("") 走内置 fallback chain
//     （locale → zh-CN → "" → 任意非空），首个非空即视为 present
//   - Category / Changelog：纯 string，非空即 present
func ComputeMissingFields(spec kstypes.AppSpec) []string {
	var missing []string
	if spec.Summary.Get("") == "" {
		missing = append(missing, "summary")
	}
	if spec.Description.Get("") == "" {
		missing = append(missing, "description")
	}
	if spec.Category == "" {
		missing = append(missing, "category")
	}
	if len(spec.Tags.Get("")) == 0 {
		missing = append(missing, "tags")
	}
	if len(nonEmptyStoreStrings(spec.Store.Audience)) == 0 {
		missing = append(missing, "store.audience")
	}
	if len(nonEmptyStoreStrings(spec.Store.Highlights)) == 0 {
		missing = append(missing, "store.highlights")
	}
	if len(nonEmptyStoreStrings(spec.Store.TryPrompts)) == 0 {
		missing = append(missing, "store.try_prompts")
	}
	if spec.Changelog == "" {
		missing = append(missing, "changelog")
	}
	return missing
}
