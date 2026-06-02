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
/// POST <c>/admin/v1/endpoints</c> — cria proxy endpoint sem targets.
/// Targets entram via <c>POST /endpoints/{id}/targets</c> (proxima slice).
/// Requer role <c>operator</c>.
/// </summary>
/// <remarks>
/// Paridade com <c>handlers/endpoints.go:CreateEndpoint</c> e
/// <c>adminservice.go:373 CreateEndpoint</c>:
/// <list type="number">
///   <item>Valida body (slug e name obrigatorios)</item>
///   <item>Aplica defaults: provider_kind = "custom", lb_strategy = "round_robin"</item>
///   <item>Valida <c>provider_kind</c> in enum (ADR-0016)</item>
///   <item>Valida <c>lb_strategy</c> in enum (CHECK migration 004)</item>
///   <item>Valida <c>provider_config</c> per kind (ADR-0017, <see cref="ProviderConfigValidator"/>)</item>
///   <item>INSERT com OUTPUT — devolve 201 com <see cref="EndpointResponse"/>, targets vazio</item>
/// </list>
///
/// Slug duplicado -> 409 (<c>uq_proxy_endpoints_slug</c>).
/// </remarks>
public static class Create
{
    public static RouteGroupBuilder MapEndpointsCreate(this RouteGroupBuilder group)
    {
        group.MapPost("/", HandleAsync)
            .WithName("EndpointsCreate")
            .WithTags("Endpoints")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("operator"));
        return group;
    }

    public sealed record CreateRequest(
        string? Slug,
        string? Name,
        string? ProviderKind,
        JsonElement? ProviderConfig,
        string? LbStrategy,
        int MaxRps,
        long MaxMonthlyRequests);

    public static async Task<IResult> HandleAsync(
        CreateRequest req,
        HttpContext http,
        ConnectionFactory connections,
        IAuditEventWriter audit,
        IConfiguration config,
        ILogger<CreateMarker> logger)
    {
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
            var row = await conn.QuerySingleAsync<EndpointRow>(
                new CommandDefinition(
                    """
                    INSERT INTO gogateway.proxy_endpoints
                        (slug, name, provider_kind, provider_config,
                         lb_strategy, max_rps, max_monthly_requests, active)
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
                    VALUES
                        (@Slug, @Name, @ProviderKind, @ProviderConfig,
                         @LbStrategy, @MaxRps, @MaxMonthlyRequests, 1);
                    """,
                    new
                    {
                        req.Slug,
                        req.Name,
                        ProviderKind = providerKind,
                        ProviderConfig = providerConfigJson,
                        LbStrategy = lbStrategy,
                        req.MaxRps,
                        req.MaxMonthlyRequests,
                    },
                    cancellationToken: ct));

            await audit.WriteAsync(
                new AuditEvent(
                    RequestId: http.TraceIdentifier,
                    ApplicationName: config.GetValue("Audit:ApplicationName", "admin-api")!,
                    EventType: "endpoint_created",
                    Severity: "info",
                    Metadata: new { endpoint_id = row.Id, row.Slug, row.Name, row.ProviderKind }),
                ct);

            logger.LogInformation("endpoint_created id={Id} slug={Slug} provider_kind={Kind}",
                row.Id, row.Slug, providerKind);

            return Results.Created($"/admin/v1/endpoints/{row.Id}",
                EndpointMapper.ToResponse(row, Array.Empty<TargetRow>()));
        }
        catch (SqlException ex) when (ex.Number is 2627 or 2601)
        {
            return ApiErrorResults.Error(StatusCodes.Status409Conflict,
                "conflict", "slug already exists", ex.Message);
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "endpoint_create_failed slug={Slug}", req.Slug);
            return ApiErrorResults.Internal("falha ao criar endpoint", ex.Message);
        }
    }

    public sealed class CreateMarker { }
}
