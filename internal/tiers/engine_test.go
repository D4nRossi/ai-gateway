package tiers

import "testing"

// TestPipelineFor verifies that each tier produces the correct combination of
// guardrail flags and fail-mode, matching the matrix in SPEC §5.3.
//
// References:
//   - SPEC.md §5.3 — tier pipeline matrix
func TestPipelineFor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tier string
		want Pipeline
	}{
		{
			tier: "tier_1",
			want: Pipeline{
				RunLocalMasking:   true,
				RunLocalInjection: false,
				RunPromptShield:   false,
				RunContentSafety:  false,
				RunPostValidation: false,
				FailMode:          "open",
			},
		},
		{
			tier: "tier_2",
			want: Pipeline{
				RunLocalMasking:   true,
				RunLocalInjection: true,
				RunPromptShield:   false,
				RunContentSafety:  false,
				RunPostValidation: false,
				FailMode:          "open",
			},
		},
		{
			tier: "tier_3",
			want: Pipeline{
				RunLocalMasking:   true,
				RunLocalInjection: true,
				RunPromptShield:   true,
				RunContentSafety:  true,
				RunPostValidation: true,
				FailMode:          "closed",
			},
		},
		{
			// Unknown tier: fail-closed on all guardrails is the safe default.
			tier: "tier_unknown",
			want: Pipeline{
				RunLocalMasking:   true,
				RunLocalInjection: true,
				RunPromptShield:   true,
				RunContentSafety:  true,
				RunPostValidation: true,
				FailMode:          "closed",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run("tier="+tc.tier, func(t *testing.T) {
			t.Parallel()
			got := PipelineFor(tc.tier)
			if got != tc.want {
				t.Errorf("PipelineFor(%q):\n got  %+v\n want %+v", tc.tier, got, tc.want)
			}
		})
	}
}
