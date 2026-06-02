package masking

import (
	"testing"
)

// ── CPF checksum (mod-11) ─────────────────────────────────────────────────────

func TestValidCPF_Valid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
	}{
		{"standard formatted", "529.982.247-25"},
		{"digits only", "52998224725"},
		{"real-world vector 1", "111.444.777-35"},
		// Sequential digits still pass CPF checksum (sequential ≠ invalid per algorithm).
		{"sequential with valid checksum", "123.456.789-09"},
		// Near-zero base with valid checksum.
		{"near-zero base", "000.000.001-91"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			digits := stripNonDigits(tc.input)
			if !validCPF(digits) {
				t.Errorf("validCPF(%q) = false; want true", tc.input)
			}
		})
	}
}

func TestValidCPF_Invalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
	}{
		{"single digit off (last)", "529.982.247-26"},
		{"single digit off (first)", "629.982.247-25"},
		{"wrong check digit 1", "111.444.777-36"},
		{"wrong last digit sequential", "123.456.789-10"},
		{"too short", "1234567"},
		{"too long", "123456789012"},
		{"all zeros", "000.000.000-00"},
		{"all ones", "111.111.111-11"},
		{"all nines", "999.999.999-99"},
		{"all same (fives)", "555.555.555-55"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			digits := stripNonDigits(tc.input)
			if validCPF(digits) {
				t.Errorf("validCPF(%q) = true; want false", tc.input)
			}
		})
	}
}

func TestValidCPF_AllSameDigitRejected(t *testing.T) {
	t.Parallel()
	// Every all-same-digit CPF must be rejected regardless of checksum.
	for d := '0'; d <= '9'; d++ {
		digits := string([]byte{byte(d), byte(d), byte(d), byte(d), byte(d),
			byte(d), byte(d), byte(d), byte(d), byte(d), byte(d)})
		if validCPF(digits) {
			t.Errorf("validCPF(%q) = true; all-same sequences must be rejected", digits)
		}
	}
}

func TestValidCPF_WrongLength(t *testing.T) {
	t.Parallel()
	cases := []string{"", "1", "1234567890", "123456789012"}
	for _, c := range cases {
		c := c
		t.Run(c, func(t *testing.T) {
			t.Parallel()
			if validCPF(c) {
				t.Errorf("validCPF(%q) = true; wrong-length must be rejected", c)
			}
		})
	}
}

// ── CNPJ checksum (mod-11) ────────────────────────────────────────────────────

func TestValidCNPJ_Valid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
	}{
		{"formatted", "11.222.333/0001-81"},
		{"digits only", "11222333000181"},
		{"real-world vector 1", "45.997.418/0001-53"},
		{"real-world vector 2", "62.173.620/0001-80"},
		{"real-world vector 3", "00.000.000/0001-91"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			digits := stripNonDigits(tc.input)
			if !validCNPJ(digits) {
				t.Errorf("validCNPJ(%q) = false; want true", tc.input)
			}
		})
	}
}

func TestValidCNPJ_Invalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
	}{
		{"wrong last digit", "11.222.333/0001-82"},
		{"all zeros", "00.000.000/0000-00"},
		{"all ones", "11.111.111/1111-11"},
		{"too short", "1122233300018"},
		{"too long", "112223330001810"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			digits := stripNonDigits(tc.input)
			if validCNPJ(digits) {
				t.Errorf("validCNPJ(%q) = true; want false", tc.input)
			}
		})
	}
}

func TestValidCNPJ_AllSameDigitRejected(t *testing.T) {
	t.Parallel()
	for d := '1'; d <= '9'; d++ {
		digits := string([]byte{byte(d), byte(d), byte(d), byte(d), byte(d),
			byte(d), byte(d), byte(d), byte(d), byte(d), byte(d), byte(d), byte(d), byte(d)})
		if validCNPJ(digits) {
			t.Errorf("validCNPJ(%q) = true; all-same sequences must be rejected", digits)
		}
	}
}

// ── stripNonDigits ────────────────────────────────────────────────────────────

func TestStripNonDigits(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		want  string
	}{
		{"123-456.789", "123456789"},
		{"abc123def456", "123456"},
		{"", ""},
		{"nenhum digito", ""},
		{"000.000.000-00", "00000000000"},
		{"  1 2 3  ", "123"},
		{"こんにちは123", "123"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := stripNonDigits(tc.input)
			if got != tc.want {
				t.Errorf("stripNonDigits(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ── allSame ───────────────────────────────────────────────────────────────────

func TestAllSame(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		want  bool
	}{
		{"", true},
		{"a", true},
		{"aaa", true},
		{"111", true},
		{"ab", false},
		{"aab", false},
		{"aba", false},
		{"123", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := allSame(tc.input)
			if got != tc.want {
				t.Errorf("allSame(%q) = %v; want %v", tc.input, got, tc.want)
			}
		})
	}
}

// ── Masker benchmarks ─────────────────────────────────────────────────────────

func BenchmarkMasker_Mask_NoDetections(b *testing.B) {
	m := NewMasker("tier_2")
	text := "Olá, preciso de ajuda com meu pedido de reembolso referente ao produto comprado no dia 12 de novembro."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Mask(text)
	}
}

func BenchmarkMasker_Mask_CPFOnly(b *testing.B) {
	m := NewMasker("tier_1")
	text := "Meu CPF é 529.982.247-25 e preciso de suporte."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Mask(text)
	}
}

func BenchmarkMasker_Mask_MultiplePII(b *testing.B) {
	m := NewMasker("tier_2")
	text := "CPF: 529.982.247-25, CNPJ: 11.222.333/0001-81, email: user@example.com, fone: (11) 98765-4321, CEP: 01310-100, cartão: 4111 1111 1111 1111."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Mask(text)
	}
}

func BenchmarkMasker_Mask_LongText(b *testing.B) {
	m := NewMasker("tier_2")
	// ~2 KB of text with one CPF embedded.
	base := "Lorem ipsum dolor sit amet, consectetur adipiscing elit. "
	text := ""
	for len(text) < 2000 {
		text += base
	}
	text += " CPF: 529.982.247-25"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Mask(text)
	}
}
