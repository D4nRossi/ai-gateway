using System.Data;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;

namespace APIGateway.Admin.Api.Features.Applications;

/// <summary>
/// GET <c>/admin/v1/applications/{id}</c> — busca application por ID.
/// Requer role <c>operator</c>.
/// </summary>
public static class Get
{
    public static RouteGroupBuilder MapApplicationsGet(this RouteGroupBuilder group)
    {
        group.MapGet("/{id:long}", HandleAsync)
            .WithName("ApplicationsGet")
            .WithTags("Applications")
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
            var row = await conn.QuerySingleOrDefaultAsync<ApplicationRow>(
                new CommandDefinition(ApplicationSql.SelectById, new { Id = id }, cancellationToken: ct));

            if (row is null)
            {
                return ApiErrorResults.Error(StatusCodes.Status404NotFound,
                    "not_found", "application not found");
            }

            return Results.Ok(ApplicationMapper.ToResponse(row));
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "applications_get_failed id={Id}", id);
            return ApiErrorResults.Internal("failed to get application");
        }
    }

    public sealed class GetMarker { }
}
