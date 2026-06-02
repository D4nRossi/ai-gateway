using System.Data;
using System.Text.Json;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Infrastructure.Auditing;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;
using Microsoft.Data.SqlClient;

namespace APIGateway.Admin.Api.Features.Endpoints;

/// <summary>
/// PUT <c>/admin/v1/endpoints/{id}</c> — full-fields replacement.
/// Requer role <c>operator</c>.
/// </summary>
/// <remarks>
/// Paridade com <c>handlers/endpoints.go:UpdateEndpoint</c>. Mesmo conjunto
/// de validacoes do <see cref="Create"/>. Nao mexe em targets — targets tem
/// endpoints proprios pra add/update/delete.
///
/// Retorna o endpoint atualizado COM seus targets ativos (paridade Go que
/// reusa toEndpointResponse e o repository Update carrega targets).
/// </remarks>
public static class Update
{
    public static RouteGroupBuilder MapEndpointsUpdate(this RouteGroupBuilder group)
    {
        group.MapPut("/{id:long}", HandleAsync)
            .WithName("EndpointsUpdate")
            .WithTags("Endpoints")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("operator"));
        return group;
    }

    public sealed record UpdateRequest(
        string? Slug,
        string? Name,
        string? ProviderKind,
        JsonElement? ProviderConfig,
        string? LbStrategy,
        int MaxRps,
        long MaxMonthlyRequests,
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
        if (string.IsNullOrWhiteSpace(req.Slug) || string.IsNullOrWhiteSpace(req.Name))
        {
            return ApiErrorResults.BadRequest("bad_request", "slug and name are required");
        }

        var providerKind = string.IsNullOrEmpty(req.ProviderKind)
            ? EndpointConstants.DefaultProviderKind
            : req.ProviderKind;
        if (!EndpointConstants.ValidProviderKinds.Contains(providerKind))
        {
            return ApiErrorResults.BadRequest("invalid_provider", "provider_kind inválido");
        }

        var lbStrategy = string.IsNullOrEmpty(req.LbStrategy)
            ? EndpointConstants.DefaultLbStrategy
            : req.LbStrategy;
        if (!EndpointConstants.ValidLbStrategies.Contains(lbStrategy))
        {
            return ApiErrorResults.BadRequest("invalid_lb_strategy", "lb_strategy inválido");
        }

        var validation = ProviderConfigValidator.Validate(providerKind, req.ProviderConfig);
        if (!validation.Ok)
        {
            return ApiErrorResults.BadRequest("invalid_provider_config", validation.ErrorMessage!);
        }

        var providerConfigJson = req.ProviderConfig is JsonElement el && el.ValueKind == JsonValueKind.Object
            ? el.GetRawText()
            : "{}";

        var ct = http.RequestAborted;

        try
        {
            using IDbConnection conn = await connections.CreateOpenAsync(ct);

            var updated = await conn.QuerySingleOrDefaultAsync<EndpointRow>(
                new CommandDefinition(
                    """
                    UPDATE gogateway.proxy_endpoints
                    SET slug = @Slug,
                        name = @Name,
                        provider_kind = @ProviderKind,
                        provider_config = @ProviderConfig,
                        lb_strategy = @LbStrategy,
                        max_rps = @MaxRps,
                        max_monthly_requests = @MaxMonthlyRequests,
                        active = @Active,
                        updated_at = SYSUTCDATETIME()
                    OUTPUT
                        INSERTED.id                    AS Id,
                        INSERTED.slug                  AS Slug,
                        INSERTED.name                  AS Name,
                        INSERTED.provider_kind         AS ProviderKind,
                        INSERTED.provider_config       AS ProviderConfigJson,
                        INSERTED.lb_strategy           AS LbStrategy,
                        INSERTED.max_rps               AS MaxRps,
                        INSERTED.max_monthly_requests  AS MaxMonthlyRequests,
                        INSERTED.active                AS Active,
                        INSERTED.created_at            AS CreatedAt,
                        INSERTED.updated_at            AS UpdatedAt
                    WHERE id = @Id;
                    """,
                    new
                    {
                        Id = id,
                        req.Slug,
                        req.Name,
                        ProviderKind = providerKind,
                        ProviderConfig = providerConfigJson,
                        LbStrategy = lbStrategy,
                        req.MaxRps,
                        req.MaxMonthlyRequests,
                        Active = req.Active ? 1 : 0,
                    },
                    cancellationToken: ct));

            if (updated is null)
            {
                return ApiErrorResults.Error(StatusCodes.Status404NotFound,
                    "not_found", "endpoint not found");
            }

            // Carrega targets ativos pra paridade com Go (que devolve com targets).
            var targets = await conn.QueryAsync<TargetRow>(
                new CommandDefinition(
                    EndpointSql.SelectTargetsColumns +
                    " WHERE t.endpoint_id = @Id AND t.active = 1 ORDER BY t.id ASC;",
                    new { Id = id },
                    cancellationToken: ct));

            await audit.WriteAsync(
                new AuditEvent(
                    RequestId: http.TraceIdentifier,
                    ApplicationName: config.GetValue("Audit:ApplicationName", "admin-api")!,
                    EventType: "endpoint_updated",
                    Severity: "info",
                    Metadata: new { endpoint_id = id, updated.Slug, updated.Name, updated.ProviderKind, active = req.Active }),
                ct);

            logger.LogInformation("endpoint_updated id={Id} slug={Slug}", id, updated.Slug);
            return Results.Ok(EndpointMapper.ToResponse(updated, targets));
        }
        catch (SqlException ex) when (ex.Number is 2627 or 2601)
        {
            return ApiErrorResults.Error(StatusCodes.Status409Conflict,
                "conflict", "slug already exists", ex.Message);
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "endpoint_update_failed id={Id}", id);
            return ApiErrorResults.Internal("falha ao atualizar endpoint", ex.Message);
        }
    }

    public sealed class UpdateMarker { }
}
