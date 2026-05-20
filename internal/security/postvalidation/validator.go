// Package postvalidation implements Tier 3 output content checking.
//
// After the provider returns a response, Tier 3 runs the assistant's reply
// through the local injection heuristic to catch any jailbreak content that
// might have leaked into the output.
//
// References:
//   - SPEC.md §9.1 step 10 — post-validation (Tier 3 only)
package postvalidation

import "github.com/D4nRossi/ai-gateway/internal/security/promptshield"

// Validator performs post-generation output checks for Tier 3 requests.
type Validator struct {
	scanner *promptshield.LocalScanner
}

// New returns a Validator backed by the local injection scanner.
//
// References:
//   - SPEC.md §9.1 step 10
func New() *Validator {
	return &Validator{scanner: promptshield.NewLocalScanner()}
}

// Check reports whether the assistant output triggers any local injection
// heuristic. Returns true if the content should be blocked.
//
// Reasoning: a Tier 3 response that contains jailbreak-like patterns in the
// model's output suggests the upstream model was compromised; blocking at this
// point prevents leaking the unsafe output to the consumer.
//
// References:
//   - SPEC.md §9.1 step 10
func (v *Validator) Check(content string) bool {
	return v.scanner.DetectInjection(content)
}
