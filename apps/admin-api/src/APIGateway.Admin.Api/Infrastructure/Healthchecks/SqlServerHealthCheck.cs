using APIGateway.Admin.Api.Infrastructure.Database;
using Dapper;
using Microsoft.Extensions.Diagnostics.HealthChecks;

namespace APIGateway.Admin.Api.Infrastructure.Healthchecks;

/// <summary>
/// Health check de readiness que valida SELECT 1 contra o SQL Server.
/// Usado em <c>/readyz</c> (liveness <c>/healthz</c> é trivial, retorna 200).
/// </summary>
public sealed class SqlServerHealthCheck : IHealthCheck
{
    private readonly ConnectionFactory _connections;

    public SqlServerHealthCheck(ConnectionFactory connections) => _connections = connections;

    public async Task<HealthCheckResult> CheckHealthAsync(
        HealthCheckContext context,
        CancellationToken cancellationToken = default)
    {
        try
        {
            using var conn = await _connections.CreateOpenAsync(cancellationToken);
            var one = await conn.ExecuteScalarAsync<int>(
                new CommandDefinition("SELECT 1;", cancellationToken: cancellationToken));
            return one == 1
                ? HealthCheckResult.Healthy("sql server reachable")
                : HealthCheckResult.Unhealthy($"unexpected probe result: {one}");
        }
        catch (Exception ex)
        {
            return HealthCheckResult.Unhealthy("sql server unreachable", ex);
        }
    }
}
