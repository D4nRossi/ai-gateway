using System.Data;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Infrastructure.Auditing;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;

namespace APIGateway.Admin.Api.Features.Applications;

/// <summary>
/// DELETE <c>/admin/v1/applications/{id}</c> — soft-delete (active=0).
/// 204 sucesso, 404 nao existe. Requer role <c>operator</c>.
/// </summary>
/// <remarks>
/// Paridade com <c>handlers/applications.go:DeleteApplication</c>. api_keys
/// permanece pra preservar trilha de auditoria — gateway Go ja rejeita auth
/// quando <c>applications.active = 0</c>.
/// </remarks>
public static class Delete
{
    public static RouteGroupBuilder MapApplicationsDelete(this RouteGroupBuilder group)
    {
        group.MapDelete("/{id:long}", HandleAsync)
            .WithName("ApplicationsDelete")
            .WithTags("Applications")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("operator"));
        return group;
    }

    public static async Task<IResult> HandleAsync(
        long id,
        HttpContext http,
        ConnectionFactory connections,
        IAuditEventWriter audit,
        IConfiguration config,
        ILogger<DeleteMarker> logger)
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
                    UPDATE gogateway.applications
                    SET active = 0, updated_at = SYSUTCDATETIME()
                    WHERE id = @Id AND active = 1;
                    """,
                    new { Id = id },
                    cancellationToken: ct));

            if (affected == 0)
            {
                var exists = await conn.ExecuteScalarAsync<int?>(
                    new CommandDefinition(
                        "SELECT 1 FROM gogateway.applications WHERE id = @Id;",
                        new { Id = id },
                        cancellationToken: ct));
                if (exists is null)
                {
                    return ApiErrorResults.Error(StatusCodes.Status404NotFound,
                        "not_found", "application not found");
                }
                // Idempotente: ja inativo — segue pra emitir audit
            }

            await audit.WriteAsync(
                new AuditEvent(
                    RequestId: http.TraceIdentifier,
                    ApplicationName: config.GetValue("Audit:ApplicationName", "admin-api")!,
                    EventType: "application_deleted",
                    Severity: "info",
                    Metadata: new { app_id = id, was_already_inactive = affected == 0 }),
                ct);

            logger.LogInformation("application_deleted app_id={Id} idempotent={Idem}",
                id, affected == 0);

            return Results.NoContent();
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "application_delete_failed id={Id}", id);
            return ApiErrorResults.Internal("failed to delete application");
        }
    }

    public sealed class DeleteMarker { }
}
