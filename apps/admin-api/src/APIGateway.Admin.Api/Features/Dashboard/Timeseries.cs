using System.Data;
using System.Text.Json.Serialization;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Features.Observability;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;

namespace APIGateway.Admin.Api.Features.Dashboard;

/// <summary>
/// GET <c>/admin/v1/dashboard/timeseries</c> — agrega usage_events em buckets
/// horarios ou diarios pra grafico de linha/area. Requer role <c>viewer</c>.
/// </summary>
/// <remarks>
/// Paridade com <c>handlers/dashboard.go:DashboardTimeseries</c>. T-SQL trunca
/// timestamp via <c>DATEADD(unit, DATEDIFF(unit, 0, created_at), 0)</c>.
/// Buckets vazios NAO retornam — frontend interpola via recharts.
///
/// V1 emite <c>avg_latency_ms</c> + <c>max_latency_ms</c>. Percentis p50/p95/p99
/// ficam como follow-up (PERCENTILE_CONT exige subquery por percentil).
/// </remarks>
public static class Timeseries
{
    public static IEndpointRouteBuilder MapDashboardTimeseries(this IEndpointRouteBuilder routes)
    {
        routes.MapGet("/dashboard/timeseries", HandleAsync)
            .WithName("DashboardTimeseries")
            .WithTags("Dashboard")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("viewer"));
        return routes;
    }

    public sealed record TimeseriesPoint(
        [property: JsonPropertyName("bucket_start")]      string BucketStart,
        [property: JsonPropertyName("request_count")]     long RequestCount,
        [property: JsonPropertyName("avg_latency_ms")]    double AvgLatencyMs,
        [property: JsonPropertyName("max_latency_ms")]    int MaxLatencyMs,
        [property: JsonPropertyName("total_tokens")]      long TotalTokens,
        [property: JsonPropertyName("total_cost_brl")]    decimal TotalCostBrl,
        [property: JsonPropertyName("error_count_4xx")]   long ErrorCount4xx,
        [property: JsonPropertyName("error_count_5xx")]   long ErrorCount5xx);

    public static async Task<IResult> HandleAsync(
        HttpContext http,
        ConnectionFactory connections,
        ILogger<TimeseriesMarker> logger)
    {
        var (error, from, to, _) = QueryHelpers.ParseTimeRange(http.Request);
        if (error is not null) return error;

        var bucket = http.Request.Query["bucket"].ToString().ToLowerInvariant();
        if (string.IsNullOrEmpty(bucket)) bucket = "hour";
        if (bucket is not ("hour" or "day"))
        {
            return ApiErrorResults.BadRequest("invalid_param",
                "bucket must be 'hour' or 'day'");
        }

        // bucket vem do allowlist acima — embeddar no SQL eh seguro.
        var sql = $"""
            SELECT
                DATEADD({bucket}, DATEDIFF({bucket}, 0, created_at), 0) AS BucketStart,
                COUNT(*)                                AS RequestCount,
                AVG(CAST(latency_ms AS FLOAT))          AS AvgLatencyMs,
                MAX(latency_ms)                         AS MaxLatencyMs,
                COALESCE(SUM(CAST(total_tokens AS BIGINT)), 0) AS TotalTokens,
                COALESCE(SUM(estimated_cost_brl), 0)    AS TotalCostBrl,
                SUM(CASE WHEN status_code BETWEEN 400 AND 499 THEN 1 ELSE 0 END) AS ErrorCount4xx,
                SUM(CASE WHEN status_code >= 500             THEN 1 ELSE 0 END) AS ErrorCount5xx
            FROM gogateway.usage_events
            WHERE created_at BETWEEN @From AND @To
            GROUP BY DATEADD({bucket}, DATEDIFF({bucket}, 0, created_at), 0)
            ORDER BY BucketStart ASC;
            """;

        var ct = http.RequestAborted;

        try
        {
            using IDbConnection conn = await connections.CreateOpenAsync(ct);
            var rows = await conn.QueryAsync<TimeseriesRow>(
                new CommandDefinition(sql, new { From = from, To = to }, cancellationToken: ct));

            var response = rows.Select(r => new TimeseriesPoint(
                QueryHelpers.FormatRfc3339Z(r.BucketStart),
                r.RequestCount, r.AvgLatencyMs, r.MaxLatencyMs,
                r.TotalTokens, r.TotalCostBrl,
                r.ErrorCount4xx, r.ErrorCount5xx))
                .ToArray();

            return Results.Ok(response);
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "dashboard_timeseries_failed");
            return ApiErrorResults.Internal("failed to query timeseries");
        }
    }

    private sealed record TimeseriesRow(
        DateTimeOffset BucketStart,
        long RequestCount,
        double AvgLatencyMs,
        int MaxLatencyMs,
        long TotalTokens,
        decimal TotalCostBrl,
        long ErrorCount4xx,
        long ErrorCount5xx);

    public sealed class TimeseriesMarker { }
}
