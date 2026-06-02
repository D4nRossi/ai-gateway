using System.Data;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;

namespace APIGateway.Admin.Api.Features.Endpoints;

/// <summary>
/// GET <c>/admin/v1/endpoints/{id}</c> — devolve endpoint COM seus targets
/// ativos. 404 se nao existe. Requer role <c>operator</c>.
/// </summary>
public static class Get
{
    public static RouteGroupBuilder MapEndpointsGet(this RouteGroupBuilder group)
    {
        group.MapGet("/{id:long}", HandleAsync)
            .WithName("EndpointsGet")
            .WithTags("Endpoints")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("operator"));
        return group;
    }

    public static async Task<IResult> HandleAsync(
        long id,
        HttpContext http,
        ConnectionFactory connections,
        ILogger<GetMarker> logger)
    {
        if (id <= 0)
        {
            return ApiErrorResults.BadRequest("invalid_param", "id must be a positive integer");
        }

        var ct = http.RequestAborted;

        try
        {
            using IDbConnection conn = await connections.CreateOpenAsync(ct);

            var endpoint = await conn.QuerySingleOrDefaultAsync<EndpointRow>(
                new CommandDefinition(
                    EndpointSql.SelectAllColumns + " WHERE ep.id = @Id;",
                    new { Id = id },
                    cancellationToken: ct));

            if (endpoint is null)
            {
                return ApiErrorResults.Error(StatusCodes.Status404NotFound,
                    "not_found", "endpoint not found");
            }

            var targets = await conn.QueryAsync<TargetRow>(
                new CommandDefinition(
                    EndpointSql.SelectTargetsColumns +
                    " WHERE t.endpoint_id = @Id AND t.active = 1 ORDER BY t.id ASC;",
                    new { Id = id },
                    cancellationToken: ct));

            return Results.Ok(EndpointMapper.ToResponse(endpoint, targets));
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "endpoints_get_failed id={Id}", id);
            return ApiErrorResults.Internal("failed to get endpoint");
        }
    }

    public sealed class GetMarker { }
}
