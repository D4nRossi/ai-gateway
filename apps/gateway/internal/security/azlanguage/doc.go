// Package azlanguage wraps the Azure AI Language Service "Analyze Text" API
// for PII (Personally Identifiable Information) detection. It complements the
// regex-based masker in internal/security/masking by catching entities that
// have no fixed pattern — proper names, addresses, dates in free text, and
// foreign identifiers.
//
// Pipeline contract (ADR-0019):
//
//   - Tier 1: not invoked (regex local only).
//   - Tier 2: invoked, fail-open (Language down -> log warn, request continues
//     with whatever the regex pass already produced).
//   - Tier 3: invoked, fail-closed (Language down -> 503 with audit event).
//
// The client substitutes detected entities in-place with placeholders shaped
// like [CATEGORY_REDACTED] (e.g. [PERSON_REDACTED], [BRCPFNUMBER_REDACTED]).
// It does NOT use the redactedText returned by the API (which uses asterisks)
// because we want named placeholders for consistency with the regex masker.
//
// References:
//   - ADR-0019 — Azure Language PII como camada complementar
//   - SPEC.md §10 — PII masking specification
//   - https://learn.microsoft.com/azure/ai-services/language-service/personally-identifiable-information/overview
package azlanguage
