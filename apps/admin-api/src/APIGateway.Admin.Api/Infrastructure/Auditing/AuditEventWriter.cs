using System.Data;
using System.Text.Json;
using APIGateway.Admin.Api.Infrastructure.Database;
using Dapper;

namespace APIGateway.Admin.Api.Infrastructure.Auditing;

/// <summary>
/// Escreve uma linha em <c>gogateway.audit_events</c> mantendo paridade exata
/// de schema e taxonomia com o gateway Go (ADR-0024).
/// </summary>
/// <remarks>
/// Reasoning: o frontend exibe um único feed de auditoria misturando
/// eventos emitidos pelo data plane (Go) e pelo admin plane (.NET). Toda
/// mutator slice em <c>Features/*</c> chama <see cref="WriteAsync"/> depois
/// do COMMIT do trabalho de domínio.
///
/// References:
///   - migrations/001_init.up.sql (schema audit_events)
///   - ADR-0024 — usage tracking + audit taxonomy
/// </remarks>
public interface IAuditEventWriter
{
    Task WriteAsync(AuditEvent evt, CancellationToken ct = default);
}

public sealed record AuditEvent(
    string RequestId,
    string ApplicationName,
    string EventType,
    string Severity,
    object? Metadata = null);

public sealed class SqlAuditEventWriter : IAuditEventWriter
{
    private readonly ConnectionFactory _connections;
    private readonly ILogger<SqlAuditEventWriter> _logger;

    public SqlAuditEventWriter(ConnectionFactory connections, ILogger<SqlAuditEventWriter> logger)
    {
        _connections = connections;
        _logger = logger;
    }

    public async Task WriteAsync(AuditEvent evt, CancellationToken ct = default)
    {
        const string sql = """
            INSERT INTO gogateway.audit_events
                (request_id, application_name, event_type, severity, metadata)
            VALUES
                (@RequestId, @ApplicationName, @EventType, @Severity, @Metadata);
            """;

        var metadataJson = evt.Metadata is null
            ? null
            : JsonSerializer.Serialize(evt.Metadata);

        try
        {
            using IDbConnection conn = await _connections.CreateOpenAsync(ct);
            await conn.ExecuteAsync(
                new CommandDefinition(
                    sql,
                    new
                    {
                        evt.RequestId,
                        evt.ApplicationName,
                        evt.EventType,
                        evt.Severity,
                        Metadata = metadataJson,
                    },
                    cancellationToken: ct));
        }
        catch (Exception ex)
        {
            // Audit não é caminho crítico — logar e seguir. Perder evento de
            // audit não pode derrubar a request principal (ADR-0005 idem).
            _logger.LogError(ex,
                "audit_event_write_failed event_type={EventType} request_id={RequestId}",
                evt.EventType, evt.RequestId);
        }
    }
}
