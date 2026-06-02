using System.Data;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;

namespace APIGateway.Admin.Api.Features.Users;

/// <summary>
/// GET <c>/admin/v1/users</c> — lista todos os admin users (ativos e inativos).
/// Requer role <c>admin</c>.
/// </summary>
/// <remarks>
/// Paridade exata com <c>internal/api/admin/handlers/users.go:ListUsers</c>.
/// Response shape: array de <see cref="AdminUserResponse"/> sem password_hash.
/// </remarks>
public static class List
{
    public static RouteGroupBuilder MapList(this RouteGroupBuilder group)
    {
        group.MapGet("/", HandleAsync)
            .WithName("UsersList")
            .WithTags("Users")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("admin"));
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
            var rows = await conn.QueryAsync<AdminUserResponse>(
                new CommandDefinition(
                    """
                    SELECT
                        id            AS id,
                        username      AS username,
                        role          AS role,
                        active        AS active,
                        created_at    AS created_at,
                        updated_at    AS updated_at
                    FROM gogateway.admin_users
                    ORDER BY id ASC;
                    """,
                    cancellationToken: ct));

            // Paridade com Go: handler retorna [] mesmo quando vazio (nao null).
            return Results.Ok(rows.ToArray());
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "admin_users_list_failed");
            return ApiErrorResults.Internal("failed to list users");
        }
    }

    public sealed class ListMarker { }
}
