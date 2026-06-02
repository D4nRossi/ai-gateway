using System.Data;
using System.Text.Json;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Infrastructure.Auditing;
using APIGateway.Admin.Api.Infrastructure.Crypto;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;
using Microsoft.Data.SqlClient;

namespace APIGateway.Admin.Api.Features.Endpoints.Targets;

/// <summary>
/// POST <c>/admin/v1/endpoints/{id}/targets</c> — adiciona target upstream.
/// Credenciais sao cifradas via AES-256-GCM (ADR-0012). Requer role <c>operator</c>.
/// </summary>
/// <remarks>
/// Paridade com <c>handlers/endpoints.go:AddTarget</c> e
/// <c>adminservice.go:AddTarget</c>. <c>weight</c> default = 1 quando &lt;= 0.
/// <c>credential_storage_mode</c> default = "aes" quando vazio. <c>auth.type =
/// "none"</c> persiste <c>auth_config_enc = NULL</c>.
/// </remarks>
public static class AddTarget
{
    public static RouteGroupBuilder MapEndpointsAddTarget(this RouteGroupBuilder group)
    {
        group.MapPost("/{id:long}/targets", HandleAsync)
            .WithName("EndpointsAddTarget")
            .WithTags("Endpoints")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("operator"));
        return group;
    }

    public sealed record AddTargetRequest(
        string? Url,
        int Weight,
        TargetAuthRequest? Auth,
        string? CredentialStorageMode,
        string? KvSecretName);

    public static async Task<IResult> HandleAsync(
        long id,
        AddTargetRequest req,
        HttpContext http,
        ConnectionFactory connections,
        AesGcmCipher cipher,
        IAuditEventWriter audit,
        IConfiguration config,
        ILogger<AddTargetMarker> logger)
    {
        if (id <= 0)
        {
            return ApiErrorResults.BadRequest("invalid_param", "id must be a positive integer");
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

        // Cifra apenas se auth_type != none. Paridade Go: AuthNone persiste NULL.
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
            var row = await conn.QuerySingleAsync<TargetRow>(
                new CommandDefinition(
                    """
                    INSERT INTO gogateway.proxy_targets
                        (endpoint_id, url, weight, auth_type, auth_config_enc,
                         credential_storage_mode, kv_secret_name, active)
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
                    VALUES
                        (@EndpointId, @Url, @Weight, @AuthType, @AuthConfigEnc,
                         @Mode, @KvSecretName, 1);
                    """,
                    new
                    {
                        EndpointId = id,
                        Url = req.Url,
                        Weight = weight,
                        AuthType = authType,
                        AuthConfigEnc = (object?)authConfigEnc ?? DBNull.Value,
                        Mode = mode,
                        KvSecretName = (object?)(string.IsNullOrEmpty(req.KvSecretName) ? null : req.KvSecretName) ?? DBNull.Value,
                    },
                    cancellationToken: ct));

            await audit.WriteAsync(
                new AuditEvent(
                    RequestId: http.TraceIdentifier,
                    ApplicationName: config.GetValue("Audit:ApplicationName", "admin-api")!,
                    EventType: "target_added",
                    Severity: "info",
                    Metadata: new { endpoint_id = id, target_id = row.Id, row.Url, row.AuthType, mode }),
                ct);

            logger.LogInformation("target_added endpoint_id={EndpointId} target_id={TargetId} auth_type={AuthType}",
                id, row.Id, authType);

            return Results.Created($"/admin/v1/endpoints/{id}/targets/{row.Id}",
                EndpointMapper.ToResponse(row));
        }
        catch (SqlException ex) when (ex.Number is 547)
        {
            return ApiErrorResults.Error(StatusCodes.Status404NotFound,
                "not_found", "endpoint not found", ex.Message);
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "target_add_failed endpoint_id={EndpointId}", id);
            return ApiErrorResults.Internal("failed to add target", ex.Message);
        }
    }

    public sealed class AddTargetMarker { }
}
