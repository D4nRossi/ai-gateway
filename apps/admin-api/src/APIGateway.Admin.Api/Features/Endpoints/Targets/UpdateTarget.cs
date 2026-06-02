using System.Data;
using System.Text.Json;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Infrastructure.Auditing;
using APIGateway.Admin.Api.Infrastructure.Crypto;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;

namespace APIGateway.Admin.Api.Features.Endpoints.Targets;

/// <summary>
/// PUT <c>/admin/v1/endpoints/{id}/targets/{targetID}</c> — substitui URL, weight,
/// auth e mode. Credenciais sao re-cifradas (novo nonce, ADR-0012).
/// Requer role <c>operator</c>.
/// </summary>
/// <remarks>
/// Paridade com <c>handlers/endpoints.go:UpdateTarget</c>. <c>endpoint id</c>
/// na URL eh validado mas o lookup eh por <c>target_id</c>. 404 quando o target
/// nao existe.
/// </remarks>
public static class UpdateTarget
{
    public static RouteGroupBuilder MapEndpointsUpdateTarget(this RouteGroupBuilder group)
    {
        group.MapPut("/{id:long}/targets/{targetID:long}", HandleAsync)
            .WithName("EndpointsUpdateTarget")
            .WithTags("Endpoints")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("operator"));
        return group;
    }

    public sealed record UpdateTargetRequest(
        string? Url,
        int Weight,
        TargetAuthRequest? Auth,
        bool Active,
        string? CredentialStorageMode,
        string? KvSecretName);

    public static async Task<IResult> HandleAsync(
        long id,
        long targetID,
        UpdateTargetRequest req,
        HttpContext http,
        ConnectionFactory connections,
        AesGcmCipher cipher,
        IAuditEventWriter audit,
        IConfiguration config,
        ILogger<UpdateTargetMarker> logger)
    {
        if (id <= 0 || targetID <= 0)
        {
            return ApiErrorResults.BadRequest("invalid_param",
                "id and targetID must be positive integers");
        }
        if (string.IsNullOrWhiteSpace(req.Url))
        {
            return ApiErrorResults.BadRequest("bad_request", "url is required");
        }

        var weight = req.Weight > 0 ? req.Weight : 1;
        var mode = string.IsNullOrEmpty(req.CredentialStorageMode) ? "aes" : req.CredentialStorageMode;
        if (mode is not ("aes" or "kv" or "both"))
        {
            return ApiErrorResults.BadRequest("invalid_mode",
                "credential_storage_mode must be aes, kv, or both");
        }

        var auth = req.Auth ?? new TargetAuthRequest("none", null, null, null, null, null);
        var authType = string.IsNullOrEmpty(auth.Type) ? "none" : auth.Type;
        if (authType is not ("none" or "bearer_token" or "api_key_header" or "basic_auth"))
        {
            return ApiErrorResults.BadRequest("invalid_auth_type",
                "auth.type must be none, bearer_token, api_key_header, or basic_auth");
        }

        byte[]? authConfigEnc = null;
        if (authType != "none")
        {
            var plaintextJson = JsonSerializer.SerializeToUtf8Bytes(
                auth.ToPlaintext() with { Type = authType });
            authConfigEnc = cipher.Encrypt(plaintextJson);
        }

        var ct = http.RequestAborted;

        try
        {
            using IDbConnection conn = await connections.CreateOpenAsync(ct);
            var updated = await conn.QuerySingleOrDefaultAsync<TargetRow>(
                new CommandDefinition(
                    """
                    UPDATE gogateway.proxy_targets
                    SET url = @Url,
                        weight = @Weight,
                        auth_type = @AuthType,
                        auth_config_enc = @AuthConfigEnc,
                        active = @Active,
                        credential_storage_mode = @Mode,
                        kv_secret_name = @KvSecretName
                    OUTPUT
                        INSERTED.id                       AS Id,
                        INSERTED.endpoint_id              AS EndpointId,
                        INSERTED.url                      AS Url,
                        INSERTED.weight                   AS Weight,
                        INSERTED.auth_type                AS AuthType,
                        INSERTED.credential_storage_mode  AS CredentialStorageMode,
                        INSERTED.kv_secret_name           AS KvSecretName,
                        INSERTED.active                   AS Active,
                        INSERTED.created_at               AS CreatedAt
                    WHERE id = @TargetId;
                    """,
                    new
                    {
                        TargetId = targetID,
                        Url = req.Url,
                        Weight = weight,
                        AuthType = authType,
                        AuthConfigEnc = (object?)authConfigEnc ?? DBNull.Value,
                        Active = req.Active ? 1 : 0,
                        Mode = mode,
                        KvSecretName = (object?)(string.IsNullOrEmpty(req.KvSecretName) ? null : req.KvSecretName) ?? DBNull.Value,
                    },
                    cancellationToken: ct));

            if (updated is null)
            {
                return ApiErrorResults.Error(StatusCodes.Status404NotFound,
                    "not_found", "target not found");
            }

            await audit.WriteAsync(
                new AuditEvent(
                    RequestId: http.TraceIdentifier,
                    ApplicationName: config.GetValue("Audit:ApplicationName", "admin-api")!,
                    EventType: "target_updated",
                    Severity: "info",
                    Metadata: new { endpoint_id = id, target_id = targetID, updated.Url, updated.AuthType, mode }),
                ct);

            logger.LogInformation("target_updated endpoint_id={EndpointId} target_id={TargetId}",
                id, targetID);

            return Results.Ok(EndpointMapper.ToResponse(updated));
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "target_update_failed target_id={TargetId}", targetID);
            return ApiErrorResults.Internal("failed to update target", ex.Message);
        }
    }

    public sealed class UpdateTargetMarker { }
}
