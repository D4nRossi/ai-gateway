// Package masking provides PII/PCI detection and redaction for prompt content.
//
// Detectors are selected based on the application's tier (SPEC §10.2):
//   - Tier 1: BR_CPF and PCI_CARD only (highest-risk PII/PCI)
//   - Tier 2+: all detectors (CPF, CNPJ, CARD, EMAIL, PHONE_BR, CEP_BR)
//
// Overlap resolution: when two detectors match overlapping regions, the longer
// match wins. Equal-length ties are broken by detector priority order
// (CARD > CNPJ > CPF > PHONE > CEP > EMAIL) — see SPEC §10.3.
//
// References:
//   - SPEC.md §10 — masking specification
package masking

import (
	"sort"
	"strings"
)

// replacementTag maps each detector category to its redaction placeholder.
var replacementTag = map[string]string{
	"PCI_CARD": "[PCI_CARD_REDACTED]",
	"BR_CPF":   "[BR_CPF_REDACTED]",
	"BR_CNPJ":  "[BR_CNPJ_REDACTED]",
	"EMAIL":    "[EMAIL_REDACTED]",
	"PHONE_BR": "[PHONE_REDACTED]",
	"CEP_BR":   "[CEP_REDACTED]",
}

// MaskResult holds the masked text and detection statistics.
type MaskResult struct {
	// Text is the content with sensitive regions replaced by category tags.
	Text string

	// Categories maps each detected category to the number of replacements made.
	Categories map[string]int

	// TotalReplacements is the sum of all per-category counts.
	TotalReplacements int
}

// Masker orchestrates PII/PCI detection and redaction.
// Create with NewMasker; safe for concurrent use after construction.
type Masker struct {
	detectors []detector
}

// NewMasker returns a Masker configured for the given tier.
//
// References:
//   - SPEC.md §10.2 — tier-specific detector selection
func NewMasker(tier string) *Masker {
	var dets []detector
	if tier == "tier_1" {
		dets = tier1Detectors()
	} else {
		dets = allDetectors()
	}
	return &Masker{detectors: dets}
}

// Mask scans text, redacts all detected sensitive regions, and returns the
// masked result along with per-category counts.
//
// Reasoning: overlap resolution is done by collecting all candidate matches,
// sorting by position then length (longest wins), and consuming them left-to-
// right while skipping any match whose range overlaps an already-consumed region.
//
// References:
//   - SPEC.md §10.1 — detector categories and replacements
//   - SPEC.md §10.3 — overlap resolution
func (m *Masker) Mask(text string) MaskResult {
	// Collect all candidate matches across all detectors.
	var candidates []match
	for _, det := range m.detectors {
		locs := det.re.FindAllStringIndex(text, -1)
		for _, loc := range locs {
			raw := text[loc[0]:loc[1]]
			if det.validate != nil && !det.validate(raw) {
				continue
			}
			candidates = append(candidates, match{
				start:    loc[0],
				end:      loc[1],
				category: det.category,
			})
		}
	}

	if len(candidates) == 0 {
		return MaskResult{Text: text}
	}

	// Sort: primary ascending start; secondary descending length (longer wins);
	// tertiary by detector priority order defined in priorityOf.
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.start != b.start {
			return a.start < b.start
		}
		lenA := a.end - a.start
		lenB := b.end - b.start
		if lenA != lenB {
			return lenA > lenB
		}
		return priorityOf(a.category) < priorityOf(b.category)
	})

	// Consume matches left-to-right, skipping overlaps.
	var buf strings.Builder
	cats := make(map[string]int)
	cursor := 0

	for _, m := range candidates {
		if m.start < cursor {
			continue // overlaps a previously consumed region
		}
		buf.WriteString(text[cursor:m.start])
		buf.WriteString(replacementTag[m.category])
		cats[m.category]++
		cursor = m.end
	}
	buf.WriteString(text[cursor:])

	total := 0
	for _, n := range cats {
		total += n
	}

	return MaskResult{
		Text:              buf.String(),
		Categories:        cats,
		TotalReplacements: total,
	}
}

// priorityOf returns the tie-breaking priority index for a category.
// Lower index = higher priority (wins ties in overlap resolution).
//
// References:
//   - SPEC.md §10.3 — "prefer order: CARD > CNPJ > CPF > PHONE > CEP > EMAIL"
func priorityOf(category string) int {
	order := []string{"PCI_CARD", "BR_CNPJ", "BR_CPF", "PHONE_BR", "CEP_BR", "EMAIL"}
	for i, c := range order {
		if c == category {
			return i
		}
	}
	return len(order)
}
