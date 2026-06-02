using System.Data;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Infrastructure.Auditing;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;
using Microsoft.Data.SqlClient;

namespace APIGateway.Admin.Api.Features.Applications;

/// <summary>
/// PUT <c>/admin/v1/applications/{id}</c> — substituicao full-fields da
/// application. Requer role <c>operator</c>.
/// </summary>
/// <remarks>
/// Paridade com <c>handlers/applications.go:UpdateApplication</c>. Nao mexe
/// em api_keys (rotacao tem endpoint proprio: <c>rotate-key</c>). Caller
/// passa todos os campos; PATCH parcial nao eh suportado (paridade Go).
/// </remarks>
public static class Update
{
    public static RouteGroupBuilder MapApplicationsUpdate(this RouteGroupBuilder group)
    {
        group.MapPut("/{id:long}", HandleAsync)
            .WithName("ApplicationsUpdate")
            .WithTags("Applications")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("operator"));
        return group;
    }

    public sealed record UpdateRequest(
        string? Name,
        string? Tier,
        IReadOnlyList<string>? AllowedModels,
        bool StreamingAllowed,
        int MaxRpm,
        int MaxTpm,
        decimal MonthlyBudgetBrl,
        bool Active);

    public static async Task<IResult> HandleAsync(
        long id,
        UpdateRequest req,
        HttpContext http,
        ConnectionFactory connections,
        IAuditEventWriter audit,
        IConfiguration config,
        ILogger<UpdateMarker> logger)
    {
        if (id <= 0)
        {
            return ApiErrorResults.BadRequest("invalid_param", "id must be a positive integer");
        }
        if (string.IsNullOrWhiteSpace(req.Name))
        {
            return ApiErrorResults.BadRequest("bad_request", "name is required");
        }

        var tier = (req.Tier ?? string.Empty).Trim().ToLowerInvariant();
        if (tier is not ("tier_1" or "tier_2" or "tier_3"))
        {
            return ApiErrorResults.BadRequest("invalid_tier",
                "tier must be tier_1, tier_2, or tier_3");
        }

        var ct = http.RequestAborted;
        var allowedModelsJson = ApplicationMapper.SerializeModels(req.AllowedModels);

        try
        {
            using IDbConnection conn = await connections.CreateOpenAsync(ct);

            var updated = await conn.QuerySingleOrDefaultAsync<ApplicationRow>(
                new CommandDefinition(
                    """
                    UPDATE gogateway.applications
                    SET name = @Name,
                        tier = @Tier,
                        allowed_models = @AllowedModels,
                        streaming_allowed = @StreamingAllowed,
                        max_rpm = @MaxRpm,
                        max_tpm = @MaxTpm,
                        monthly_budget_brl = @MonthlyBudgetBrl,
                        active = @Active,
                        updated_at = SYSUTCDATETIME()
                    OUTPUT
                        INSERTED.id                  AS Id,
                        INSERTED.name                AS Name,
                        INSERTED.tier                AS Tier,
                        INSERTED.allowed_models      AS AllowedModelsJson,
                        INSERTED.streaming_allowed   AS StreamingAllowed,
                        INSERTED.max_rpm             AS MaxRpm,
                        INSERTED.max_tpm             AS MaxTpm,
                        INSERTED.monthly_budget_brl  AS MonthlyBudgetBrl,
                        INSERTED.active              AS Active,
                        INSERTED.created_at          AS CreatedAt,
                        INSERTED.updated_at          AS UpdatedAt
                    WHERE id = @Id;
                    """,
                    new
                    {
                        Id = id,
                        req.Name,
                        Tier = tier,
                        AllowedModels = allowedModelsJson,
                        req.StreamingAllowed,
                        req.MaxRpm,
                        req.MaxTpm,
                        req.MonthlyBudgetBrl,
                        Active = req.Active ? 1 : 0,
                    },
                    cancellationToken: ct));

            if (updated is null)
            {
                return ApiErrorResults.Error(StatusCodes.Status404NotFound,
                    "not_found", "aplicação não encontrada");
            }

            await audit.WriteAsync(
                new AuditEvent(
                    RequestId: http.TraceIdentifier,
                    ApplicationName: config.GetValue("Audit:ApplicationName", "admin-api")!,
                    EventType: "application_updated",
                    Severity: "info",
                    Metadata: new { app_id = id, name = updated.Name, tier, active = req.Active }),
                ct);

            logger.LogInformation("application_updated app_id={Id} name={Name}", id, updated.Name);
            return Results.Ok(ApplicationMapper.ToResponse(updated));
        }
        catch (SqlException ex) when (ex.Number is 2627 or 2601)
        {
            return ApiErrorResults.Error(StatusCodes.Status409Conflict,
                "conflict", "application name already exists", ex.Message);
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "application_update_failed id={Id}", id);
            return ApiErrorResults.Internal("falha ao atualizar aplicação", ex.Message);
        }
    }

    public sealed class UpdateMarker { }
}
