// Package tiers defines the security pipeline configuration for each application tier.
//
// Each tier activates a different combination of guardrails (SPEC §5.3):
//   - Tier 1: local PII masking (CPF + card only), fail-open
//   - Tier 2: full PII masking + remote PII (Language) + local injection, fail-open
//   - Tier 3: all detectors + remote PII + Azure Content Safety + post-validation, fail-closed
//
// References:
//   - SPEC.md §5.3 — tier pipeline table
//   - SPEC.md §11.4 — fail-mode semantics
//   - ADR-0019 — Azure Language PII como camada complementar
package tiers

// Pipeline describes which guardrails are active for a request.
//
// References:
//   - SPEC.md §5.3
//   - ADR-0019 (RunRemotePII)
type Pipeline struct {
	// RunLocalMasking enables PII/PCI redaction on prompt messages via regex.
	RunLocalMasking bool

	// RunRemotePII enables the Azure AI Language PII detection step. Runs
	// AFTER RunLocalMasking on the already-masked body so the cloud only sees
	// what the regex pass left intact (ADR-0019, sequential pipeline).
	RunRemotePII bool

	// RunLocalInjection enables the keyword-based injection heuristic.
	RunLocalInjection bool

	// RunPromptShield enables the Azure Content Safety Prompt Shield API call.
	RunPromptShield bool

	// RunContentSafety enables the Azure Content Safety Text Analyze API call.
	RunContentSafety bool

	// RunPostValidation enables post-generation output checking (Tier 3 only).
	RunPostValidation bool

	// FailMode is "open" (continue on external service error) or
	// "closed" (block request on external service error).
	FailMode string
}

// PipelineFor returns the Pipeline for the given tier identifier.
// Unknown tiers return a maximally restrictive pipeline (fail-closed, all checks).
//
// References:
//   - SPEC.md §5.3 — tier pipeline table
//   - SPEC.md §11.4 — fail-mode semantics
//   - ADR-0019 — Azure Language PII tier matrix
func PipelineFor(tier string) Pipeline {
	switch tier {
	case "tier_1":
		return Pipeline{
			RunLocalMasking:   true,
			RunRemotePII:      false,
			RunLocalInjection: false,
			RunPromptShield:   false,
			RunContentSafety:  false,
			RunPostValidation: false,
			FailMode:          "open",
		}
	case "tier_2":
		return Pipeline{
			RunLocalMasking:   true,
			RunRemotePII:      true,
			RunLocalInjection: true,
			RunPromptShield:   false,
			RunContentSafety:  false,
			RunPostValidation: false,
			FailMode:          "open",
		}
	case "tier_3":
		return Pipeline{
			RunLocalMasking:   true,
			RunRemotePII:      true,
			RunLocalInjection: true,
			RunPromptShield:   true,
			RunContentSafety:  true,
			RunPostValidation: true,
			FailMode:          "closed",
		}
	default:
		// Unknown tier: fail-closed with all checks active.
		return Pipeline{
			RunLocalMasking:   true,
			RunRemotePII:      true,
			RunLocalInjection: true,
			RunPromptShield:   true,
			RunContentSafety:  true,
			RunPostValidation: true,
			FailMode:          "closed",
		}
	}
}
