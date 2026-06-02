using System.Data;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Infrastructure.Auditing;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;

namespace APIGateway.Admin.Api.Features.Users;

/// <summary>
/// DELETE <c>/admin/v1/users/{id}</c> — desativa o admin user
/// (<c>active = 0</c>) e revoga todas as sessions dele. Requer role <c>admin</c>.
/// </summary>
/// <remarks>
/// Paridade com <c>internal/api/admin/handlers/users.go:DeactivateUser</c>
/// e o servico <c>adminservice.go:240 DeactivateAdminUser</c>:
/// <list type="number">
///   <item>UPDATE active=0 com OUTPUT pra detectar miss (404)</item>
///   <item>Best-effort UPDATE em <c>admin_sessions</c> revogando sessoes vivas</item>
///   <item>Falha no revoke nao falha o request — sessions expiram pelo TTL</item>
/// </list>
///
/// Soft-delete; nao DELETE — preserva auditoria historica.
/// </remarks>
public static class Deactivate
{
    public static RouteGroupBuilder MapUsersDeactivate(this RouteGroupBuilder group)
    {
        group.MapDelete("/{id:long}", HandleAsync)
            .WithName("UsersDeactivate")
            .WithTags("Users")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("admin"));
        return group;
    }

    public static async Task<IResult> HandleAsync(
        long id,
        HttpContext http,
        ConnectionFactory connections,
        IAuditEventWriter audit,
        IConfiguration config,
        ILogger<DeactivateMarker> logger)
    {
        if (id <= 0)
        {
            return ApiErrorResults.BadRequest("invalid_param", "id must be a positive integer");
        }

        var ct = http.RequestAborted;

        try
        {
            using IDbConnection conn = await connections.CreateOpenAsync(ct);

            var affected = await conn.ExecuteAsync(
                new CommandDefinition(
                    """
                    UPDATE gogateway.admin_users
                    SET active = 0, updated_at = SYSUTCDATETIME()
                    WHERE id = @Id AND active = 1;
                    """,
                    new { Id = id },
                    cancellationToken: ct));

            if (affected == 0)
            {
                // Distinguir "ja inativo / desativado" de "nao existe": SELECT
                // por id valida a existencia (paridade Go: GetUser antes do
                // UpdateUser dispara ErrNotFound).
                var exists = await conn.ExecuteScalarAsync<int?>(
                    new CommandDefinition(
                        "SELECT 1 FROM gogateway.admin_users WHERE id = @Id;",
                        new { Id = id },
                        cancellationToken: ct));
                if (exists is null)
                {
                    return ApiErrorResults.Error(StatusCodes.Status404NotFound,
                        "not_found", "admin user not found");
                }
                // Existe mas ja estava inativo — idempotente, segue revoke + audit
            }

            // Best-effort: revoke sessions vivas. Log se falhar mas nao quebra
            // o request (paridade adminservice.go: logger.Warn).
            int sessionsRevoked = 0;
            try
            {
                sessionsRevoked = await conn.ExecuteAsync(
                    new CommandDefinition(
                        """
                        UPDATE gogateway.admin_sessions
                        SET revoked_at = SYSUTCDATETIME()
                        WHERE admin_user_id = @Id AND revoked_at IS NULL;
                        """,
                        new { Id = id },
                        cancellationToken: ct));
            }
            catch (Exception ex)
            {
                logger.LogWarning(ex,
                    "admin_user_revoke_sessions_failed user_id={UserId}", id);
            }

            await audit.WriteAsync(
                new AuditEvent(
                    RequestId: http.TraceIdentifier,
                    ApplicationName: config.GetValue("Audit:ApplicationName", "admin-api")!,
                    EventType: "admin_user_deactivated",
                    Severity: "info",
                    Metadata: new { user_id = id, sessions_revoked = sessionsRevoked }),
                ct);

            logger.LogInformation(
                "admin_user_deactivated user_id={UserId} sessions_revoked={Sessions}",
                id, sessionsRevoked);

            return Results.NoContent();
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "admin_user_deactivate_failed user_id={UserId}", id);
            return ApiErrorResults.Internal("failed to deactivate user");
        }
    }

    public sealed class DeactivateMarker { }
}
