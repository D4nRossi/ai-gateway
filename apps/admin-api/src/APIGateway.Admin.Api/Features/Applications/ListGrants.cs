using System.Data;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Features.Endpoints;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;

namespace APIGateway.Admin.Api.Features.Applications;

/// <summary>
/// GET <c>/admin/v1/applications/{id}/grants</c> — lista os
/// <see cref="EndpointResponse"/> que a application tem permissao de chamar.
/// Requer role <c>operator</c>.
/// </summary>
/// <remarks>
/// Paridade com <c>handlers/applications.go:ListGrants</c>.
///
/// O Go usa N+1 (comentario: aceito porque admin pages sao low-traffic e
/// grant count tipico &lt; 20). Aqui faco DUAS queries: 1 pros endpoints +
/// 1 pros targets ativos de todos os endpoints granted. Eh O(1) round-trips
/// vs O(N) do Go.
///
/// Retorna <c>[]</c> em vez de <c>null</c> quando nao ha grants.
/// </remarks>
public static class ListGrants
{
    public static RouteGroupBuilder MapApplicationsListGrants(this RouteGroupBuilder group)
    {
        group.MapGet("/{id:long}/grants", HandleAsync)
            .WithName("ApplicationsListGrants")
            .WithTags("Applications")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("operator"));
        return group;
    }

    public static async Task<IResult> HandleAsync(
        long id,
        HttpContext http,
        ConnectionFactory connections,
        ILogger<ListGrantsMarker> logger)
    {
        if (id <= 0)
        {
            return ApiErrorResults.BadRequest("invalid_param", "id must be a positive integer");
        }

        var ct = http.RequestAborted;

        try
        {
            using IDbConnection conn = await connections.CreateOpenAsync(ct);

            var endpoints = (await conn.QueryAsync<EndpointRow>(
                new CommandDefinition(
                    EndpointSql.SelectAllColumns + """
                    INNER JOIN gogateway.application_endpoint_grants AS g
                        ON g.endpoint_id = ep.id
                    WHERE g.application_id = @AppId
                    ORDER BY ep.name ASC;
                    """,
                    new { AppId = id },
                    cancellationToken: ct))).ToList();

            if (endpoints.Count == 0)
            {
                return Results.Ok(Array.Empty<EndpointResponse>());
            }

            var targets = (await conn.QueryAsync<TargetRow>(
                new CommandDefinition(
                    EndpointSql.SelectTargetsColumns + """
                    INNER JOIN gogateway.application_endpoint_grants AS g
                        ON g.endpoint_id = t.endpoint_id
                    WHERE g.application_id = @AppId AND t.active = 1
                    ORDER BY t.id ASC;
                    """,
                    new { AppId = id },
                    cancellationToken: ct))).ToList();

            var response = endpoints
                .Select(ep => EndpointMapper.ToResponse(ep, targets))
                .ToArray();

            return Results.Ok(response);
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "applications_list_grants_failed id={Id}", id);
            return ApiErrorResults.Internal("failed to list grants");
        }
    }

    public sealed class ListGrantsMarker { }
}
