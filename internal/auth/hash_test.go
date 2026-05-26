package auth

import "testing"

// TestExtractPrefix verifies that ExtractPrefix correctly isolates the key_prefix
// segment from a bearer token, and degrades gracefully for malformed inputs.
//
// References:
//   - SPEC.md §9.1 step 4b — "Lookup AppPolicy by key_prefix"
func TestExtractPrefix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		token string
		want  string
	}{
		{
			name:  "standard three-segment token",
			token: "gwk_leve_realkey_12345",
			want:  "gwk_leve",
		},
		{
			name:  "token with only one extra segment",
			token: "gwk_med_realkey67890",
			want:  "gwk_med",
		},
		{
			name:  "long secret segment with underscores",
			token: "gwk_sens_secret_part_one_two",
			want:  "gwk_sens",
		},
		{
			name:  "only two segments — returned as-is (no third part)",
			token: "gwk_leve",
			want:  "gwk_leve",
		},
		{
			name:  "one segment — returned as-is",
			token: "gwk",
			want:  "gwk",
		},
		{
			name:  "empty string — returned as-is",
			token: "",
			want:  "",
		},
		{
			// UTF-8 multibyte (e.g. "ç" = 0xC3 0xA7) must be rejected — the prefix
			// column is UTF-8 text but HTTP headers may transliterate to latin-1,
			// triggering Postgres SQLSTATE 22021.
			name:  "token with UTF-8 multibyte char — rejected",
			token: "gwk_aplicação_secret",
			want:  "",
		},
		{
			// Latin-1 single-byte representation of "çã" — bytes 0xE7 0xE3.
			// These appear in the wild when a client encodes the Authorization
			// header with ISO-8859-1 instead of UTF-8 (RFC 7230 allows both).
			name:  "token with raw latin-1 bytes — rejected",
			token: "gwk_aplica\xe7\xe3_secret",
			want:  "",
		},
		{
			name:  "token with embedded space — rejected",
			token: "gwk_app demo_secret",
			want:  "",
		},
		{
			name:  "token with tab — rejected",
			token: "gwk_app\tdemo_secret",
			want:  "",
		},
		{
			name:  "token with high-bit byte 0x80 — rejected",
			token: "gwk_a\x80b_secret",
			want:  "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractPrefix(tc.token)
			if got != tc.want {
				t.Errorf("ExtractPrefix(%q) = %q; want %q", tc.token, got, tc.want)
			}
		})
	}
}
