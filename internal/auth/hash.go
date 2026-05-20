package auth

import "strings"

// ExtractPrefix returns the key_prefix portion of a bearer token.
//
// Bearer tokens follow the convention "gwk_<prefix_segment>_<secret…>",
// so the prefix is the first two underscore-delimited segments joined:
//
//	"gwk_leve_realkey_xyz" → "gwk_leve"
//	"gwk_med_realkey_67890" → "gwk_med"
//	"gwk_sens_secretpart"  → "gwk_sens"
//
// This prefix is used for O(1) lookup in PolicyStore before the more
// expensive constant-time hash comparison.
//
// Reasoning: indexing by prefix avoids iterating all policies on every
// request. The prefix is not secret — it may appear in logs. Only the
// full token value is kept confidential.
//
// References:
//   - SPEC.md §9.1 step 4b — "Lookup AppPolicy by key_prefix"
//   - CLAUDE.md §1.4 — only key_prefix may be logged, never the full token
func ExtractPrefix(token string) string {
	// SplitN with n=3 gives at most 3 parts; any additional underscores land in parts[2].
	parts := strings.SplitN(token, "_", 3)
	if len(parts) < 3 {
		// Token has fewer than 2 underscores; return as-is so Lookup returns a miss.
		return token
	}
	return parts[0] + "_" + parts[1]
}
