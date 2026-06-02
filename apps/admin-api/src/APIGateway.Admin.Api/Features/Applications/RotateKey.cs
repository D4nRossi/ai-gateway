using System.Data;
using System.Text.Json.Serialization;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Infrastructure.Auditing;
using APIGateway.Admin.Api.Infrastructure.Crypto;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;

namespace APIGateway.Admin.Api.Features.Applications;

/// <summary>
/// POST <c>/admin/v1/applications/{id}/rotate-key</c> — emite nova api_key
/// atomica e invalida a anterior. Raw token devolvido uma unica vez.
/// Requer role <c>operator</c>.
/// </summary>
/// <remarks>
/// Paridade com <c>handlers/applications.go:RotateAPIKey</c> e
/// <c>adminservice.go:331 RotateAPIKey</c>:
/// <list type="number">
///   <item>SELECT application (precisa do name pra derivar prefix); 404 se nao existe</item>
///   <item>Gera rawSecret 32B hex; prefix = deriveKeyPrefix(name); fullToken = prefix + "_" + rawSecret</item>
///   <item>Em transacao: UPDATE rotated_at em todas as keys ativas dessa app + INSERT a nova</item>
/// </list>
///
/// Filtered unique <c>idx_api_keys_active_per_app</c> (migration 007) garante
/// que so existe uma key com <c>rotated_at IS NULL</c> por app — entao a
/// transacao acima sempre converge em "exatamente uma ativa".
///
/// References:
///   - ADR-0009 — raw token visivel apenas no momento da emissao
///   - migration 007 — filtered unique pra rotacao atomica
/// </remarks>
public static class RotateKey
{
    public static RouteGroupBuilder MapApplicationsRotateKey(this RouteGroupBuilder group)
    {
        group.MapPost("/{id:long}/rotate-key", HandleAsync)
            .WithName("ApplicationsRotateKey")
            .WithTags("Applications")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("operator"));
        return group;
    }

    public sealed record RotateKeyResponse(
        [property: JsonPropertyName("token")]      string Token,
        [property: JsonPropertyName("key_prefix")] string KeyPrefix);

    public static async Task<IResult> HandleAsync(
        long id,
        HttpContext http,
        ConnectionFactory connections,
        IAuditEventWriter audit,
        IConfiguration config,
        ILogger<RotateKeyMarker> logger)
    {
        if (id <= 0)
        {
            return ApiErrorResults.BadRequest("invalid_param", "id must be a positive integer");
        }

        var ct = http.RequestAborted;

        try
        {
            using IDbConnection conn = await connections.CreateOpenAsync(ct);

            var appName = await conn.ExecuteScalarAsync<string?>(
                new CommandDefinition(
                    "SELECT name FROM gogateway.applications WHERE id = @Id;",
                    new { Id = id },
                    cancellationToken: ct));

            if (appName is null)
            {
                return ApiErrorResults.Error(StatusCodes.Status404NotFound,
                    "not_found", "application not found");
            }

            var (rawSecret, _) = OpaqueTokenGenerator.Generate();
            var prefix = KeyPrefixDeriver.Derive(appName);
            var fullToken = $"{prefix}_{rawSecret}";
            var keyHash = OpaqueTokenGenerator.Hash(fullToken);

            using var tx = conn.BeginTransaction();

            await conn.ExecuteAsync(
                new CommandDefinition(
                    """
                    UPDATE gogateway.api_keys
                    SET rotated_at = SYSUTCDATETIME()
                    WHERE application_id = @Id AND rotated_at IS NULL;
                    """,
                    new { Id = id },
                    transaction: tx,
                    cancellationToken: ct));

            await conn.ExecuteAsync(
                new CommandDefinition(
                    """
                    INSERT INTO gogateway.api_keys (application_id, key_prefix, key_hash)
                    VALUES (@Id, @Prefix, @Hash);
                    """,
                    new { Id = id, Prefix = prefix, Hash = keyHash },
                    transaction: tx,
                    cancellationToken: ct));

            tx.Commit();

            await audit.WriteAsync(
                new AuditEvent(
                    RequestId: http.TraceIdentifier,
                    ApplicationName: config.GetValue("Audit:ApplicationName", "admin-api")!,
                    EventType: "application_api_key_rotated",
                    Severity: "info",
                    Metadata: new { app_id = id, name = appName, key_prefix = prefix }),
                ct);

            logger.LogInformation("api_key_rotated app_id={Id} application_name={Name} key_prefix={Prefix}",
                id, appName, prefix);

            return Results.Ok(new RotateKeyResponse(fullToken, prefix));
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "application_rotate_key_failed id={Id}", id);
            return ApiErrorResults.Internal("failed to rotate API key");
        }
    }

    public sealed class RotateKeyMarker { }
}
