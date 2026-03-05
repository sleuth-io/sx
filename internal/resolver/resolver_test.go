package resolver

import (
	"testing"

	"github.com/sleuth-io/sx/internal/requirements"
)

func TestSkillsShAssetName(t *testing.T) {
	tests := []struct {
		name string
		req  requirements.Requirement
		want string
	}{
		{
			name: "with skill name",
			req: requirements.Requirement{
				Type:              requirements.RequirementTypeSkillsSh,
				SkillsShOwnerRepo: "anthropics/skills",
				SkillsShSkillName: "frontend-design",
			},
			want: "frontend-design",
		},
		{
			name: "whole repo uses repo name",
			req: requirements.Requirement{
				Type:              requirements.RequirementTypeSkillsSh,
				SkillsShOwnerRepo: "vercel-labs/agent-skills",
			},
			want: "agent-skills",
		},
		{
			name: "simple owner/repo",
			req: requirements.Requirement{
				Type:              requirements.RequirementTypeSkillsSh,
				SkillsShOwnerRepo: "org/my-repo",
			},
			want: "my-repo",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := skillsShAssetName(tc.req)
			if got != tc.want {
				t.Errorf("skillsShAssetName() = %q, want %q", got, tc.want)
			}
		})
	}
}
