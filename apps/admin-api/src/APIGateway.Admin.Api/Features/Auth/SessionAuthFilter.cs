using System.Data;
using APIGateway.Admin.Api.Domain.Admin;
using APIGateway.Admin.Api.Infrastructure.Crypto;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;

namespace APIGateway.Admin.Api.Features.Auth;

/// <summary>
/// Endpoint filter que valida o token Bearer contra <c>gogateway.admin_sessions</c>
/// e injeta <see cref="AdminSession"/>/<see cref="AdminUser"/> no
/// <see cref="HttpContext.Items"/> sob as chaves <see cref="SessionItemKey"/>
/// e <see cref="UserItemKey"/>.
/// </summary>
/// <remarks>
/// Reasoning: paridade comportamental com
/// <c>internal/api/admin/middleware/session.go</c>. Hash do raw token é
/// calculado em memória; lookup é <c>SELECT</c> com filtro de
/// <c>revoked_at IS NULL</c> e <c>expires_at &gt; SYSUTCDATETIME()</c>,
/// que casa com o índice filtrado <c>idx_admin_sessions_token</c>.
///
/// References:
///   - ADR-0011 — opaque session tokens
///   - migrations/002_admin_auth.up.sql
/// </remarks>
public sealed class SessionAuthFilter : IEndpointFilter
{
    public const string SessionItemKey = "admin.session";
    public const string UserItemKey = "admin.user";

    public async ValueTask<object?> InvokeAsync(
        EndpointFilterInvocationContext ctx,
        EndpointFilterDelegate next)
    {
        var http = ctx.HttpContext;
        var connections = http.RequestServices.GetRequiredService<ConnectionFactory>();
        var logger = http.RequestServices.GetRequiredService<ILogger<SessionAuthFilter>>();

        var raw = ExtractBearer(http.Request.Headers["Authorization"].ToString());
        if (string.IsNullOrEmpty(raw))
        {
            return ApiErrorResults.Unauthorized("missing or invalid Authorization header");
        }

        var tokenHash = OpaqueTokenGenerator.Hash(raw);

        // Lookup atômico: session ativa + user ativo. Paridade com Go
        // ValidateSession que junta session + user num só round-trip.
        const string sql = """
            SELECT
                s.id            AS Id,
                s.admin_user_id AS AdminUserId,
                s.token_hash    AS TokenHash,
                s.expires_at    AS ExpiresAt,
                s.created_at    AS CreatedAt,
                s.revoked_at    AS RevokedAt,
                u.id            AS UserId,
                u.username      AS Username,
                u.password_hash AS PasswordHash,
                u.role          AS Role,
                u.active        AS Active,
                u.created_at    AS UserCreatedAt,
                u.updated_at    AS UserUpdatedAt
            FROM gogateway.admin_sessions AS s
            INNER JOIN gogateway.admin_users AS u ON u.id = s.admin_user_id
            WHERE s.token_hash = @TokenHash
              AND s.revoked_at IS NULL
              AND s.expires_at > SYSUTCDATETIME()
              AND u.active = 1;
            """;

        try
        {
            using IDbConnection conn = await connections.CreateOpenAsync(http.RequestAborted);
            var row = await conn.QuerySingleOrDefaultAsync<SessionUserRow>(
                new CommandDefinition(sql, new { TokenHash = tokenHash }, cancellationToken: http.RequestAborted));

            if (row is null)
            {
                return ApiErrorResults.Unauthorized("invalid or expired session token");
            }

            http.Items[SessionItemKey] = new AdminSession(
                row.Id, row.AdminUserId, row.TokenHash, row.ExpiresAt, row.CreatedAt, row.RevokedAt);
            http.Items[UserItemKey] = new AdminUser(
                row.UserId, row.Username, row.PasswordHash, row.Role, row.Active, row.UserCreatedAt, row.UserUpdatedAt);
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "admin_session_validation_error");
            return ApiErrorResults.Unauthorized("session validation error");
        }

        return await next(ctx);
    }

    /// <summary>Extrai o token de um header <c>Authorization: Bearer ...</c>.</summary>
    private static string ExtractBearer(string header)
    {
        const string scheme = "Bearer ";
        if (string.IsNullOrEmpty(header) || !header.StartsWith(scheme, StringComparison.Ordinal))
        {
            return string.Empty;
        }
        return header[scheme.Length..].Trim();
    }

    private sealed record SessionUserRow(
        long Id,
        long AdminUserId,
        string TokenHash,
        DateTimeOffset ExpiresAt,
        DateTimeOffset CreatedAt,
        DateTimeOffset? RevokedAt,
        long UserId,
        string Username,
        string PasswordHash,
        string Role,
        bool Active,
        DateTimeOffset UserCreatedAt,
        DateTimeOffset UserUpdatedAt);
}
