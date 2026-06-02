using System.Data;
using System.Text;
using System.Text.Json.Serialization;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;

namespace APIGateway.Admin.Api.Features.Observability;

/// <summary>
/// GET <c>/admin/v1/usage</c> — lista de <c>gogateway.usage_events</c> filtrada
/// por janela de tempo + application. Requer role <c>viewer</c>.
/// </summary>
/// <remarks>
/// Query params: <c>from</c>, <c>to</c> (RFC3339), <c>application</c>, <c>limit</c>
/// (default 100, max 1000). Paridade com <c>handlers/observability.go:ListUsageEvents</c>.
/// </remarks>
public static class Usage
{
    public static IEndpointRouteBuilder MapObservabilityUsage(this IEndpointRouteBuilder routes)
    {
        routes.MapGet("/usage", HandleAsync)
            .WithName("ObservabilityUsageList")
            .WithTags("Observability")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("viewer"));
        return routes;
    }

    public sealed record UsageEventResponse(
        [property: JsonPropertyName("id")]                  long Id,
        [property: JsonPropertyName("request_id")]          string RequestId,
        [property: JsonPropertyName("application_name")]    string ApplicationName,
        [property: JsonPropertyName("tier")]                string Tier,
        [property: JsonPropertyName("model")]               string Model,
        [property: JsonPropertyName("provider")]            string Provider,
        [property: JsonPropertyName("input_tokens")]        int? InputTokens,
        [property: JsonPropertyName("output_tokens")]       int? OutputTokens,
        [property: JsonPropertyName("total_tokens")]        int? TotalTokens,
        [property: JsonPropertyName("latency_ms")]          int LatencyMs,
        [property: JsonPropertyName("status_code")]         int StatusCode,
        [property: JsonPropertyName("estimated_cost_brl")]  decimal? EstimatedCostBrl,
        [property: JsonPropertyName("created_at")]          string CreatedAt);

    public static async Task<IResult> HandleAsync(
        HttpContext http,
        ConnectionFactory connections,
        ILogger<UsageMarker> logger)
    {
        var (error, from, to, limit) = QueryHelpers.ParseTimeRange(http.Request);
        if (error is not null) return error;

        var application = http.Request.Query["application"].ToString();
        var ct = http.RequestAborted;

        var sql = new StringBuilder("""
            SELECT id                  AS Id,
                   request_id          AS RequestId,
                   application_name    AS ApplicationName,
                   tier                AS Tier,
                   model               AS Model,
                   provider            AS Provider,
                   input_tokens        AS InputTokens,
                   output_tokens       AS OutputTokens,
                   total_tokens        AS TotalTokens,
                   latency_ms          AS LatencyMs,
                   status_code         AS StatusCode,
                   estimated_cost_brl  AS EstimatedCostBrl,
                   created_at          AS CreatedAt
            FROM gogateway.usage_events
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
        sql.Append(" ORDER BY created_at DESC OFFSET 0 ROWS FETCH NEXT @Limit ROWS ONLY;");
        parameters.Add("Limit", limit);

        try
        {
            using IDbConnection conn = await connections.CreateOpenAsync(ct);
            var rows = await conn.QueryAsync<UsageEventRow>(
                new CommandDefinition(sql.ToString(), parameters, cancellationToken: ct));

            var response = rows.Select(r => new UsageEventResponse(
                r.Id, r.RequestId, r.ApplicationName, r.Tier, r.Model, r.Provider,
                r.InputTokens, r.OutputTokens, r.TotalTokens,
                r.LatencyMs, r.StatusCode, r.EstimatedCostBrl,
                QueryHelpers.FormatRfc3339Z(r.CreatedAt)))
                .ToArray();

            return Results.Ok(response);
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "usage_events_query_failed");
            return ApiErrorResults.Internal("failed to query usage events");
        }
    }

    private sealed record UsageEventRow(
        long Id, string RequestId, string ApplicationName, string Tier, string Model, string Provider,
        int? InputTokens, int? OutputTokens, int? TotalTokens,
        int LatencyMs, int StatusCode, decimal? EstimatedCostBrl,
        DateTimeOffset CreatedAt);

    public sealed class UsageMarker { }
}
