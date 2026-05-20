package masking

import (
	"strings"
	"testing"
)

// TestMasker_NoDetections confirms that text without sensitive data passes through unchanged.
func TestMasker_NoDetections(t *testing.T) {
	t.Parallel()
	m := NewMasker("tier_2")
	r := m.Mask("Hello, this is a completely safe sentence.")
	if r.Text != "Hello, this is a completely safe sentence." {
		t.Errorf("unexpected modification: %q", r.Text)
	}
	if r.TotalReplacements != 0 {
		t.Errorf("TotalReplacements = %d; want 0", r.TotalReplacements)
	}
	if len(r.Categories) != 0 {
		t.Errorf("Categories non-empty: %v", r.Categories)
	}
}

// TestMasker_CPF verifies that a valid CPF is detected and replaced.
//
// References:
//   - SPEC.md §10.1 — BR_CPF detector
func TestMasker_CPF(t *testing.T) {
	t.Parallel()
	m := NewMasker("tier_1")
	r := m.Mask("Meu CPF é 529.982.247-25 por favor.")
	if !strings.Contains(r.Text, "[BR_CPF_REDACTED]") {
		t.Errorf("expected [BR_CPF_REDACTED] in output, got: %q", r.Text)
	}
	if r.Categories["BR_CPF"] != 1 {
		t.Errorf("Categories[BR_CPF] = %d; want 1", r.Categories["BR_CPF"])
	}
	if r.TotalReplacements != 1 {
		t.Errorf("TotalReplacements = %d; want 1", r.TotalReplacements)
	}
}

// TestMasker_CardNumber verifies that a valid Luhn card number is detected.
//
// References:
//   - SPEC.md §10.1 — PCI_CARD detector (requires Luhn validation)
func TestMasker_CardNumber(t *testing.T) {
	t.Parallel()
	m := NewMasker("tier_1")
	r := m.Mask("My card is 4111111111111111 thanks.")
	if !strings.Contains(r.Text, "[PCI_CARD_REDACTED]") {
		t.Errorf("expected [PCI_CARD_REDACTED] in output, got: %q", r.Text)
	}
	if r.Categories["PCI_CARD"] != 1 {
		t.Errorf("Categories[PCI_CARD] = %d; want 1", r.Categories["PCI_CARD"])
	}
}

// TestMasker_InvalidLuhnNotMasked ensures that digit sequences that fail Luhn are not redacted.
func TestMasker_InvalidLuhnNotMasked(t *testing.T) {
	t.Parallel()
	m := NewMasker("tier_2")
	// 4111111111111112 is invalid Luhn (last digit changed).
	r := m.Mask("number 4111111111111112 here")
	if strings.Contains(r.Text, "[PCI_CARD_REDACTED]") {
		t.Errorf("should not redact invalid Luhn number, got: %q", r.Text)
	}
}

// TestMasker_EmailTier2Only verifies that EMAIL is masked in Tier 2 but not Tier 1.
//
// References:
//   - SPEC.md §10.2 — Tier 1 detects only CPF and PCI_CARD
func TestMasker_EmailTier2Only(t *testing.T) {
	t.Parallel()
	text := "Contact me at user@example.com please."

	tier1 := NewMasker("tier_1")
	r1 := tier1.Mask(text)
	if strings.Contains(r1.Text, "[EMAIL_REDACTED]") {
		t.Errorf("Tier 1 should not mask email, got: %q", r1.Text)
	}

	tier2 := NewMasker("tier_2")
	r2 := tier2.Mask(text)
	if !strings.Contains(r2.Text, "[EMAIL_REDACTED]") {
		t.Errorf("Tier 2 should mask email, got: %q", r2.Text)
	}
}

// TestMasker_MultipleDetections verifies that multiple different categories are all masked.
func TestMasker_MultipleDetections(t *testing.T) {
	t.Parallel()
	m := NewMasker("tier_2")
	text := "CPF 529.982.247-25 e email test@example.com e CEP 01310-100."
	r := m.Mask(text)

	if !strings.Contains(r.Text, "[BR_CPF_REDACTED]") {
		t.Errorf("expected CPF redacted in %q", r.Text)
	}
	if !strings.Contains(r.Text, "[EMAIL_REDACTED]") {
		t.Errorf("expected EMAIL redacted in %q", r.Text)
	}
	if !strings.Contains(r.Text, "[CEP_REDACTED]") {
		t.Errorf("expected CEP redacted in %q", r.Text)
	}
	if r.TotalReplacements < 3 {
		t.Errorf("TotalReplacements = %d; want >= 3", r.TotalReplacements)
	}
}

// TestMasker_OverlapResolution verifies that when CPF digits and CARD could overlap,
// the longer match wins (CARD > CPF by both length and priority).
//
// References:
//   - SPEC.md §10.3 — overlap resolution: longer wins; CARD > CPF on tie
func TestMasker_OverlapResolution(t *testing.T) {
	t.Parallel()
	m := NewMasker("tier_2")
	// A Luhn-valid 16-digit number that also looks like it could match a sub-pattern.
	// We verify only one replacement tag appears (not two overlapping tags).
	text := "card: 4111111111111111 end"
	r := m.Mask(text)

	count := strings.Count(r.Text, "_REDACTED]")
	if count != 1 {
		t.Errorf("expected exactly 1 redaction, got %d in %q", count, r.Text)
	}
	if !strings.Contains(r.Text, "[PCI_CARD_REDACTED]") {
		t.Errorf("expected PCI_CARD to win, got: %q", r.Text)
	}
}
