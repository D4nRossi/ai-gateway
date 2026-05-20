// Package main is the composition root for the AI Gateway.
// It bootstraps all dependencies, assembles the router, and starts the HTTP server.
//
// Bootstrap sequence (SPEC §16):
//  1. Parse config file path from env/flags.
//  2. Load + validate YAML config.
//  3. Initialize slog logger.
//  4. Initialize Postgres pool.
//  5. Run migrations.
//  6. Build PolicyStore.
//  7. Initialize Provider (azure or mock via PROVIDER env).
//  8. Initialize PromptShield client (if configured).
//  9. Initialize RateLimiter Manager.
// 10. Initialize Budget PreChecker + Counter.
// 11. Initialize Usage writer.
// 12. Initialize Audit writer.
// 13. Build chi router.
// 14. Build http.Server.
// 15. Trap SIGINT/SIGTERM for graceful shutdown.
// 16. ListenAndServe.
//
// References:
//   - SPEC.md §16 — bootstrap sequence
//   - CLAUDE.md §5.1 — no business logic in main
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/D4nRossi/ai-gateway/internal/api"
	adminapi "github.com/D4nRossi/ai-gateway/internal/api/admin"
	"github.com/D4nRossi/ai-gateway/internal/api/handlers"
	"github.com/D4nRossi/ai-gateway/internal/app/adminservice"
	"github.com/D4nRossi/ai-gateway/internal/app/proxyservice"
	"github.com/D4nRossi/ai-gateway/internal/audit"
	"github.com/D4nRossi/ai-gateway/internal/auth"
	"github.com/D4nRossi/ai-gateway/internal/budget"
	"github.com/D4nRossi/ai-gateway/internal/config"
	"github.com/D4nRossi/ai-gateway/internal/db"
	"github.com/D4nRossi/ai-gateway/internal/infra/crypto"
	pginfra "github.com/D4nRossi/ai-gateway/internal/infra/postgres"
	"github.com/D4nRossi/ai-gateway/internal/observability"
	"github.com/D4nRossi/ai-gateway/internal/providers"
	"github.com/D4nRossi/ai-gateway/internal/providers/azureopenai"
	"github.com/D4nRossi/ai-gateway/internal/providers/mock"
	"github.com/D4nRossi/ai-gateway/internal/proxy"
	"github.com/D4nRossi/ai-gateway/internal/proxy/loadbalancer"
	"github.com/D4nRossi/ai-gateway/internal/ratelimit"
	"github.com/D4nRossi/ai-gateway/internal/security/masking"
	"github.com/D4nRossi/ai-gateway/internal/security/postvalidation"
	"github.com/D4nRossi/ai-gateway/internal/security/promptshield"
	"github.com/D4nRossi/ai-gateway/internal/usage"
)

func main() {
	if err := run(); err != nil {
		slog.Error("gateway failed to start", "err", err)
		os.Exit(1)
	}
}

func run() error {
	// ── 1. Config file path ───────────────────────────────────────────────────
	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "configs/gateway.yaml"
	}

	// ── 2. Load + validate config ─────────────────────────────────────────────
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// ── 3. Initialize logger ──────────────────────────────────────────────────
	logLevel := cfg.Logging.Level
	if logLevel == "" {
		logLevel = "info"
	}
	logFormat := cfg.Logging.Format
	if logFormat == "" {
		logFormat = "json"
	}
	logger, err := observability.New(logLevel, logFormat)
	if err != nil {
		return fmt.Errorf("initializing logger: %w", err)
	}
	logger.Info("ai gateway starting",
		"config_path", cfgPath,
		"log_level", logLevel,
	)

	// ── 4. Postgres pool ──────────────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := db.NewPool(ctx, cfg.Database.URL, cfg.Database.MaxConns, cfg.Database.MinConns)
	if err != nil {
		return fmt.Errorf("connecting to postgres: %w", err)
	}
	defer pool.Close()
	logger.Info("postgres pool connected")

	// ── 5. Run migrations ─────────────────────────────────────────────────────
	if err := db.RunMigrations(cfg.Database.URL, "migrations"); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}
	logger.Info("migrations applied")

	// ── 5a. Admin infrastructure ──────────────────────────────────────────────
	// AES-256-GCM encrypter for proxy target credentials (ADR-0012).
	encrypter, err := crypto.NewAESGCMEncrypter(cfg.Database.EncryptionKeyHex)
	if err != nil {
		return fmt.Errorf("initializing AES-GCM encrypter: %w", err)
	}

	adminRepo := pginfra.NewAdminRepo(pool)
	appRepo := pginfra.NewApplicationRepo(pool)
	endpointRepo := pginfra.NewEndpointRepo(pool, encrypter)

	adminSvc := adminservice.New(appRepo, endpointRepo, adminRepo, logger, 0)

	adminRouter := adminapi.NewRouter(adminapi.Deps{
		Svc:    adminSvc,
		Pool:   pool,
		Logger: logger,
	})
	logger.Info("admin api configured")

	// Purge stale sessions at startup so the admin_sessions table stays bounded.
	// Errors are non-fatal — the gateway can run with stale rows.
	if err := adminSvc.PurgeExpiredSessions(ctx); err != nil {
		logger.Warn("failed to purge expired admin sessions at startup", "err", err)
	}

	// ── 5b. Proxy plane (generic HTTP proxy engine) ───────────────────────────
	// One shared *http.Transport across all targets keeps connection pools warm
	// (ADR-0010, ADR-0013). Balancer state is kept per endpoint by the Registry,
	// which detects strategy changes automatically.
	balancerRegistry := loadbalancer.NewRegistry()
	proxySvc := proxyservice.New(endpointRepo, balancerRegistry, logger)
	proxyTransport := proxy.NewTransport()
	proxyAuth := proxy.Auth(appRepo, logger)
	proxyHandler := proxy.Handler(proxySvc, proxyTransport, logger)
	logger.Info("generic proxy plane configured")

	// ── 6. PolicyStore ────────────────────────────────────────────────────────
	policyStore := auth.NewPolicyStore(cfg.Applications)

	// ── 6a. Pre-build PII maskers ─────────────────────────────────────────────
	// One Masker per tier, constructed here so regex patterns are compiled at
	// bootstrap. Detector *regexp.Regexp vars are package-level (compiled once
	// at package init); pre-building Masker instances avoids per-request slice
	// and struct allocation on the hot path. Safe for concurrent use.
	// References: SPEC.md §10.2; CLAUDE.md §14.
	maskers := map[string]*masking.Masker{
		"tier_1": masking.NewMasker("tier_1"),
		"tier_2": masking.NewMasker("tier_2"),
		"tier_3": masking.NewMasker("tier_3"),
	}

	// ── 7. Provider ───────────────────────────────────────────────────────────
	var prov providers.Provider
	switch os.Getenv("PROVIDER") {
	case "mock":
		prov = mock.New()
		logger.Info("using mock provider")
	default:
		timeout := time.Duration(cfg.AzureOpenAI.RequestTimeoutSeconds) * time.Second
		if timeout == 0 {
			timeout = 60 * time.Second
		}
		prov = azureopenai.New(
			cfg.AzureOpenAI.Endpoint,
			cfg.AzureOpenAI.APIKey,
			cfg.AzureOpenAI.APIVersion,
			timeout,
		)
		logger.Info("using azure openai provider",
			"endpoint", cfg.AzureOpenAI.Endpoint,
			"api_version", cfg.AzureOpenAI.APIVersion,
		)
	}

	// ── 8. PromptShield client ────────────────────────────────────────────────
	var shieldClient *promptshield.Client
	if cs := cfg.AzureContentSafety; cs != nil {
		shieldTimeout := time.Duration(cs.PromptShieldTimeoutMs) * time.Millisecond
		shieldClient = promptshield.NewClient(cs.Endpoint, cs.APIKey, cs.APIVersion, shieldTimeout)
		logger.Info("azure content safety configured")
	}

	// ── 9. Rate limiter ───────────────────────────────────────────────────────
	rateMgr := ratelimit.NewManager()
	for _, app := range cfg.Applications {
		rateMgr.Register(app.Name, app.MaxRPM)
	}

	// ── 10. Budget ────────────────────────────────────────────────────────────
	budgetChecker := budget.NewChecker(pool, logger)
	budgetCounter := budget.NewCounter(ctx, pool, logger)

	// ── 11. Usage writer ──────────────────────────────────────────────────────
	usageWriter := usage.NewWriter(ctx, pool, logger)

	// ── 12. Audit writer ──────────────────────────────────────────────────────
	auditWriter := audit.NewWriter(ctx, pool, logger)

	// ── 13. Build router ──────────────────────────────────────────────────────
	routerDeps := api.RouterDeps{
		Config:       cfg,
		PolicyStore:  policyStore,
		RateLimiter:  rateMgr,
		AuditWriter:  auditWriter,
		Pool:         pool,
		Logger:       logger,
		AdminHandler: adminRouter,
		ProxyAuth:    proxyAuth,
		ProxyHandler: proxyHandler,
		ChatDeps: handlers.ChatDeps{
			Provider:     prov,
			Config:       cfg,
			AuditWriter:  auditWriter,
			UsageWriter:  usageWriter,
			BudgetCheck:  budgetChecker,
			BudgetCount:  budgetCounter,
			ShieldClient: shieldClient,
			Validator:    postvalidation.New(),
			Logger:       logger,
			Maskers:      maskers,
		},
	}

	router := api.NewRouter(routerDeps)

	// ── 14. HTTP server ───────────────────────────────────────────────────────
	port := cfg.Server.Port
	readTimeout := time.Duration(cfg.Server.ReadTimeoutSeconds) * time.Second
	readHeaderTimeout := time.Duration(cfg.Server.ReadHeaderTimeoutSeconds) * time.Second
	idleTimeout := time.Duration(cfg.Server.IdleTimeoutSeconds) * time.Second
	maxHeaderBytes := cfg.Server.MaxHeaderBytes
	if maxHeaderBytes == 0 {
		maxHeaderBytes = 1 << 20
	}

	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(port),
		Handler:           router,
		ReadTimeout:       readTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      0, // disabled for SSE streaming (ADR-0008)
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}

	// ── 15. Graceful shutdown wiring ──────────────────────────────────────────
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server: %w", err)
		}
	}()

	// ── 16. Block until signal or fatal error ─────────────────────────────────
	select {
	case sig := <-sigCh:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-errCh:
		return err
	}

	// Give in-flight requests 5 seconds to complete before forcing close.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown error", "err", err)
	}

	// Signal background workers (usage, audit, budget) to drain and exit.
	cancel()

	logger.Info("gateway stopped")
	return nil
}
