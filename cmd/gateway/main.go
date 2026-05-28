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
	"strings"
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
	"github.com/D4nRossi/ai-gateway/internal/infra/keyvault"
	mssqlinfra "github.com/D4nRossi/ai-gateway/internal/infra/mssql"
	"github.com/D4nRossi/ai-gateway/internal/infra/secretsdb"
	"github.com/D4nRossi/ai-gateway/internal/observability"
	"github.com/D4nRossi/ai-gateway/internal/providers"
	"github.com/D4nRossi/ai-gateway/internal/providers/azureopenai"
	"github.com/D4nRossi/ai-gateway/internal/providers/mock"
	"github.com/D4nRossi/ai-gateway/internal/proxy"
	"github.com/D4nRossi/ai-gateway/internal/proxy/loadbalancer"
	"github.com/D4nRossi/ai-gateway/internal/ratelimit"
	"github.com/D4nRossi/ai-gateway/internal/security/azlanguage"
	"github.com/D4nRossi/ai-gateway/internal/security/masking"
	"github.com/D4nRossi/ai-gateway/internal/security/postvalidation"
	"github.com/D4nRossi/ai-gateway/internal/security/promptshield"
	"github.com/D4nRossi/ai-gateway/internal/usage"
	"github.com/D4nRossi/ai-gateway/web"
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

	// ── 1a. Key Vault client (optional) ───────────────────────────────────────
	// When KEYVAULT_URI is set, the config loader can resolve ${kv:NAME}
	// placeholders against Azure Key Vault. When unset, ${kv:NAME} markers in
	// the YAML are a fatal config error (ADR-0018, fail-fast policy).
	//
	// Diagnostic logging is emitted via slog default (the request-scoped logger
	// is not initialized yet at this point — config validation depends on KV).
	// Without this signal, "database.password is required" errors become hard
	// to diagnose: the operator can't tell if the KV resolver was wired up at
	// all, or if it failed silently.
	bootCtx, bootCancel := context.WithCancel(context.Background())
	defer bootCancel()

	// ── Secret provider selection (ADR-0026) ──────────────────────────────────
	// SECRET_PROVIDER controls where secrets live:
	//   - "kv" (default) — Azure Key Vault via ${kv:NAME} references
	//   - "db"           — gogateway.secrets table (used pra target creds da
	//                      Onda 4.5). Boot secrets vêm de DPAPI env file no Windows.
	// Yaml em modo "db" usa ${VAR_NAME} em vez de ${kv:...} pros 4 secrets de
	// boot (DATABASE_PASSWORD, AZURE_OPENAI_API_KEY, AZURE_LANGUAGE_API_KEY,
	// DB_ENCRYPTION_KEY_HEX), populados pelo DPAPI loader abaixo.
	secretProvider := strings.ToLower(strings.TrimSpace(os.Getenv("SECRET_PROVIDER")))
	if secretProvider == "" {
		secretProvider = "kv"
	}
	if secretProvider != "kv" && secretProvider != "db" {
		return fmt.Errorf("SECRET_PROVIDER must be 'kv' or 'db', got %q", secretProvider)
	}

	// In db mode, decrypt the bootstrap .env.dpapi (Windows only) and expose
	// its KEY=VALUE pairs as process env vars so ${VAR} references in the
	// YAML resolve transparently. Path comes from DPAPI_ENV_FILE; missing
	// env var is fatal in db mode (operator must opt in explicitly).
	if secretProvider == "db" {
		envFile := strings.TrimSpace(os.Getenv("DPAPI_ENV_FILE"))
		if envFile == "" {
			return errors.New("SECRET_PROVIDER=db requires DPAPI_ENV_FILE pointing to the encrypted .env.dpapi (Windows only)")
		}
		if err := loadDPAPIEnvFile(envFile); err != nil {
			return fmt.Errorf("loading DPAPI env file %q: %w", envFile, err)
		}
		slog.Info("dpapi env file loaded", "path", envFile)
	}

	var (
		secretResolver config.SecretResolver
		kvClient       *keyvault.Client
	)
	switch secretProvider {
	case "kv":
		vaultURL := os.Getenv("KEYVAULT_URI")
		if vaultURL != "" {
			c, err := keyvault.New(vaultURL)
			if err != nil {
				return fmt.Errorf("initializing Azure Key Vault client: %w", err)
			}
			kvClient = c
			secretResolver = kvClient
			slog.Info("key vault resolver initialized", "vault_url", vaultURL)
		} else {
			slog.Warn("KEYVAULT_URI is empty or unset — ${kv:NAME} references in gateway.yaml will fail to resolve. " +
				"Check that the .env file is loaded by your runner (GoLand EnvFile plugin, or shell export) " +
				"and that KEYVAULT_URI=https://<vault-name>.vault.azure.net is set.")
		}

	case "db":
		// Boot secrets vêm de env vars (DPAPI). Yaml NÃO deve usar ${kv:...}
		// em modo db. noop resolver emite erro claro se aparecer.
		secretResolver = noopSecretResolverDB{}
		slog.Info("secret provider: db (gogateway.secrets)")
	}

	// ── 2. Load + validate config ─────────────────────────────────────────────
	cfg, err := config.Load(bootCtx, cfgPath, secretResolver)
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

	// ── 4. SQL Server connection (ADR-0022) ───────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dbHandle, err := db.NewMSSQL(ctx, cfg.Database)
	if err != nil {
		return fmt.Errorf("connecting to sqlserver: %w", err)
	}
	defer dbHandle.Close()
	logger.Info("sqlserver connected",
		"host", cfg.Database.Host,
		"database", cfg.Database.Database,
		"schema", cfg.Database.Schema,
	)

	// ── 5. Migrations ─────────────────────────────────────────────────────────
	// ConnString builds the sqlserver:// URL from the structured config; passing
	// it explicitly avoids leaking the password through any logging that might
	// inspect the raw config struct.
	//
	// MIGRATIONS_AUTO_APPLY (env var) controls who applies migrations (ADR-0025):
	//   - unset / "true" (default): gateway runs `migrate up` at boot. Suitable
	//     for dev, homolog, and single-operator environments.
	//   - "false": gateway only asserts that the database is already at the
	//     expected version. Suitable for prod under DBA-controlled change
	//     windows — operator runs `migrate up` ahead of restarting.
	autoApply := strings.ToLower(strings.TrimSpace(os.Getenv("MIGRATIONS_AUTO_APPLY")))
	manualMode := autoApply == "false" || autoApply == "0" || autoApply == "no"
	if manualMode {
		if err := db.AssertSchemaUpToDate(db.ConnString(cfg.Database), "migrations"); err != nil {
			return fmt.Errorf("schema check (MIGRATIONS_AUTO_APPLY=false): %w", err)
		}
		logger.Info("schema check passed (manual migration mode)")
	} else {
		if err := db.RunMigrations(db.ConnString(cfg.Database), "migrations"); err != nil {
			return fmt.Errorf("running migrations: %w", err)
		}
		logger.Info("migrations applied")
	}

	// ── 5a. Admin infrastructure ──────────────────────────────────────────────
	// AES-256-GCM encrypter for proxy target credentials (ADR-0012).
	encrypter, err := crypto.NewAESGCMEncrypter(cfg.Database.EncryptionKeyHex)
	if err != nil {
		return fmt.Errorf("initializing AES-GCM encrypter: %w", err)
	}

	adminRepo := mssqlinfra.NewAdminRepo(dbHandle)
	appRepo := mssqlinfra.NewApplicationRepo(dbHandle)
	endpointRepo := mssqlinfra.NewEndpointRepo(dbHandle, encrypter)

	// ── Runtime secret backend (ADR-0026) ─────────────────────────────────────
	// Both the admin service (write side, target migrations da Onda 4.5) and
	// the proxy CredentialResolver (read side) need a runtime SecretGetter /
	// SecretSetter pra target credentials. In kv mode this is kvClient
	// (possibly nil when KEYVAULT_URI is unset); in db mode this is
	// secretsdb.Client backed by gogateway.secrets. Both implement the same
	// keyvault interfaces — drop-in replacement.
	var (
		runtimeSecretGetter keyvault.SecretGetter
		runtimeSecretSetter keyvault.SecretSetter
	)
	switch secretProvider {
	case "kv":
		runtimeSecretGetter = kvClient // possibly nil; resolver handles
		runtimeSecretSetter = kvClient
	case "db":
		secretsClient := secretsdb.New(dbHandle).WithLogger(logger)
		runtimeSecretGetter = secretsClient
		runtimeSecretSetter = secretsClient
		logger.Info("secrets backend initialized", "provider", "db")
	}

	adminSvc := adminservice.New(appRepo, endpointRepo, adminRepo, logger, 0).
		WithKVSetter(runtimeSecretSetter)

	adminRouter := adminapi.NewRouter(adminapi.Deps{
		Svc:    adminSvc,
		DB:     dbHandle,
		Logger: logger,
	})
	logger.Info("admin api configured")

	// Purge stale sessions at startup so the admin_sessions table stays bounded.
	// Errors are non-fatal — the gateway can run with stale rows.
	if err := adminSvc.PurgeExpiredSessions(ctx); err != nil {
		logger.Warn("failed to purge expired admin sessions at startup", "err", err)
	}

	// ── 5b. Usage writer (constructed early so the proxy plane can emit) ─────
	// ADR-0024: proxy plane emits UsageEvent too, so the writer must exist
	// before proxy.Handler is built. Was previously created in §11 alongside
	// the chat handler deps; moved up here to satisfy the new dependency
	// order. The single writer instance is shared between both emit paths.
	usageWriter := usage.NewWriter(ctx, dbHandle, logger)

	// Model lookup map for cost computation in the proxy plane (ADR-0024).
	// Indexed by public_name — the same key clients use in chat completion
	// request bodies. Lookups for unknown models return zero cost.
	modelByName := make(map[string]config.ModelConfig, len(cfg.Models))
	for _, m := range cfg.Models {
		modelByName[m.PublicName] = m
	}

	// ── 5c. Proxy plane (generic HTTP proxy engine) ───────────────────────────
	// One shared *http.Transport across all targets keeps connection pools warm
	// (ADR-0010, ADR-0013). Balancer state is kept per endpoint by the Registry,
	// which detects strategy changes automatically.
	balancerRegistry := loadbalancer.NewRegistry()
	proxySvc := proxyservice.New(endpointRepo, balancerRegistry, logger)
	// Credential resolver wires per-target storage mode (ADR-0020).
	// runtimeSecretGetter is kvClient (kv mode) or secretsdb.Client (db mode);
	// pode ser nil em kv mode quando KEYVAULT_URI não está setado — o resolver
	// short-circuits mode=aes targets e surface erros claros pra kv|both.
	credResolver := proxyservice.NewCredentialResolver(runtimeSecretGetter, logger)
	proxyTransport := proxy.NewTransport()
	proxyAuth := proxy.Auth(appRepo, logger)
	proxyHandler := proxy.Handler(proxySvc, credResolver, proxyTransport, usageWriter, modelByName, logger)
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

	// ── 8a. Azure Language PII client (ADR-0019) ──────────────────────────────
	// Optional: when the azure_language section is absent the chat pipeline
	// silently skips the remote PII step for Tier 2/3 (regex masking still
	// runs). When present, Tier 2 fails open on errors and Tier 3 fails closed.
	var languageClient *azlanguage.Client
	if al := cfg.AzureLanguage; al != nil {
		langTimeout := time.Duration(al.TimeoutMs) * time.Millisecond
		if langTimeout == 0 {
			langTimeout = 1500 * time.Millisecond
		}
		languageClient = azlanguage.New(al.Endpoint, al.APIKey, al.APIVersion, al.Language, langTimeout)
		logger.Info("azure language pii configured",
			"endpoint", al.Endpoint,
			"api_version", al.APIVersion,
			"language", al.Language,
			"timeout_ms", langTimeout.Milliseconds(),
		)
	}

	// ── 9. Rate limiter ───────────────────────────────────────────────────────
	rateMgr := ratelimit.NewManager()
	for _, app := range cfg.Applications {
		rateMgr.Register(app.Name, app.MaxRPM)
	}

	// ── 10. Budget ────────────────────────────────────────────────────────────
	budgetChecker := budget.NewChecker(dbHandle, logger)
	budgetCounter := budget.NewCounter(ctx, dbHandle, logger)

	// ── 11. Usage writer — moved to §5b to be available to the proxy plane.

	// ── 12. Audit writer ──────────────────────────────────────────────────────
	auditWriter := audit.NewWriter(ctx, dbHandle, logger)

	// ── 13. Build router ──────────────────────────────────────────────────────
	routerDeps := api.RouterDeps{
		Config:       cfg,
		PolicyStore:  policyStore,
		RateLimiter:  rateMgr,
		AuditWriter:  auditWriter,
		DB:           dbHandle,
		Logger:       logger,
		AdminHandler: adminRouter,
		ProxyAuth:    proxyAuth,
		ProxyHandler: proxyHandler,
		WebHandler:   web.Handler(),
		ChatDeps: handlers.ChatDeps{
			Provider:       prov,
			Config:         cfg,
			AuditWriter:    auditWriter,
			UsageWriter:    usageWriter,
			BudgetCheck:    budgetChecker,
			BudgetCount:    budgetCounter,
			ShieldClient:   shieldClient,
			LanguageClient: languageClient,
			Validator:      postvalidation.New(),
			Logger:         logger,
			Maskers:        maskers,
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
