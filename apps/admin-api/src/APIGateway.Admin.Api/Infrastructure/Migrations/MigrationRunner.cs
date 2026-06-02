using System.Reflection;
using DbUp;
using DbUp.Engine;

namespace APIGateway.Admin.Api.Infrastructure.Migrations;

/// <summary>
/// Pipeline de migrations T-SQL usando DbUp.
/// Aplica cada arquivo <c>Infrastructure/Migrations/Scripts/*.sql</c> em ordem
/// alfabética dos nomes, com uma transação por script.
/// </summary>
/// <remarks>
/// Reasoning: ADR-0025 mantém <c>MIGRATIONS_AUTO_APPLY=false</c> em produção,
/// então o pipeline NÃO é invocado no <see cref="Program"/> do Kestrel. O entry
/// point bifurca em <c>args[0] == "migrate"</c> e chama <see cref="RunAsync"/>
/// no modo CLI, encerrando após sucesso/erro. O bookkeeping table do DbUp
/// (<c>gogateway.SchemaVersions</c>) substitui o <c>schema_migrations</c> do
/// golang-migrate enquanto o gateway Go ainda lê do seu próprio bookkeeping.
///
/// References:
///   - ADR-0025 — MIGRATIONS_AUTO_APPLY toggle (default false em prod)
///   - ADR-0027 — DbUp como ferramenta de migrations
///   - https://dbup.readthedocs.io/en/latest/usage/
/// </remarks>
public static class MigrationRunner
{
    /// <summary>
    /// Aplica todas as migrations pendentes. Retorna 0 em sucesso, &gt; 0 em erro.
    /// </summary>
    public static int Run(string connectionString, TextWriter log)
    {
        var assembly = typeof(MigrationRunner).Assembly;

        var upgrader = DeployChanges.To
            .SqlDatabase(connectionString)
            .WithScriptsEmbeddedInAssembly(
                assembly,
                name => name.StartsWith(
                    "APIGateway.Admin.Api.Infrastructure.Migrations.Scripts.",
                    StringComparison.Ordinal))
            .WithTransactionPerScript()
            .JournalToSqlTable("gogateway", "SchemaVersions")
            .LogToConsole()
            .Build();

        DatabaseUpgradeResult result = upgrader.PerformUpgrade();

        if (!result.Successful)
        {
            log.WriteLine($"migration failed at {result.ErrorScript?.Name ?? "(unknown)"}");
            log.WriteLine(result.Error?.ToString() ?? "(no error message)");
            return 1;
        }

        log.WriteLine($"migration ok — applied {result.Scripts.Count} script(s)");
        return 0;
    }

    /// <summary>
    /// Lista os scripts SQL embedded em ordem de execução.
    /// Útil pra <c>migrate status</c> e diagnóstico.
    /// </summary>
    public static IReadOnlyList<string> EmbeddedScriptNames()
    {
        return typeof(MigrationRunner).Assembly
            .GetManifestResourceNames()
            .Where(n => n.StartsWith(
                "APIGateway.Admin.Api.Infrastructure.Migrations.Scripts.",
                StringComparison.Ordinal))
            .OrderBy(n => n, StringComparer.Ordinal)
            .ToArray();
    }
}
