package manifest

import (
	"fmt"
	"strings"

	kstypes "github.com/wuhanyuhan/ks-types"
)

// ValidateStoreQuality 校验新发布应用的 Store 展示字段是否达到最低上架质量。
func ValidateStoreQuality(spec *kstypes.AppSpec) error {
	if spec == nil {
		return fmt.Errorf("manifest 为空")
	}
	if strings.TrimSpace(string(spec.Store.Presentation)) == "" {
		return fmt.Errorf("store.presentation 为必填")
	}
	if len(nonEmptyStoreStrings(spec.Store.Highlights)) == 0 {
		return fmt.Errorf("store.highlights 至少需要 1 条")
	}
	if len(nonEmptyStoreStrings(spec.Store.TryPrompts)) == 0 {
		return fmt.Errorf("store.try_prompts 至少需要 1 条")
	}
	if spec.Store.Presentation == kstypes.StorePresentationExpertTeam {
		if spec.Store.Team == nil || len(spec.Store.Team.Members) == 0 {
			return fmt.Errorf("store.presentation=expert_team 时 store.team.members 为必填")
		}
		for i, member := range spec.Store.Team.Members {
			if strings.TrimSpace(member.Key) == "" ||
				strings.TrimSpace(member.Name) == "" ||
				strings.TrimSpace(member.Avatar) == "" {
				return fmt.Errorf("store.team.members[%d] 必须包含 key/name/avatar", i)
			}
		}
	}
	return nil
}

func nonEmptyStoreStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
