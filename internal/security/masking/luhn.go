package masking

// luhn reports whether the digit string s passes the Luhn (mod-10) checksum.
// s must contain only ASCII digit characters '0'–'9'; any other input returns false.
//
// Algorithm: starting from the rightmost digit and moving left, double every
// second digit. If the doubled value exceeds 9, subtract 9 (equivalent to
// summing the two resulting digits). Sum all values; valid if total % 10 == 0.
//
// References:
//   - SPEC.md §10.5 — Luhn algorithm specification
//   - https://en.wikipedia.org/wiki/Luhn_algorithm
func luhn(s string) bool {
	if len(s) < 2 {
		return false
	}
	sum := 0
	double := false // toggles for every second digit from the right
	for i := len(s) - 1; i >= 0; i-- {
		ch := s[i]
		if ch < '0' || ch > '9' {
			return false
		}
		digit := int(ch - '0')
		if double {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
		double = !double
	}
	return sum%10 == 0
}
