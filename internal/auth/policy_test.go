package auth

import (
	"testing"

	"github.com/D4nRossi/ai-gateway/internal/config"
)

// TestPolicyStore_Lookup verifies that the in-memory PolicyStore correctly
// returns policies for known prefixes and misses for unknown ones.
//
// References:
//   - SPEC.md §5.2 — PolicyStore interface
func TestPolicyStore_Lookup(t *testing.T) {
	t.Parallel()

	apps := []config.ApplicationConfig{
		{
			Name:             "AppLeve",
			KeyPrefix:        "gwk_leve",
			KeyHash:          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Tier:             "tier_1",
			AllowedModels:    []string{"gpt-4.1-nano"},
			StreamingAllowed: true,
			MaxRPM:           60,
			MonthlyBudgetBRL: 100.00,
		},
		{
			Name:             "AppMedio",
			KeyPrefix:        "gwk_med",
			KeyHash:          "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Tier:             "tier_2",
			AllowedModels:    []string{"gpt-4.1-mini", "gpt-4.1-nano"},
			StreamingAllowed: true,
			MaxRPM:           120,
			MonthlyBudgetBRL: 500.00,
		},
	}

	store := NewPolicyStore(apps)

	t.Run("known prefix returns policy", func(t *testing.T) {
		t.Parallel()
		p, ok := store.Lookup("gwk_leve")
		if !ok {
			t.Fatal("expected ok=true for gwk_leve, got false")
		}
		if p.Name != "AppLeve" {
			t.Errorf("Name = %q; want %q", p.Name, "AppLeve")
		}
		if p.Tier != "tier_1" {
			t.Errorf("Tier = %q; want %q", p.Tier, "tier_1")
		}
		if !p.StreamingAllowed {
			t.Error("StreamingAllowed = false; want true")
		}
		if p.MonthlyBudgetBRL != 100.00 {
			t.Errorf("MonthlyBudgetBRL = %v; want 100.00", p.MonthlyBudgetBRL)
		}
	})

	t.Run("second app returns correct policy", func(t *testing.T) {
		t.Parallel()
		p, ok := store.Lookup("gwk_med")
		if !ok {
			t.Fatal("expected ok=true for gwk_med, got false")
		}
		if p.Name != "AppMedio" {
			t.Errorf("Name = %q; want %q", p.Name, "AppMedio")
		}
		if len(p.AllowedModels) != 2 {
			t.Errorf("AllowedModels len = %d; want 2", len(p.AllowedModels))
		}
	})

	t.Run("unknown prefix returns miss", func(t *testing.T) {
		t.Parallel()
		_, ok := store.Lookup("gwk_unknown")
		if ok {
			t.Error("expected ok=false for unknown prefix, got true")
		}
	})

	t.Run("empty prefix returns miss", func(t *testing.T) {
		t.Parallel()
		_, ok := store.Lookup("")
		if ok {
			t.Error("expected ok=false for empty prefix, got true")
		}
	})
}
