package masking

import (
	"regexp"
	"strings"
)

// detector holds a compiled pattern and the logic to validate and tag a match.
type detector struct {
	category string
	re       *regexp.Regexp
	validate func(s string) bool // nil means accept all regex matches
}

// match records a single detected sensitive region.
type match struct {
	start    int
	end      int
	category string
}

// allDetectors returns all detectors in priority order (longest-match wins;
// ties broken by this order: CARD > CNPJ > CPF > PHONE > CEP > EMAIL).
//
// References:
//   - SPEC.md §10.1 — detector categories and replacements
//   - SPEC.md §10.3 — overlap resolution rules
func allDetectors() []detector {
	return []detector{
		cardDetector(),
		cnpjDetector(),
		cpfDetector(),
		phoneDetector(),
		cepDetector(),
		emailDetector(),
	}
}

// tier1Detectors returns only the detectors active for Tier 1 (highest-risk only).
//
// References:
//   - SPEC.md §10.2 — Tier 1 runs only BR_CPF and PCI_CARD
func tier1Detectors() []detector {
	return []detector{
		cardDetector(),
		cpfDetector(),
	}
}

// ─── PCI_CARD ────────────────────────────────────────────────────────────────

// cardRe matches 13–19 consecutive digits, optionally separated by spaces or
// hyphens in common formatting patterns.
var cardRe = regexp.MustCompile(`\b(?:\d[ -]?){12,18}\d\b`)

func cardDetector() detector {
	return detector{
		category: "PCI_CARD",
		re:       cardRe,
		validate: func(s string) bool {
			digits := stripNonDigits(s)
			if len(digits) < 13 || len(digits) > 19 {
				return false
			}
			return luhn(digits)
		},
	}
}

// ─── BR_CPF ──────────────────────────────────────────────────────────────────

// cpfRe matches the standard Brazilian CPF format: NNN.NNN.NNN-DD or 11 digits.
var cpfRe = regexp.MustCompile(`\b(\d{3}\.?\d{3}\.?\d{3}-?\d{2})\b`)

func cpfDetector() detector {
	return detector{
		category: "BR_CPF",
		re:       cpfRe,
		validate: func(s string) bool {
			digits := stripNonDigits(s)
			return len(digits) == 11 && validCPF(digits)
		},
	}
}

// validCPF implements the Brazilian mod-11 CPF checksum algorithm.
//
// References:
//   - SPEC.md §10.6 — CPF/CNPJ checksum
func validCPF(d string) bool {
	if len(d) != 11 {
		return false
	}
	// Reject trivially invalid sequences (all same digit).
	if allSame(d) {
		return false
	}
	return cpfCheck(d, 10) && cpfCheck(d, 11)
}

// cpfCheck validates one check digit. mul is the initial multiplier (10 or 11).
func cpfCheck(d string, mul int) bool {
	sum := 0
	for i := 0; i < mul-1; i++ {
		sum += int(d[i]-'0') * (mul - i)
	}
	rem := (sum * 10) % 11
	if rem == 10 || rem == 11 {
		rem = 0
	}
	return rem == int(d[mul-1]-'0')
}

// ─── BR_CNPJ ─────────────────────────────────────────────────────────────────

// cnpjRe matches the standard Brazilian CNPJ format: NN.NNN.NNN/NNNN-DD or 14 digits.
var cnpjRe = regexp.MustCompile(`\b(\d{2}\.?\d{3}\.?\d{3}/?\.?\d{4}-?\d{2})\b`)

func cnpjDetector() detector {
	return detector{
		category: "BR_CNPJ",
		re:       cnpjRe,
		validate: func(s string) bool {
			digits := stripNonDigits(s)
			return len(digits) == 14 && validCNPJ(digits)
		},
	}
}

// validCNPJ implements the Brazilian mod-11 CNPJ checksum algorithm.
//
// References:
//   - SPEC.md §10.6
func validCNPJ(d string) bool {
	if len(d) != 14 {
		return false
	}
	if allSame(d) {
		return false
	}
	mul1 := []int{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	mul2 := []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	return cnpjCheck(d, mul1, 12) && cnpjCheck(d, mul2, 13)
}

func cnpjCheck(d string, muls []int, checkIdx int) bool {
	sum := 0
	for i, m := range muls {
		sum += int(d[i]-'0') * m
	}
	rem := sum % 11
	expected := 0
	if rem >= 2 {
		expected = 11 - rem
	}
	return expected == int(d[checkIdx]-'0')
}

// ─── EMAIL ───────────────────────────────────────────────────────────────────

// emailRe is a pragmatic (not RFC-strict) email detector.
//
// References:
//   - SPEC.md §10.1
var emailRe = regexp.MustCompile(`\b[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}\b`)

func emailDetector() detector {
	return detector{
		category: "EMAIL",
		re:       emailRe,
	}
}

// ─── PHONE_BR ─────────────────────────────────────────────────────────────────

// phoneRe matches common Brazilian phone number formats (with or without country
// code, with or without DDD, fixed and mobile).
var phoneRe = regexp.MustCompile(
	`(?:(?:\+55|0055)[ -]?)?(?:\(?\d{2}\)?[ -]?)(?:9[ -]?\d{4}|\d{4})[ -]?\d{4}`,
)

func phoneDetector() detector {
	return detector{
		category: "PHONE_BR",
		re:       phoneRe,
	}
}

// ─── CEP_BR ──────────────────────────────────────────────────────────────────

// cepRe matches Brazilian postal codes (CEP) in both formats: NNNNN-NNN or NNNNNNNN.
var cepRe = regexp.MustCompile(`\b\d{5}-?\d{3}\b`)

func cepDetector() detector {
	return detector{
		category: "CEP_BR",
		re:       cepRe,
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// stripNonDigits returns s with all non-ASCII-digit characters removed.
func stripNonDigits(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// allSame reports whether all characters in s are identical.
func allSame(s string) bool {
	if len(s) == 0 {
		return true
	}
	for i := 1; i < len(s); i++ {
		if s[i] != s[0] {
			return false
		}
	}
	return true
}
