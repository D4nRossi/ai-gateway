// Package promptshield provides prompt injection detection for the AI Gateway.
//
// It exposes two implementations:
//
//   - [LocalScanner]: keyword-based heuristic used for Tier 2 and as a fallback
//     when Azure Content Safety is not configured.
//   - [Client]: HTTP client for Azure Content Safety Prompt Shield and Text Analyze
//     APIs, used for Tier 3.
//
// Fail-mode semantics (SPEC §11.4):
//   - Tier 1 / Tier 2 (fail-open): Azure CS unreachable → continue with warn log.
//   - Tier 3 (fail-closed): Azure CS unreachable → return error, block request.
//
// References:
//   - SPEC.md §11 — prompt shield specification
//   - https://learn.microsoft.com/en-us/azure/ai-services/content-safety/quickstart-prompt-shield
//   - https://learn.microsoft.com/en-us/azure/ai-services/content-safety/quickstart-text
package promptshield
