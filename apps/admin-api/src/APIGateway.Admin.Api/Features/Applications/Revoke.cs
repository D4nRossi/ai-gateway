using System.Data;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Infrastructure.Auditing;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;

namespace APIGateway.Admin.Api.Features.Applications;

/// <summary>
/// DELETE <c>/admin/v1/applications/{id}/grants/{endpointID}</c> — revoga
/// permissao da application sobre o endpoint. Idempotente (revoke em
/// inexistente devolve 204 normalmente — paridade Go). Requer role <c>operator</c>.
/// </summary>
/// <remarks>
/// Paridade com <c>handlers/applications.go:RevokeEndpointAccess</c> e
/// <c>adminservice.go:691 RevokeAccess</c>. Emite log com
/// <c>event_type=grant_revoked</c> pra parear com Grant na investigacao
/// de "acessos nao salva" mencionada no <c>docs/handoff.md</c>.
/// </remarks>
public static class Revoke
{
    public static RouteGroupBuilder MapApplicationsRevoke(this RouteGroupBuilder group)
    {
        group.MapDelete("/{id:long}/grants/{endpointID:long}", HandleAsync)
            .WithName("ApplicationsRevoke")
            .WithTags("Applications")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("operator"));
        return group;
    }

    public static async Task<IResult> HandleAsync(
        long id,
        long endpointID,
        HttpContext http,
        ConnectionFactory connections,
        IAuditEventWriter audit,
        IConfiguration config,
        ILogger<RevokeMarker> logger)
    {
        if (id <= 0 || endpointID <= 0)
        {
            return ApiErrorResults.BadRequest("invalid_param",
                "id and endpointID must be positive integers");
        }

        var ct = http.RequestAborted;

        try
        {
            using IDbConnection conn = await connections.CreateOpenAsync(ct);
            var affected = await conn.ExecuteAsync(
                new CommandDefinition(
                    """
                    DELETE FROM gogateway.application_endpoint_grants
                    WHERE application_id = @AppId AND endpoint_id = @EndpointId;
                    """,
                    new { AppId = id, EndpointId = endpointID },
                    cancellationToken: ct));

            await audit.WriteAsync(
                new AuditEvent(
                    RequestId: http.TraceIdentifier,
                    ApplicationName: config.GetValue("Audit:ApplicationName", "admin-api")!,
                    EventType: "grant_revoked",
                    Severity: "info",
                    Metadata: new { application_id = id, endpoint_id = endpointID, idempotent_noop = affected == 0 }),
                ct);

            logger.LogInformation("grant_revoked application_id={AppId} endpoint_id={EndpointId} idempotent={Idem}",
                id, endpointID, affected == 0);

            return Results.NoContent();
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "revoke_failed application_id={AppId} endpoint_id={EndpointId}",
                id, endpointID);
            return ApiErrorResults.Internal("failed to revoke access");
        }
    }

    public sealed class RevokeMarker { }
}
