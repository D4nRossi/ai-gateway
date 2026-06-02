using System.Data;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;

namespace APIGateway.Admin.Api.Features.Endpoints;

/// <summary>
/// GET <c>/admin/v1/endpoints</c> — lista todos os proxy endpoints.
/// **Sem** carregar targets (paridade Go: ListEndpoints comment "without
/// their target lists"). Use <see cref="Get"/> pra detalhe completo.
/// Requer role <c>operator</c>.
/// </summary>
public static class List
{
    public static RouteGroupBuilder MapEndpointsList(this RouteGroupBuilder group)
    {
        group.MapGet("/", HandleAsync)
            .WithName("EndpointsList")
            .WithTags("Endpoints")
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
            var rows = await conn.QueryAsync<EndpointRow>(
                new CommandDefinition(
                    EndpointSql.SelectAllColumns + " ORDER BY ep.name ASC;",
                    cancellationToken: ct));

            // targets vazio em cada response (paridade Go list mode).
            var response = rows
                .Select(r => EndpointMapper.ToResponse(r, Array.Empty<TargetRow>()))
                .ToArray();

            return Results.Ok(response);
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "endpoints_list_failed");
            return ApiErrorResults.Internal("failed to list endpoints");
        }
    }

    public sealed class ListMarker { }
}
