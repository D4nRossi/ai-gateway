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
// Tokens with any byte outside the printable ASCII range (0x21–0x7E) are
// rejected by returning the empty string. Callers MUST treat "" as an
// authentication miss and respond 401 without touching downstream stores.
//
// Reasoning: HTTP/1.1 (RFC 7230) defines header field values as ISO-8859-1
// after encoding, so a token containing UTF-8 multibyte chars (e.g. "ç", "ã")
// can be transmitted as either the original UTF-8 bytes or transliterated
// latin-1 bytes by the client. Postgres rejects latin-1 bytes on UTF-8 text
// columns with SQLSTATE 22021, surfacing as a 500 to the consumer. Enforcing
// printable-ASCII at the prefix boundary keeps tokens portable and prevents
// that storage-layer failure from leaking through the auth layer.
//
// References:
//   - SPEC.md §9.1 step 4b — "Lookup AppPolicy by key_prefix"
//   - CLAUDE.md §1.4 — only key_prefix may be logged, never the full token
//   - RFC 7230 §3.2.4 — Field Value Components
func ExtractPrefix(token string) string {
	for i := 0; i < len(token); i++ {
		// Reject control chars and any byte >= 0x80 (non-ASCII).
		// Tabs/spaces are control chars here too — Bearer tokens never contain them.
		if token[i] < 0x21 || token[i] > 0x7E {
			return ""
		}
	}

	// SplitN with n=3 gives at most 3 parts; any additional underscores land in parts[2].
	parts := strings.SplitN(token, "_", 3)
	if len(parts) < 3 {
		// Token has fewer than 2 underscores; return as-is so Lookup returns a miss.
		return token
	}
	return parts[0] + "_" + parts[1]
}
