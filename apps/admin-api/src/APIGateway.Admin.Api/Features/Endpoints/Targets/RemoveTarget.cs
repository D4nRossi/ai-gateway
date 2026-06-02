using System.Data;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Infrastructure.Auditing;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;

namespace APIGateway.Admin.Api.Features.Endpoints.Targets;

/// <summary>
/// DELETE <c>/admin/v1/endpoints/{id}/targets/{targetID}</c> — soft-delete
/// (active=0). 204 sucesso, 404 nao existe. Requer role <c>operator</c>.
/// </summary>
/// <remarks>
/// Paridade com <c>handlers/endpoints.go:RemoveTarget</c>. <c>auth_config_enc</c>
/// permanece — soft-delete preserva trilha de auditoria caso target seja
/// reativado depois.
/// </remarks>
public static class RemoveTarget
{
    public static RouteGroupBuilder MapEndpointsRemoveTarget(this RouteGroupBuilder group)
    {
        group.MapDelete("/{id:long}/targets/{targetID:long}", HandleAsync)
            .WithName("EndpointsRemoveTarget")
            .WithTags("Endpoints")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("operator"));
        return group;
    }

    public static async Task<IResult> HandleAsync(
        long id,
        long targetID,
        HttpContext http,
        ConnectionFactory connections,
        IAuditEventWriter audit,
        IConfiguration config,
        ILogger<RemoveTargetMarker> logger)
    {
        if (id <= 0 || targetID <= 0)
        {
            return ApiErrorResults.BadRequest("invalid_param",
                "id and targetID must be positive integers");
        }

        var ct = http.RequestAborted;

        try
        {
            using IDbConnection conn = await connections.CreateOpenAsync(ct);
            var affected = await conn.ExecuteAsync(
                new CommandDefinition(
                    """
                    UPDATE gogateway.proxy_targets
                    SET active = 0
                    WHERE id = @TargetId AND active = 1;
                    """,
                    new { TargetId = targetID },
                    cancellationToken: ct));

            if (affected == 0)
            {
                var exists = await conn.ExecuteScalarAsync<int?>(
                    new CommandDefinition(
                        "SELECT 1 FROM gogateway.proxy_targets WHERE id = @TargetId;",
                        new { TargetId = targetID },
                        cancellationToken: ct));
                if (exists is null)
                {
                    return ApiErrorResults.Error(StatusCodes.Status404NotFound,
                        "not_found", "target not found");
                }
                // Idempotente: ja inativo
            }

            await audit.WriteAsync(
                new AuditEvent(
                    RequestId: http.TraceIdentifier,
                    ApplicationName: config.GetValue("Audit:ApplicationName", "admin-api")!,
                    EventType: "target_removed",
                    Severity: "info",
                    Metadata: new { endpoint_id = id, target_id = targetID, was_already_inactive = affected == 0 }),
                ct);

            logger.LogInformation("target_removed endpoint_id={EndpointId} target_id={TargetId}",
                id, targetID);

            return Results.NoContent();
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "target_remove_failed target_id={TargetId}", targetID);
            return ApiErrorResults.Internal("failed to remove target");
        }
    }

    public sealed class RemoveTargetMarker { }
}
