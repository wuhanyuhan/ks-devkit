package manifest

import (
	"strings"
	"testing"

	kstypes "github.com/wuhanyuhan/ks-types"
)

func TestValidateStoreQuality_ExpertTeamRequiresMembers(t *testing.T) {
	spec := &kstypes.AppSpec{
		ID:   "ks-mcp-squad-legal",
		Type: kstypes.AppTypeApp,
		Store: kstypes.StoreSpec{
			Presentation: kstypes.StorePresentationExpertTeam,
			Highlights:   []string{"多角色协作完成合同审查"},
			TryPrompts:   []string{"让法务专家团审查这个合同"},
		},
	}
	err := ValidateStoreQuality(spec)
	if err == nil {
		t.Fatal("expert_team without members should fail")
	}
	if !strings.Contains(err.Error(), "store.team.members") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateStoreQuality_SkillRequiresTryPrompts(t *testing.T) {
	spec := &kstypes.AppSpec{
		ID:   "skill-tdd",
		Type: kstypes.AppTypeSkill,
		Store: kstypes.StoreSpec{
			Presentation: kstypes.StorePresentationMethodSkill,
			Highlights:   []string{"提供 TDD 开发流程"},
		},
	}
	err := ValidateStoreQuality(spec)
	if err == nil {
		t.Fatal("skill without try_prompts should fail")
	}
	if !strings.Contains(err.Error(), "store.try_prompts") {
		t.Fatalf("unexpected error: %v", err)
	}

	spec.Store.TryPrompts = []string{"帮我用 TDD 开发这个功能"}
	if err := ValidateStoreQuality(spec); err != nil {
		t.Fatalf("valid skill store quality should pass: %v", err)
	}
}

func TestValidateStoreQuality_RequiresPresentation(t *testing.T) {
	spec := &kstypes.AppSpec{
		ID:   "ks-mcp-writer",
		Type: kstypes.AppTypeApp,
		Store: kstypes.StoreSpec{
			Highlights: []string{"提供文章写作能力"},
			TryPrompts: []string{"帮我写一篇文章"},
		},
	}
	err := ValidateStoreQuality(spec)
	if err == nil {
		t.Fatal("missing store.presentation should fail")
	}
	if !strings.Contains(err.Error(), "store.presentation") {
		t.Fatalf("unexpected error: %v", err)
	}
}
