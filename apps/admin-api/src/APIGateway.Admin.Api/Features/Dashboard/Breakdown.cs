using System.Data;
using System.Globalization;
using System.Text.Json.Serialization;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Features.Observability;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;

namespace APIGateway.Admin.Api.Features.Dashboard;

/// <summary>
/// GET <c>/admin/v1/dashboard/breakdown</c> — agrega usage_events por dimensao
/// (application, tier, model) pra grafico pie/bar.
/// Requer role <c>viewer</c>.
/// </summary>
/// <remarks>
/// Paridade com <c>handlers/dashboard.go:DashboardBreakdown</c>. Dimension
/// mapeia pra coluna via allowlist (anti-injection). Ordenacao: total_cost DESC,
/// request_count DESC — paridade Go (top spenders primeiro pro
/// <c>application</c>; tier/model cardinalidade baixa, ordem ainda util).
///
/// Limit: 1..50 com fallback silencioso pra 10 (paridade
/// <c>parsePositiveIntDefault</c> Go).
/// </remarks>
public static class Breakdown
{
    private const int LimitMax = 50;
    private const int LimitDefault = 10;

    public static IEndpointRouteBuilder MapDashboardBreakdown(this IEndpointRouteBuilder routes)
    {
        routes.MapGet("/dashboard/breakdown", HandleAsync)
            .WithName("DashboardBreakdown")
            .WithTags("Dashboard")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("viewer"));
        return routes;
    }

    public sealed record BreakdownRow(
        [property: JsonPropertyName("key")]              string Key,
        [property: JsonPropertyName("request_count")]    long RequestCount,
        [property: JsonPropertyName("total_tokens")]     long TotalTokens,
        [property: JsonPropertyName("total_cost_brl")]   decimal TotalCostBrl);

    public static async Task<IResult> HandleAsync(
        HttpContext http,
        ConnectionFactory connections,
        ILogger<BreakdownMarker> logger)
    {
        var (error, from, to, _) = QueryHelpers.ParseTimeRange(http.Request);
        if (error is not null) return error;

        var dimension = http.Request.Query["dimension"].ToString();
        if (string.IsNullOrEmpty(dimension)) dimension = "application";

        var column = ResolveDimensionColumn(dimension);
        if (column is null)
        {
            return ApiErrorResults.BadRequest("invalid_param",
                "dimension must be one of: application, tier, model");
        }

        // Limit silencioso: tenta parsear; se invalido/fora-de-range, usa default.
        var limit = LimitDefault;
        var limitStr = http.Request.Query["limit"].ToString();
        if (!string.IsNullOrEmpty(limitStr)
            && int.TryParse(limitStr, NumberStyles.Integer, CultureInfo.InvariantCulture, out var n)
            && n >= 1 && n <= LimitMax)
        {
            limit = n;
        }

        // column vem do allowlist (ResolveDimensionColumn) — seguro embeddar.
        var sql = $"""
            SELECT TOP (@Limit)
                {column}                                AS [Key],
                COUNT(*)                                AS RequestCount,
                COALESCE(SUM(CAST(total_tokens AS BIGINT)), 0) AS TotalTokens,
                COALESCE(SUM(estimated_cost_brl), 0)    AS TotalCostBrl
            FROM gogateway.usage_events
            WHERE created_at BETWEEN @From AND @To
              AND {column} IS NOT NULL
              AND {column} <> ''
            GROUP BY {column}
            ORDER BY TotalCostBrl DESC, RequestCount DESC;
            """;

        var ct = http.RequestAborted;

        try
        {
            using IDbConnection conn = await connections.CreateOpenAsync(ct);
            var rows = await conn.QueryAsync<BreakdownRow>(
                new CommandDefinition(
                    sql,
                    new { Limit = limit, From = from, To = to },
                    cancellationToken: ct));

            return Results.Ok(rows.ToArray());
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "dashboard_breakdown_failed dimension={Dimension}", dimension);
            return ApiErrorResults.Internal("failed to query breakdown");
        }
    }

    /// <summary>
    /// Allowlist <c>dimension</c> → coluna SQL. Retorna <see langword="null"/>
    /// pra valores nao reconhecidos (paridade <c>dimensionColumn</c> Go).
    /// </summary>
    private static string? ResolveDimensionColumn(string dimension) => dimension switch
    {
        "application" => "application_name",
        "tier"        => "tier",
        "model"       => "model",
        _             => null,
    };

    public sealed class BreakdownMarker { }
}
