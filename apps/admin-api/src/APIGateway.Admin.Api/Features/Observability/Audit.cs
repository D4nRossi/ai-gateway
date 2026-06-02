using System.Data;
using System.Text;
using System.Text.Json.Serialization;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;

namespace APIGateway.Admin.Api.Features.Observability;

/// <summary>
/// GET <c>/admin/v1/audit</c> — lista de <c>gogateway.audit_events</c>.
/// Filtros: from/to/application/event_type/limit. Requer role <c>viewer</c>.
/// </summary>
/// <remarks>
/// Paridade com <c>handlers/observability.go:ListAuditEvents</c>. <c>metadata</c>
/// fica como string opaca (NVARCHAR(MAX)); frontend faz o parse quando precisa.
/// </remarks>
public static class Audit
{
    public static IEndpointRouteBuilder MapObservabilityAudit(this IEndpointRouteBuilder routes)
    {
        routes.MapGet("/audit", HandleAsync)
            .WithName("ObservabilityAuditList")
            .WithTags("Observability")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("viewer"));
        return routes;
    }

    public sealed record AuditEventResponse(
        [property: JsonPropertyName("id")]                long Id,
        [property: JsonPropertyName("request_id")]        string RequestId,
        [property: JsonPropertyName("application_name")]  string ApplicationName,
        [property: JsonPropertyName("event_type")]        string EventType,
        [property: JsonPropertyName("severity")]          string Severity,
        [property: JsonPropertyName("metadata")]          string? Metadata,
        [property: JsonPropertyName("created_at")]        string CreatedAt);

    public static async Task<IResult> HandleAsync(
        HttpContext http,
        ConnectionFactory connections,
        ILogger<AuditMarker> logger)
    {
        var (error, from, to, limit) = QueryHelpers.ParseTimeRange(http.Request);
        if (error is not null) return error;

        var application = http.Request.Query["application"].ToString();
        var eventType = http.Request.Query["event_type"].ToString();
        var ct = http.RequestAborted;

        var sql = new StringBuilder("""
            SELECT id                AS Id,
                   request_id        AS RequestId,
                   application_name  AS ApplicationName,
                   event_type        AS EventType,
                   severity          AS Severity,
                   metadata          AS Metadata,
                   created_at        AS CreatedAt
            FROM gogateway.audit_events
            WHERE created_at BETWEEN @From AND @To
            """);
        var parameters = new DynamicParameters();
        parameters.Add("From", from);
        parameters.Add("To", to);

        if (!string.IsNullOrEmpty(application))
        {
            sql.Append(" AND application_name = @Application");
            parameters.Add("Application", application);
        }
        if (!string.IsNullOrEmpty(eventType))
        {
            sql.Append(" AND event_type = @EventType");
            parameters.Add("EventType", eventType);
        }
        sql.Append(" ORDER BY created_at DESC OFFSET 0 ROWS FETCH NEXT @Limit ROWS ONLY;");
        parameters.Add("Limit", limit);

        try
        {
            using IDbConnection conn = await connections.CreateOpenAsync(ct);
            var rows = await conn.QueryAsync<AuditEventRow>(
                new CommandDefinition(sql.ToString(), parameters, cancellationToken: ct));

            var response = rows.Select(r => new AuditEventResponse(
                r.Id, r.RequestId, r.ApplicationName, r.EventType, r.Severity, r.Metadata,
                QueryHelpers.FormatRfc3339Z(r.CreatedAt)))
                .ToArray();

            return Results.Ok(response);
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "audit_events_query_failed");
            return ApiErrorResults.Internal("failed to query audit events");
        }
    }

    private sealed record AuditEventRow(
        long Id, string RequestId, string ApplicationName,
        string EventType, string Severity, string? Metadata,
        DateTimeOffset CreatedAt);

    public sealed class AuditMarker { }
}
