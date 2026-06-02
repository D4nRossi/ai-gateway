package masking

import "testing"

// TestLuhn verifies the standard mod-10 checksum used to validate card numbers.
//
// References:
//   - SPEC.md §10.5 — Luhn algorithm specification
//   - https://en.wikipedia.org/wiki/Luhn_algorithm
func TestLuhn(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  bool
	}{
		// Valid card numbers (from public Luhn test vectors).
		{name: "Visa test card", input: "4111111111111111", want: true},
		{name: "Mastercard test card", input: "5500005555555559", want: true},
		{name: "Amex test card (15 digits)", input: "371449635398431", want: true},
		{name: "Discover test card", input: "6011111111111117", want: true},

		// Invalid numbers.
		{name: "single digit off", input: "4111111111111112", want: false},
		{name: "sequential digits invalid", input: "1234567890123456", want: false},

		// Edge cases — implementation requires len >= 2.
		{name: "single digit zero rejected (len < 2)", input: "0", want: false},
		{name: "empty string rejected (len < 2)", input: "", want: false},
		// All-zeros passes Luhn mathematically: sum=0, 0%10=0 → valid.
		// Real card processors reject this via issuer checks; the gateway only runs Luhn.
		{name: "all zeros passes luhn", input: "0000000000000000", want: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := luhn(tc.input)
			if got != tc.want {
				t.Errorf("luhn(%q) = %v; want %v", tc.input, got, tc.want)
			}
		})
	}
}
