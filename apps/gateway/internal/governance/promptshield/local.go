package promptshield

import (
	"regexp"
	"strings"
)

// injectionPattern is a compiled regex for each local injection heuristic keyword.
// Word-boundary anchors are added where the phrase starts/ends with a word character.
//
// References:
//   - SPEC.md §11.3 — local heuristics fallback
var injectionPatterns []*regexp.Regexp

func init() {
	keywords := []string{
		`ignore previous instructions`,
		`ignore all previous`,
		`disregard the above`,
		`system prompt`,
		`your instructions`,
		`your system message`,
		`pretend to be`,
		`act as`,
		`you are now`,
		`you are DAN`,
		`developer mode`,
		`<\|im_start\|>`,
		`<\|im_end\|>`,
		`<\|system\|>`,
	}
	for _, kw := range keywords {
		// Wrap phrases that start/end with word characters in \b anchors.
		pat := `(?i)` + kw
		if len(kw) > 0 && isWordChar(rune(kw[0])) {
			pat = `(?i)\b` + kw
		}
		if len(kw) > 0 && isWordChar(rune(kw[len(kw)-1])) {
			pat = pat + `\b`
		}
		injectionPatterns = append(injectionPatterns, regexp.MustCompile(pat))
	}
}

// LocalScanner performs keyword-based prompt injection detection without any
// external API call. It is used for Tier 2 and as a fallback for Tier 3.
//
// References:
//   - SPEC.md §11.3 — local heuristics fallback
type LocalScanner struct{}

// NewLocalScanner returns a ready-to-use LocalScanner.
func NewLocalScanner() *LocalScanner {
	return &LocalScanner{}
}

// DetectInjection reports whether text contains any known injection pattern.
// Returns true if a potential injection was detected.
//
// References:
//   - SPEC.md §11.3
func (s *LocalScanner) DetectInjection(text string) bool {
	lower := strings.ToLower(text)
	for _, pat := range injectionPatterns {
		if pat.MatchString(lower) {
			return true
		}
	}
	return false
}

// isWordChar reports whether r is a word character (letter, digit, or underscore).
func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}
