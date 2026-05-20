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
