using APIGateway.Admin.Api.Domain.Admin;
using APIGateway.Admin.Api.Infrastructure.Auditing;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;

namespace APIGateway.Admin.Api.Features.Auth;

/// <summary>
/// DELETE <c>/admin/v1/auth/logout</c> — revoga a sessão corrente (a do
/// Bearer token recebido), idempotente.
/// </summary>
/// <remarks>
/// Paridade com <c>internal/api/admin/handlers/auth.go:Logout</c>:
/// requer <see cref="SessionAuthFilter"/> antes do handler. Devolve 204
/// em sucesso e 401 quando não existe sessão no contexto.
///
/// References:
///   - ADR-0011 — revoke por session.id, UPDATE revoked_at = SYSUTCDATETIME()
///   - apps/gateway/internal/app/adminservice/service.go (Logout Go)
/// </remarks>
public static class Logout
{
    public static RouteGroupBuilder MapLogout(this RouteGroupBuilder group)
    {
        group.MapDelete("/logout", HandleAsync)
            .WithName("AuthLogout")
            .WithTags("Auth")
            .AddEndpointFilter<SessionAuthFilter>();
        return group;
    }

    public static async Task<IResult> HandleAsync(
        HttpContext http,
        ConnectionFactory connections,
        IAuditEventWriter audit,
        IConfiguration config,
        ILogger<LogoutRequestMarker> logger)
    {
        if (http.Items[SessionAuthFilter.SessionItemKey] is not AdminSession session)
        {
            return ApiErrorResults.Unauthorized("no active session");
        }

        var ct = http.RequestAborted;

        try
        {
            using var conn = await connections.CreateOpenAsync(ct);
            var rows = await conn.ExecuteAsync(
                new CommandDefinition(
                    """
                    UPDATE gogateway.admin_sessions
                    SET revoked_at = SYSUTCDATETIME()
                    WHERE id = @Id AND revoked_at IS NULL;
                    """,
                    new { session.Id },
                    cancellationToken: ct));

            await audit.WriteAsync(
                new AuditEvent(
                    RequestId: http.TraceIdentifier,
                    ApplicationName: config.GetValue("Audit:ApplicationName", "admin-api")!,
                    EventType: "admin_logout",
                    Severity: "info",
                    Metadata: new { session_id = session.Id, idempotent_noop = rows == 0 }),
                ct);

            logger.LogInformation("admin_logout session_id={SessionId} idempotent={Idempotent}",
                session.Id, rows == 0);

            return Results.NoContent();
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "admin_logout_failed session_id={SessionId}", session.Id);
            return ApiErrorResults.Internal("logout failed");
        }
    }

    /// <summary>Marker pra ILogger&lt;T&gt;.</summary>
    public sealed class LogoutRequestMarker { }
}
