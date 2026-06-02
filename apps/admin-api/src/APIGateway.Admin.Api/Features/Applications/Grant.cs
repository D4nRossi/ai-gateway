using System.Data;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Infrastructure.Auditing;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;
using Microsoft.Data.SqlClient;

namespace APIGateway.Admin.Api.Features.Applications;

/// <summary>
/// POST <c>/admin/v1/applications/{id}/grants/{endpointID}</c> — autoriza a
/// application a chamar o endpoint. Idempotente (insercao duplicada eh
/// silenciosamente ignorada). Requer role <c>operator</c>.
/// </summary>
/// <remarks>
/// Paridade com <c>handlers/applications.go:GrantEndpointAccess</c> e
/// <c>adminservice.go:674 GrantAccess</c>. Tabela:
/// <c>application_endpoint_grants</c> PK composta <c>(application_id, endpoint_id)</c>.
///
/// Implementacao: <c>IF NOT EXISTS ... INSERT</c> em vez de tentar INSERT e
/// engolir 2627 — mais explicito e evita custo de exception flow.
/// </remarks>
public static class Grant
{
    public static RouteGroupBuilder MapApplicationsGrant(this RouteGroupBuilder group)
    {
        group.MapPost("/{id:long}/grants/{endpointID:long}", HandleAsync)
            .WithName("ApplicationsGrant")
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
        ILogger<GrantMarker> logger)
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
            var inserted = await conn.ExecuteAsync(
                new CommandDefinition(
                    """
                    IF NOT EXISTS (
                        SELECT 1 FROM gogateway.application_endpoint_grants
                        WHERE application_id = @AppId AND endpoint_id = @EndpointId
                    )
                    BEGIN
                        INSERT INTO gogateway.application_endpoint_grants
                            (application_id, endpoint_id)
                        VALUES (@AppId, @EndpointId);
                    END;
                    """,
                    new { AppId = id, EndpointId = endpointID },
                    cancellationToken: ct));

            await audit.WriteAsync(
                new AuditEvent(
                    RequestId: http.TraceIdentifier,
                    ApplicationName: config.GetValue("Audit:ApplicationName", "admin-api")!,
                    EventType: "grant_created",
                    Severity: "info",
                    Metadata: new { application_id = id, endpoint_id = endpointID, idempotent_noop = inserted == 0 }),
                ct);

            logger.LogInformation("grant_created application_id={AppId} endpoint_id={EndpointId} idempotent={Idem}",
                id, endpointID, inserted == 0);

            return Results.NoContent();
        }
        catch (SqlException ex) when (ex.Number is 547)
        {
            // 547 = FK constraint violation. application_id ou endpoint_id nao existe.
            return ApiErrorResults.Error(StatusCodes.Status404NotFound,
                "not_found", "application or endpoint not found", ex.Message);
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "grant_failed application_id={AppId} endpoint_id={EndpointId}",
                id, endpointID);
            return ApiErrorResults.Internal("failed to grant access");
        }
    }

    public sealed class GrantMarker { }
}
