using System.Data;
using System.Globalization;
using System.Text;
using System.Text.Json.Serialization;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;

namespace APIGateway.Admin.Api.Features.Observability;

/// <summary>
/// GET <c>/admin/v1/budget</c> — contadores mensais por application.
/// Params: <c>period</c> (YYYYMM, default mes corrente UTC),
/// <c>application</c> (opcional). Requer role <c>viewer</c>.
/// </summary>
/// <remarks>
/// Paridade com <c>handlers/observability.go:ListBudget</c>. JSON usa key
/// <c>period</c> (nao <c>period_yyyymm</c>) — paridade <c>budgetRow</c> Go.
/// </remarks>
public static class Budget
{
    public static IEndpointRouteBuilder MapObservabilityBudget(this IEndpointRouteBuilder routes)
    {
        routes.MapGet("/budget", HandleAsync)
            .WithName("ObservabilityBudgetList")
            .WithTags("Observability")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("viewer"));
        return routes;
    }

    public sealed record BudgetResponse(
        [property: JsonPropertyName("application_name")]    string ApplicationName,
        [property: JsonPropertyName("period")]              string Period,
        [property: JsonPropertyName("total_requests")]      long TotalRequests,
        [property: JsonPropertyName("total_tokens")]        long TotalTokens,
        [property: JsonPropertyName("estimated_cost_brl")]  decimal EstimatedCostBrl,
        [property: JsonPropertyName("updated_at")]          string UpdatedAt);

    public static async Task<IResult> HandleAsync(
        HttpContext http,
        ConnectionFactory connections,
        ILogger<BudgetMarker> logger)
    {
        var period = http.Request.Query["period"].ToString();
        if (string.IsNullOrEmpty(period))
        {
            period = DateTime.UtcNow.ToString("yyyyMM", CultureInfo.InvariantCulture);
        }

        var application = http.Request.Query["application"].ToString();
        var ct = http.RequestAborted;

        var sql = new StringBuilder("""
            SELECT application_name    AS ApplicationName,
                   period_yyyymm       AS Period,
                   total_requests      AS TotalRequests,
                   total_tokens        AS TotalTokens,
                   estimated_cost_brl  AS EstimatedCostBrl,
                   updated_at          AS UpdatedAt
            FROM gogateway.budget_counters
            WHERE period_yyyymm = @Period
            """);
        var parameters = new DynamicParameters();
        parameters.Add("Period", period);

        if (!string.IsNullOrEmpty(application))
        {
            sql.Append(" AND application_name = @Application");
            parameters.Add("Application", application);
        }
        sql.Append(" ORDER BY application_name;");

        try
        {
            using IDbConnection conn = await connections.CreateOpenAsync(ct);
            var rows = await conn.QueryAsync<BudgetRow>(
                new CommandDefinition(sql.ToString(), parameters, cancellationToken: ct));

            var response = rows.Select(r => new BudgetResponse(
                r.ApplicationName, r.Period,
                r.TotalRequests, r.TotalTokens, r.EstimatedCostBrl,
                QueryHelpers.FormatRfc3339Z(r.UpdatedAt)))
                .ToArray();

            return Results.Ok(response);
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "budget_query_failed");
            return ApiErrorResults.Internal("failed to query budget");
        }
    }

    private sealed record BudgetRow(
        string ApplicationName, string Period,
        long TotalRequests, long TotalTokens, decimal EstimatedCostBrl,
        DateTimeOffset UpdatedAt);

    public sealed class BudgetMarker { }
}
