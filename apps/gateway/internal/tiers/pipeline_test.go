package tiers

import (
	"testing"
)

// TestPipelineFor_Tier1_Config verifies every field of the tier_1 pipeline.
func TestPipelineFor_Tier1_Config(t *testing.T) {
	t.Parallel()
	p := PipelineFor("tier_1")

	checks := []struct {
		field string
		got   bool
		want  bool
	}{
		{"RunLocalMasking", p.RunLocalMasking, true},
		{"RunLocalInjection", p.RunLocalInjection, false},
		{"RunPromptShield", p.RunPromptShield, false},
		{"RunContentSafety", p.RunContentSafety, false},
		{"RunPostValidation", p.RunPostValidation, false},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("tier_1 %s = %v; want %v", c.field, c.got, c.want)
		}
	}
	if p.FailMode != "open" {
		t.Errorf("tier_1 FailMode = %q; want %q", p.FailMode, "open")
	}
}

// TestPipelineFor_Tier2_Config verifies every field of the tier_2 pipeline.
func TestPipelineFor_Tier2_Config(t *testing.T) {
	t.Parallel()
	p := PipelineFor("tier_2")

	checks := []struct {
		field string
		got   bool
		want  bool
	}{
		{"RunLocalMasking", p.RunLocalMasking, true},
		{"RunLocalInjection", p.RunLocalInjection, true},
		{"RunPromptShield", p.RunPromptShield, false},
		{"RunContentSafety", p.RunContentSafety, false},
		{"RunPostValidation", p.RunPostValidation, false},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("tier_2 %s = %v; want %v", c.field, c.got, c.want)
		}
	}
	if p.FailMode != "open" {
		t.Errorf("tier_2 FailMode = %q; want %q", p.FailMode, "open")
	}
}

// TestPipelineFor_Tier3_Config verifies every field of the tier_3 pipeline.
func TestPipelineFor_Tier3_Config(t *testing.T) {
	t.Parallel()
	p := PipelineFor("tier_3")

	checks := []struct {
		field string
		got   bool
		want  bool
	}{
		{"RunLocalMasking", p.RunLocalMasking, true},
		{"RunLocalInjection", p.RunLocalInjection, true},
		{"RunPromptShield", p.RunPromptShield, true},
		{"RunContentSafety", p.RunContentSafety, true},
		{"RunPostValidation", p.RunPostValidation, true},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("tier_3 %s = %v; want %v", c.field, c.got, c.want)
		}
	}
	if p.FailMode != "closed" {
		t.Errorf("tier_3 FailMode = %q; want %q", p.FailMode, "closed")
	}
}

// TestPipelineFor_Unknown_FailClosed verifies unknown tiers default to
// the most restrictive pipeline (fail-closed, all guards active).
func TestPipelineFor_Unknown_FailClosed(t *testing.T) {
	t.Parallel()
	unknowns := []string{"", "tier_0", "tier_4", "TIER_1", "Tier1", "free", "admin"}

	for _, tier := range unknowns {
		tier := tier
		t.Run(tier, func(t *testing.T) {
			t.Parallel()
			p := PipelineFor(tier)
			if p.FailMode != "closed" {
				t.Errorf("PipelineFor(%q).FailMode = %q; want %q (unknown tier must fail-closed)", tier, p.FailMode, "closed")
			}
			if !p.RunLocalMasking {
				t.Errorf("PipelineFor(%q).RunLocalMasking = false; want true", tier)
			}
			if !p.RunLocalInjection {
				t.Errorf("PipelineFor(%q).RunLocalInjection = false; want true", tier)
			}
			if !p.RunPromptShield {
				t.Errorf("PipelineFor(%q).RunPromptShield = false; want true", tier)
			}
			if !p.RunContentSafety {
				t.Errorf("PipelineFor(%q).RunContentSafety = false; want true", tier)
			}
			if !p.RunPostValidation {
				t.Errorf("PipelineFor(%q).RunPostValidation = false; want true", tier)
			}
		})
	}
}

// TestPipelineFor_FailModeSemantics verifies the fail-mode invariant:
//   - Tier 1 and Tier 2 must be fail-open (external service errors don't block requests)
//   - Tier 3 must be fail-closed (external service errors block requests)
//
// References:
//   - SPEC.md §11.4 — fail-mode semantics
func TestPipelineFor_FailModeSemantics(t *testing.T) {
	t.Parallel()
	cases := []struct {
		tier         string
		wantFailMode string
	}{
		{"tier_1", "open"},
		{"tier_2", "open"},
		{"tier_3", "closed"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.tier, func(t *testing.T) {
			t.Parallel()
			p := PipelineFor(tc.tier)
			if p.FailMode != tc.wantFailMode {
				t.Errorf("%s: FailMode = %q; want %q", tc.tier, p.FailMode, tc.wantFailMode)
			}
		})
	}
}

// TestPipelineFor_GuardEscalation verifies that each tier activates a superset
// of guards compared to the previous tier (no regression in security coverage).
func TestPipelineFor_GuardEscalation(t *testing.T) {
	t.Parallel()
	t1 := PipelineFor("tier_1")
	t2 := PipelineFor("tier_2")
	t3 := PipelineFor("tier_3")

	// tier_2 must have everything tier_1 has, plus more.
	if !t2.RunLocalMasking && t1.RunLocalMasking {
		t.Error("tier_2 drops RunLocalMasking that tier_1 had")
	}
	if !t2.RunLocalInjection {
		t.Error("tier_2 must add RunLocalInjection over tier_1")
	}

	// tier_3 must have everything tier_2 has, plus more.
	if !t3.RunLocalMasking {
		t.Error("tier_3 must keep RunLocalMasking")
	}
	if !t3.RunLocalInjection {
		t.Error("tier_3 must keep RunLocalInjection")
	}
	if !t3.RunPromptShield {
		t.Error("tier_3 must add RunPromptShield over tier_2")
	}
	if !t3.RunContentSafety {
		t.Error("tier_3 must add RunContentSafety over tier_2")
	}
	if !t3.RunPostValidation {
		t.Error("tier_3 must add RunPostValidation over tier_2")
	}
}

// BenchmarkPipelineFor measures the overhead of PipelineFor (should be O(1) switch).
func BenchmarkPipelineFor(b *testing.B) {
	tiers := []string{"tier_1", "tier_2", "tier_3"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PipelineFor(tiers[i%3])
	}
}
