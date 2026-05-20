package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/D4nRossi/ai-gateway/internal/api/middleware"
	"github.com/D4nRossi/ai-gateway/internal/audit"
	"github.com/D4nRossi/ai-gateway/internal/auth"
	"github.com/D4nRossi/ai-gateway/internal/budget"
	"github.com/D4nRossi/ai-gateway/internal/config"
	"github.com/D4nRossi/ai-gateway/internal/observability"
	"github.com/D4nRossi/ai-gateway/internal/providers"
	"github.com/D4nRossi/ai-gateway/internal/security/masking"
	"github.com/D4nRossi/ai-gateway/internal/security/postvalidation"
	"github.com/D4nRossi/ai-gateway/internal/security/promptshield"
	"github.com/D4nRossi/ai-gateway/internal/tiers"
	"github.com/D4nRossi/ai-gateway/internal/usage"
)

// maxBodyBytes is the maximum allowed request body size (1 MiB).
// References: SPEC.md §14.3.
const maxBodyBytes = 1 << 20

// ChatDeps groups all dependencies for the Chat handler.
// Async writers and budget components are expressed as interfaces to allow
// unit testing without a live database connection (CLAUDE.md §14).
type ChatDeps struct {
	Provider     providers.Provider
	Config       *config.Config
	AuditWriter  audit.Emitter
	UsageWriter  usage.Emitter
	BudgetCheck  budget.PreChecker
	BudgetCount  budget.Recorder
	ShieldClient *promptshield.Client    // nil if azure_content_safety not configured
	Validator    *postvalidation.Validator
	Logger       *slog.Logger
}

// Chat handles POST /v1/chat/completions (streaming and non-streaming).
//
// References:
//   - SPEC.md §6.2, §6.3, §9.1, §9.2
func Chat(deps ChatDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		start := time.Now()

		rid, _ := ctx.Value(observability.RequestIDKey).(string)
		reqLogger := observability.LoggerFrom(ctx, deps.Logger)

		policy, ok := middleware.PolicyFrom(ctx)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
			return
		}

		// ── 1. Read + unmarshal body ──────────────────────────────────────────
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		var req providers.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			if strings.Contains(err.Error(), "http: request body too large") {
				writeJSONError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "")
			} else {
				writeJSONError(w, http.StatusBadRequest, "invalid_json", "")
			}
			return
		}

		// ── 2. Policy checks ──────────────────────────────────────────────────
		if !modelAllowed(req.Model, policy.AllowedModels) {
			deps.AuditWriter.Emit(audit.AuditEvent{
				RequestID: rid, ApplicationName: policy.Name,
				EventType: audit.EventModelBlocked, Severity: "warn",
				Metadata:  map[string]any{"requested_model": req.Model},
				CreatedAt: time.Now().UTC(),
			})
			writeJSONError(w, http.StatusForbidden, "model_not_allowed", "policy_error")
			return
		}

		if req.Stream && !policy.StreamingAllowed {
			writeJSONError(w, http.StatusForbidden, "streaming_not_allowed", "policy_error")
			return
		}

		// ── 3. Tier pipeline ──────────────────────────────────────────────────
		pipe := tiers.PipelineFor(policy.Tier)

		if pipe.RunLocalMasking {
			masker := masking.NewMasker(policy.Tier)
			totalCats := map[string]int{}
			totalReplace := 0
			for i, msg := range req.Messages {
				result := masker.Mask(msg.Content)
				req.Messages[i].Content = result.Text
				for cat, n := range result.Categories {
					totalCats[cat] += n
				}
				totalReplace += result.TotalReplacements
			}
			if totalReplace > 0 {
				deps.AuditWriter.Emit(audit.AuditEvent{
					RequestID: rid, ApplicationName: policy.Name,
					EventType: audit.EventPIIMasked, Severity: "info",
					Metadata: map[string]any{
						"categories":         totalCats,
						"total_replacements": totalReplace,
					},
					CreatedAt: time.Now().UTC(),
				})
			}
		}

		if pipe.RunLocalInjection {
			scanner := promptshield.NewLocalScanner()
			if scanner.DetectInjection(concatMessages(req.Messages)) {
				deps.AuditWriter.Emit(audit.AuditEvent{
					RequestID: rid, ApplicationName: policy.Name,
					EventType: audit.EventInjectionDetected, Severity: "warn",
					CreatedAt: time.Now().UTC(),
				})
				writeJSONError(w, http.StatusForbidden, "blocked_by_security", "security_error")
				return
			}
		}

		if pipe.RunPromptShield && deps.ShieldClient != nil {
			result, err := deps.ShieldClient.ShieldPrompt(ctx, concatMessages(req.Messages))
			if err != nil {
				if pipe.FailMode == "closed" {
					deps.AuditWriter.Emit(audit.AuditEvent{
						RequestID: rid, ApplicationName: policy.Name,
						EventType: audit.EventPromptShieldBlock, Severity: "error",
						Metadata:  map[string]any{"reason": "azure_cs_unavailable"},
						CreatedAt: time.Now().UTC(),
					})
					writeJSONError(w, http.StatusServiceUnavailable, "blocked_by_security", "security_error")
					return
				}
				reqLogger.Warn("prompt shield unavailable, failing open",
					"err", err, "event_type", audit.EventPromptShieldBlock,
				)
			} else if result.AttackDetected {
				deps.AuditWriter.Emit(audit.AuditEvent{
					RequestID: rid, ApplicationName: policy.Name,
					EventType: audit.EventPromptShieldBlock, Severity: "warn",
					CreatedAt: time.Now().UTC(),
				})
				writeJSONError(w, http.StatusForbidden, "blocked_by_security", "security_error")
				return
			}
		}

		if pipe.RunContentSafety && deps.ShieldClient != nil {
			result, err := deps.ShieldClient.AnalyzeText(ctx, concatMessages(req.Messages))
			if err != nil {
				if pipe.FailMode == "closed" {
					deps.AuditWriter.Emit(audit.AuditEvent{
						RequestID: rid, ApplicationName: policy.Name,
						EventType: audit.EventContentSafetyBlock, Severity: "error",
						Metadata:  map[string]any{"reason": "azure_cs_unavailable"},
						CreatedAt: time.Now().UTC(),
					})
					writeJSONError(w, http.StatusServiceUnavailable, "blocked_by_security", "security_error")
					return
				}
				reqLogger.Warn("content safety unavailable, failing open",
					"err", err, "event_type", audit.EventContentSafetyBlock,
				)
			} else if result.Blocked {
				deps.AuditWriter.Emit(audit.AuditEvent{
					RequestID: rid, ApplicationName: policy.Name,
					EventType: audit.EventContentSafetyBlock, Severity: "warn",
					Metadata:  map[string]any{"max_severity": result.MaxSeverity},
					CreatedAt: time.Now().UTC(),
				})
				writeJSONError(w, http.StatusForbidden, "blocked_by_security", "security_error")
				return
			}
		}

		// ── 4. Budget pre-check ───────────────────────────────────────────────
		if err := deps.BudgetCheck.Check(ctx, policy.Name, policy.MonthlyBudgetBRL); err != nil {
			deps.AuditWriter.Emit(audit.AuditEvent{
				RequestID: rid, ApplicationName: policy.Name,
				EventType: audit.EventBudgetExceeded, Severity: "warn",
				CreatedAt: time.Now().UTC(),
			})
			writeJSONError(w, http.StatusTooManyRequests, "budget_exceeded", "budget_error")
			return
		}

		// Resolve deployment from config.
		modelCfg, ok := deps.Config.ModelByName(req.Model)
		if !ok {
			writeJSONError(w, http.StatusBadRequest, "model_not_found", "policy_error")
			return
		}

		// ── 5+. Dispatch to streaming or non-streaming path ───────────────────
		if req.Stream {
			deps.handleStream(w, r, req, modelCfg, policy, rid, start, reqLogger)
		} else {
			deps.handleNonStream(w, r, req, modelCfg, pipe, policy, rid, start, reqLogger)
		}
	}
}

func (d *ChatDeps) handleNonStream(
	w http.ResponseWriter, r *http.Request,
	req providers.ChatCompletionRequest, modelCfg config.ModelConfig,
	pipe tiers.Pipeline, policy auth.AppPolicy,
	rid string, start time.Time, reqLogger *slog.Logger,
) {
	ctx := r.Context()

	// Apply per-request provider timeout via context (SPEC §9.1 step 9b).
	// The http.Client timeout in the Azure client acts as a secondary safety net.
	var callCtx context.Context
	var callCancel context.CancelFunc
	if d.Config.AzureOpenAI.RequestTimeoutSeconds > 0 {
		callCtx, callCancel = context.WithTimeout(ctx, time.Duration(d.Config.AzureOpenAI.RequestTimeoutSeconds)*time.Second)
		defer callCancel()
	} else {
		callCtx = ctx
	}

	resp, err := d.Provider.ChatCompletions(callCtx, req, modelCfg.Deployment)
	if err != nil {
		d.AuditWriter.Emit(audit.AuditEvent{
			RequestID: rid, ApplicationName: policy.Name,
			EventType: audit.EventProviderError, Severity: "error",
			Metadata:  map[string]any{"err": err.Error()},
			CreatedAt: time.Now().UTC(),
		})
		reqLogger.Error("provider error", "err", err, "event_type", audit.EventProviderError)
		// Distinguish upstream timeout (504) from other provider errors (502) per SPEC §6.4.
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			writeJSONError(w, http.StatusGatewayTimeout, "upstream_timeout", "")
		} else {
			writeJSONError(w, http.StatusBadGateway, "upstream_error", "provider_error")
		}
		return
	}

	// ── 6. Post-validation (Tier 3 only) ─────────────────────────────────────
	if pipe.RunPostValidation && d.Validator != nil {
		for _, ch := range resp.Choices {
			if d.Validator.Check(ch.Message.Content) {
				d.AuditWriter.Emit(audit.AuditEvent{
					RequestID: rid, ApplicationName: policy.Name,
					EventType: audit.EventContentSafetyBlock, Severity: "warn",
					Metadata:  map[string]any{"stage": "post_validation"},
					CreatedAt: time.Now().UTC(),
				})
				writeJSONError(w, http.StatusForbidden, "blocked_by_security", "security_error")
				return
			}
		}
	}

	// ── 7. Emit usage + budget ────────────────────────────────────────────────
	inputTok, outputTok, totalTok := 0, 0, 0
	if resp.Usage != nil {
		inputTok = resp.Usage.PromptTokens
		outputTok = resp.Usage.CompletionTokens
		totalTok = resp.Usage.TotalTokens
	}
	latencyMs := int(time.Since(start).Milliseconds())
	costBRL := estimateCost(modelCfg, inputTok, outputTok)

	d.UsageWriter.Emit(usage.UsageEvent{
		RequestID: rid, ApplicationName: policy.Name,
		Tier: policy.Tier, Model: req.Model, Provider: modelCfg.Provider,
		InputTokens: inputTok, OutputTokens: outputTok, TotalTokens: totalTok,
		LatencyMs: latencyMs, StatusCode: http.StatusOK,
		EstimatedCostBRL: costBRL, CreatedAt: time.Now().UTC(),
	})
	d.BudgetCount.Record(budget.UpdateEvent{
		ApplicationName: policy.Name, TotalTokens: totalTok, EstimatedCostBRL: costBRL,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (d *ChatDeps) handleStream(
	w http.ResponseWriter, r *http.Request,
	req providers.ChatCompletionRequest, modelCfg config.ModelConfig,
	policy auth.AppPolicy, rid string, start time.Time, reqLogger *slog.Logger,
) {
	ctx := r.Context()

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming_unsupported", "")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch, err := d.Provider.StreamChatCompletions(ctx, req, modelCfg.Deployment)
	if err != nil {
		d.AuditWriter.Emit(audit.AuditEvent{
			RequestID: rid, ApplicationName: policy.Name,
			EventType: audit.EventProviderError, Severity: "error",
			Metadata:  map[string]any{"err": err.Error()},
			CreatedAt: time.Now().UTC(),
		})
		// Headers already sent; surface the error in the SSE stream.
		fmt.Fprintf(w, "data: {\"error\":{\"message\":\"upstream_error\"}}\n\n")
		flusher.Flush()
		return
	}

	var lastUsage *providers.Usage

	for chunk := range ch {
		// Detect client disconnect before processing each chunk.
		select {
		case <-ctx.Done():
			d.AuditWriter.Emit(audit.AuditEvent{
				RequestID: rid, ApplicationName: policy.Name,
				EventType: audit.EventStreamCancelled, Severity: "info",
				CreatedAt: time.Now().UTC(),
			})
			reqLogger.Info("stream cancelled by client",
				"event_type", audit.EventStreamCancelled,
			)
			return
		default:
		}

		if chunk.Err != nil {
			reqLogger.Error("stream error", "err", chunk.Err,
				"event_type", audit.EventProviderError,
			)
			fmt.Fprintf(w, "data: {\"error\":{\"message\":\"upstream_error\"}}\n\n")
			flusher.Flush()
			return
		}

		if chunk.Done {
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			break
		}

		if u := tryParseUsage(chunk.Data); u != nil {
			lastUsage = u
		}

		fmt.Fprintf(w, "data: %s\n\n", chunk.Data)
		flusher.Flush()
	}

	// ── Emit usage + budget after stream completes ────────────────────────────
	inputTok, outputTok, totalTok := 0, 0, 0
	if lastUsage != nil {
		inputTok = lastUsage.PromptTokens
		outputTok = lastUsage.CompletionTokens
		totalTok = lastUsage.TotalTokens
	} else {
		// Consumer did not set stream_options.include_usage or Azure omitted it.
		// Emit audit note so operators can correlate zero-token usage records.
		// References: SPEC.md §15.4.
		d.AuditWriter.Emit(audit.AuditEvent{
			RequestID: rid, ApplicationName: policy.Name,
			EventType: audit.EventStreamNoUsage, Severity: "info",
			Metadata:  map[string]any{"note": "no_usage_in_stream"},
			CreatedAt: time.Now().UTC(),
		})
	}
	latencyMs := int(time.Since(start).Milliseconds())
	costBRL := estimateCost(modelCfg, inputTok, outputTok)

	d.UsageWriter.Emit(usage.UsageEvent{
		RequestID: rid, ApplicationName: policy.Name,
		Tier: policy.Tier, Model: req.Model, Provider: modelCfg.Provider,
		InputTokens: inputTok, OutputTokens: outputTok, TotalTokens: totalTok,
		LatencyMs: latencyMs, StatusCode: http.StatusOK,
		EstimatedCostBRL: costBRL, CreatedAt: time.Now().UTC(),
	})
	d.BudgetCount.Record(budget.UpdateEvent{
		ApplicationName: policy.Name, TotalTokens: totalTok, EstimatedCostBRL: costBRL,
	})
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// writeJSONError writes a JSON error response matching SPEC §6.4.
func writeJSONError(w http.ResponseWriter, status int, message, errType string) {
	errBody := map[string]string{"message": message}
	if errType != "" {
		errBody["type"] = errType
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": errBody})
}

// modelAllowed reports whether model appears in the allowed list.
func modelAllowed(model string, allowed []string) bool {
	for _, a := range allowed {
		if a == model {
			return true
		}
	}
	return false
}

// concatMessages joins all message contents with newlines for scanner input.
func concatMessages(msgs []providers.ChatMessage) string {
	parts := make([]string, len(msgs))
	for i, m := range msgs {
		parts[i] = m.Content
	}
	return strings.Join(parts, "\n")
}

// estimateCost computes the BRL cost estimate.
// References: SPEC.md §12.4.
func estimateCost(m config.ModelConfig, inputTok, outputTok int) float64 {
	return float64(inputTok)/1000*m.CostInputPer1kBRL +
		float64(outputTok)/1000*m.CostOutputPer1kBRL
}

// tryParseUsage extracts the Usage field from a raw SSE chunk JSON, if present.
func tryParseUsage(data []byte) *providers.Usage {
	var envelope struct {
		Usage *providers.Usage `json:"usage"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil
	}
	return envelope.Usage
}
