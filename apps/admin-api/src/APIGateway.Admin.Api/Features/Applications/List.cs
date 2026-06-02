using System.Data;
using System.Text.Json;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;

namespace APIGateway.Admin.Api.Features.Applications;

/// <summary>
/// GET <c>/admin/v1/applications</c> — lista todas as applications. Requer role <c>operator</c>.
/// </summary>
public static class List
{
    public static RouteGroupBuilder MapApplicationsList(this RouteGroupBuilder group)
    {
        group.MapGet("/", HandleAsync)
            .WithName("ApplicationsList")
            .WithTags("Applications")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("operator"));
        return group;
    }

    public static async Task<IResult> HandleAsync(
        HttpContext http,
        ConnectionFactory connections,
        ILogger<ListMarker> logger)
    {
        var ct = http.RequestAborted;

        try
        {
            using IDbConnection conn = await connections.CreateOpenAsync(ct);
            var rows = await conn.QueryAsync<ApplicationRow>(
                new CommandDefinition(
                    ApplicationSql.SelectAll,
                    cancellationToken: ct));

            var response = rows.Select(ApplicationMapper.ToResponse).ToArray();
            return Results.Ok(response);
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "applications_list_failed");
            return ApiErrorResults.Internal("failed to list applications");
        }
    }

    public sealed class ListMarker { }
}

/// <summary>
/// Linha bruta retornada por Dapper. <c>AllowedModelsJson</c> guarda o
/// NVARCHAR(MAX) com array JSON — eh decodificado em <see cref="ApplicationMapper"/>.
/// </summary>
internal sealed record ApplicationRow(
    long Id,
    string Name,
    string Tier,
    string AllowedModelsJson,
    bool StreamingAllowed,
    int MaxRpm,
    int MaxTpm,
    decimal MonthlyBudgetBrl,
    bool Active,
    DateTimeOffset CreatedAt,
    DateTimeOffset UpdatedAt);

internal static class ApplicationSql
{
    public const string SelectAll = """
        SELECT
            id                  AS Id,
            name                AS Name,
            tier                AS Tier,
            allowed_models      AS AllowedModelsJson,
            streaming_allowed   AS StreamingAllowed,
            max_rpm             AS MaxRpm,
            max_tpm             AS MaxTpm,
            monthly_budget_brl  AS MonthlyBudgetBrl,
            active              AS Active,
            created_at          AS CreatedAt,
            updated_at          AS UpdatedAt
        FROM gogateway.applications
        ORDER BY name ASC;
        """;

    public const string SelectById = """
        SELECT
            id                  AS Id,
            name                AS Name,
            tier                AS Tier,
            allowed_models      AS AllowedModelsJson,
            streaming_allowed   AS StreamingAllowed,
            max_rpm             AS MaxRpm,
            max_tpm             AS MaxTpm,
            monthly_budget_brl  AS MonthlyBudgetBrl,
            active              AS Active,
            created_at          AS CreatedAt,
            updated_at          AS UpdatedAt
        FROM gogateway.applications
        WHERE id = @Id;
        """;
}

internal static class ApplicationMapper
{
    public static ApplicationResponse ToResponse(ApplicationRow row)
    {
        var models = DeserializeModels(row.AllowedModelsJson);
        return new ApplicationResponse(
            row.Id,
            row.Name,
            row.Tier,
            models,
            row.StreamingAllowed,
            row.MaxRpm,
            row.MaxTpm,
            row.MonthlyBudgetBrl,
            row.Active,
            row.CreatedAt,
            row.UpdatedAt);
    }

    public static string[] DeserializeModels(string json)
    {
        if (string.IsNullOrWhiteSpace(json))
        {
            return Array.Empty<string>();
        }
        try
        {
            return JsonSerializer.Deserialize<string[]>(json) ?? Array.Empty<string>();
        }
        catch (JsonException)
        {
            // CHECK constraint ISJSON garante que sempre eh JSON valido,
            // mas defensivamente retorna vazio em vez de quebrar a request.
            return Array.Empty<string>();
        }
    }

    public static string SerializeModels(IReadOnlyList<string>? models)
    {
        return JsonSerializer.Serialize(models ?? Array.Empty<string>());
    }
}
