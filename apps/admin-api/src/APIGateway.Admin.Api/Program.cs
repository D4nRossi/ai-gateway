using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Features.Users;
using APIGateway.Admin.Api.Infrastructure.Auditing;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Healthchecks;
using APIGateway.Admin.Api.Infrastructure.Migrations;
using Microsoft.AspNetCore.Diagnostics.HealthChecks;
using Serilog;
using Serilog.Formatting.Compact;

// ── CLI mode: migrate ───────────────────────────────────────────────────────
// Preserva ADR-0025 (MIGRATIONS_AUTO_APPLY=false em prod): o Kestrel NUNCA
// roda DbUp automático. DBA aplica via `dotnet APIGateway.Admin.Api.dll migrate up`
// (ou docker run --entrypoint dotnet image migrate up).
if (args.Length >= 1 && args[0] == "migrate")
{
    return RunMigrationsCli(args);
}

// ── Web host: Minimal API + Serilog + Dapper + DI ──────────────────────────
var builder = WebApplication.CreateBuilder(args);

builder.Host.UseSerilog((ctx, services, logger) =>
{
    logger
        .ReadFrom.Configuration(ctx.Configuration)
        .ReadFrom.Services(services)
        .Enrich.FromLogContext()
        .Enrich.WithProperty("app", "admin-api")
        // JSON estruturado em stdout — mesmo formato semântico do slog do Go
        // (CLAUDE.md §8.1). Em Development, configuração override pra Text.
        .WriteTo.Console(new CompactJsonFormatter());
});

// ── DI ──────────────────────────────────────────────────────────────────────
var connectionString = builder.Configuration.GetConnectionString("Gateway")
    ?? throw new InvalidOperationException(
        "ConnectionStrings:Gateway is required (env ConnectionStrings__Gateway).");

builder.Services.AddSingleton(new ConnectionFactory(connectionString));
builder.Services.AddScoped<SessionAuthFilter>();
builder.Services.AddSingleton<IAuditEventWriter, SqlAuditEventWriter>();

builder.Services
    .AddHealthChecks()
    .AddCheck<SqlServerHealthCheck>("sqlserver", tags: new[] { "ready" });

builder.Services.AddOpenApi();

var app = builder.Build();

// ── Pipeline ────────────────────────────────────────────────────────────────
app.UseSerilogRequestLogging();

// Healthchecks: /healthz é trivial (liveness); /readyz roda os tagged "ready"
app.MapGet("/healthz", () => Results.Ok(new { status = "ok" }))
    .WithTags("Health");

app.MapHealthChecks("/readyz", new HealthCheckOptions
{
    Predicate = check => check.Tags.Contains("ready"),
});

// OpenAPI in dev/staging — em produção, /openapi/v1.json fica fechado pelo
// nginx terminator (não exposto pra fora da rede corp).
if (app.Environment.IsDevelopment())
{
    app.MapOpenApi();
}

// ── Routes /admin/v1 ───────────────────────────────────────────────────────
// Grouping centraliza prefixo. Cada feature monta seu subgrupo.
var adminV1 = app.MapGroup("/admin/v1");

adminV1.MapGroup("/auth")
    .MapLogin()
    .MapLogout();

adminV1.MapGroup("/users")
    .MapList()
    .MapCreate()
    .MapDeactivate();

app.Run();
return 0;

// ── Local helpers ───────────────────────────────────────────────────────────
static int RunMigrationsCli(string[] args)
{
    // args[0]=="migrate". Opções aceitas:
    //   migrate up        — aplica todos os scripts pendentes (idempotente)
    //   migrate status    — lista scripts embedded + última versão aplicada
    var sub = args.Length >= 2 ? args[1] : "up";

    // Lê connection string do mesmo lugar que o web host (env + appsettings).
    var cfg = new ConfigurationBuilder()
        .SetBasePath(Directory.GetCurrentDirectory())
        .AddJsonFile("appsettings.json", optional: true)
        .AddJsonFile($"appsettings.{Environment.GetEnvironmentVariable("DOTNET_ENVIRONMENT") ?? "Production"}.json", optional: true)
        .AddEnvironmentVariables()
        .Build();

    var connectionString = cfg.GetConnectionString("Gateway");
    if (string.IsNullOrWhiteSpace(connectionString))
    {
        Console.Error.WriteLine("ConnectionStrings:Gateway is required (env ConnectionStrings__Gateway).");
        return 2;
    }

    switch (sub)
    {
        case "up":
            return MigrationRunner.Run(connectionString, Console.Out);
        case "status":
            foreach (var name in MigrationRunner.EmbeddedScriptNames())
            {
                Console.WriteLine(name);
            }
            return 0;
        default:
            Console.Error.WriteLine($"unknown migrate subcommand: {sub}");
            Console.Error.WriteLine("usage: migrate <up|status>");
            return 2;
    }
}

/// <summary>Marker pra testes integration usarem WebApplicationFactory&lt;Program&gt;.</summary>
public partial class Program;
