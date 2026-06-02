using System.Data;
using System.Text;
using System.Text.Json;
using System.Text.RegularExpressions;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Infrastructure.Auditing;
using APIGateway.Admin.Api.Infrastructure.Crypto;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using APIGateway.Admin.Api.Infrastructure.KeyVault;
using Dapper;

namespace APIGateway.Admin.Api.Features.Endpoints.Targets;

/// <summary>
/// POST <c>/admin/v1/endpoints/{id}/targets/{targetID}/migrate-to-kv</c> —
/// move credential do target de AES at-rest pro Azure Key Vault.
/// Requer role <c>operator</c>.
/// </summary>
/// <remarks>
/// Paridade com <c>handlers/endpoints.go:MigrateTargetToKV</c> +
/// <c>adminservice.go:564 MigrateTargetToKV</c>.
///
/// Erro mapping:
/// <list type="bullet">
///   <item>503 <c>kv_unavailable</c> — admin booted sem <c>KeyVault:Uri</c></item>
///   <item>409 <c>already_migrated</c> — target nao esta em mode=aes</item>
///   <item>400 <c>no_credential</c> — auth_type=none, nada a migrar</item>
///   <item>400 <c>invalid_secret_name</c> — nome operador-supplied falha regex</item>
///   <item>404 <c>not_found</c> — endpoint ou target inexistente</item>
/// </list>
///
/// References:
///   - ADR-0020 — credential storage mode per target
///   - ADR-0012 — AES at-rest (mantido em mode=both)
///   - ADR-0018 — Key Vault provider lifecycle
/// </remarks>
public static class MigrateTargetToKV
{
    private static readonly Regex KvSecretNamePattern = new(
        "^[A-Za-z0-9-]{1,127}$",
        RegexOptions.Compiled | RegexOptions.CultureInvariant);

    public static RouteGroupBuilder MapEndpointsMigrateTargetToKV(this RouteGroupBuilder group)
    {
        group.MapPost("/{id:long}/targets/{targetID:long}/migrate-to-kv", HandleAsync)
            .WithName("EndpointsMigrateTargetToKV")
            .WithTags("Endpoints")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("operator"));
        return group;
    }

    public sealed record MigrateRequest(string? Mode, string? SecretName);

    public static async Task<IResult> HandleAsync(
        long id,
        long targetID,
        MigrateRequest req,
        HttpContext http,
        ConnectionFactory connections,
        AesGcmCipher cipher,
        IKeyVaultSecretWriter kvWriter,
        IAuditEventWriter audit,
        IConfiguration config,
        ILogger<MigrateTargetToKVMarker> logger)
    {
        if (id <= 0 || targetID <= 0)
        {
            return ApiErrorResults.BadRequest("invalid_param",
                "id and targetID must be positive integers");
        }

        if (!kvWriter.IsAvailable)
        {
            return ApiErrorResults.Error(StatusCodes.Status503ServiceUnavailable,
                "kv_unavailable",
                "key vault not configured; set KeyVault:Uri (env KeyVault__Uri) and restart the service");
        }

        var mode = (req.Mode ?? string.Empty).Trim().ToLowerInvariant();
        if (mode is not ("kv" or "both"))
        {
            return ApiErrorResults.BadRequest("invalid_mode",
                $"mode \"{req.Mode}\" invalid: expected \"kv\" or \"both\"");
        }

        var ct = http.RequestAborted;

        try
        {
            using IDbConnection conn = await connections.CreateOpenAsync(ct);

            // Verifica endpoint existe (Go faz Get(endpoint) + busca target em ep.Targets).
            var endpointExists = await conn.ExecuteScalarAsync<int?>(
                new CommandDefinition(
                    "SELECT 1 FROM gogateway.proxy_endpoints WHERE id = @Id;",
                    new { Id = id },
                    cancellationToken: ct));
            if (endpointExists is null)
            {
                return ApiErrorResults.Error(StatusCodes.Status404NotFound,
                    "not_found", "endpoint or target not found");
            }

            // Carrega target atual + auth_config_enc binario.
            var target = await conn.QuerySingleOrDefaultAsync<TargetWithEnc>(
                new CommandDefinition(
                    """
                    SELECT
                        id                       AS Id,
                        endpoint_id              AS EndpointId,
                        url                      AS Url,
                        weight                   AS Weight,
                        auth_type                AS AuthType,
                        auth_config_enc          AS AuthConfigEnc,
                        credential_storage_mode  AS CredentialStorageMode,
                        kv_secret_name           AS KvSecretName,
                        active                   AS Active,
                        created_at               AS CreatedAt
                    FROM gogateway.proxy_targets
                    WHERE id = @TargetId AND endpoint_id = @EndpointId;
                    """,
                    new { TargetId = targetID, EndpointId = id },
                    cancellationToken: ct));

            if (target is null)
            {
                return ApiErrorResults.Error(StatusCodes.Status404NotFound,
                    "not_found", "endpoint or target not found");
            }

            var currentMode = string.IsNullOrEmpty(target.CredentialStorageMode) ? "aes" : target.CredentialStorageMode;
            if (currentMode != "aes")
            {
                return ApiErrorResults.Error(StatusCodes.Status409Conflict,
                    "already_migrated",
                    "target credential is not in aes mode",
                    $"target id={targetID} in mode \"{currentMode}\"");
            }

            if (target.AuthType == "none" || target.AuthConfigEnc is null || target.AuthConfigEnc.Length == 0)
            {
                return ApiErrorResults.BadRequest("no_credential",
                    "target has auth_type=none; nothing to migrate");
            }

            // Resolve nome — gera "gateway-target-{uuid-v7}" quando vazio (paridade Go).
            var secretName = (req.SecretName ?? string.Empty).Trim();
            if (secretName.Length == 0)
            {
                secretName = "gateway-target-" + Guid.CreateVersion7().ToString();
            }
            if (!KvSecretNamePattern.IsMatch(secretName))
            {
                return ApiErrorResults.Error(StatusCodes.Status400BadRequest,
                    "invalid_secret_name",
                    "kv secret name must match [A-Za-z0-9-]{1,127}",
                    $"got \"{secretName}\"");
            }

            // Decifra plaintext + reserialize com nome <c>type</c> alinhado ao auth_type
            // do row (defensivo — caso houver desvio entre DB e o plaintext).
            byte[] plaintext;
            try
            {
                plaintext = cipher.Decrypt(target.AuthConfigEnc);
            }
            catch (CipherAuthenticationFailedException ex)
            {
                logger.LogError(ex, "target_migrate_aes_decrypt_failed target_id={TargetId}", targetID);
                return ApiErrorResults.Internal("failed to decrypt existing credential", ex.Message);
            }

            // Envia o JSON exato pro KV (paridade Go que faz `json.Marshal(t.Auth)`
            // e usa string(payload) como value).
            var jsonValue = Encoding.UTF8.GetString(plaintext);
            await kvWriter.SetAsync(secretName, jsonValue, ct);

            // Em mode="kv", limpa AES (auth_type='none', auth_config_enc=NULL).
            // Em mode="both", mantem AES como cache.
            var newAuthType = mode == "kv" ? "none" : target.AuthType;
            object newAuthConfigEnc = mode == "kv"
                ? DBNull.Value
                : target.AuthConfigEnc;

            var updated = await conn.QuerySingleAsync<TargetRow>(
                new CommandDefinition(
                    """
                    UPDATE gogateway.proxy_targets
                    SET auth_type = @AuthType,
                        auth_config_enc = @AuthConfigEnc,
                        credential_storage_mode = @Mode,
                        kv_secret_name = @SecretName
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
                        AuthType = newAuthType,
                        AuthConfigEnc = newAuthConfigEnc,
                        Mode = mode,
                        SecretName = secretName,
                    },
                    cancellationToken: ct));

            await audit.WriteAsync(
                new AuditEvent(
                    RequestId: http.TraceIdentifier,
                    ApplicationName: config.GetValue("Audit:ApplicationName", "admin-api")!,
                    EventType: "target_credential_migrated",
                    Severity: "info",
                    Metadata: new
                    {
                        endpoint_id = id,
                        target_id = targetID,
                        mode_before = "aes",
                        mode_after = mode,
                        kv_secret_name = secretName,
                    }),
                ct);

            logger.LogInformation(
                "target_credential_migrated endpoint_id={EndpointId} target_id={TargetId} mode_after={Mode} kv_secret_name={Secret}",
                id, targetID, mode, secretName);

            return Results.Ok(EndpointMapper.ToResponse(updated));
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "target_credential_migrate_failed target_id={TargetId}", targetID);
            return ApiErrorResults.Internal("failed to migrate target credential", ex.Message);
        }
    }

    /// <summary>Linha do target com auth_config_enc INCLUIDO (caso especial — so
    /// MigrateTargetToKV ve o ciphertext bruto). Demais slices usam <c>TargetRow</c>
    /// que NAO inclui auth_config_enc por seguranca.</summary>
    private sealed record TargetWithEnc(
        long Id,
        long EndpointId,
        string Url,
        int Weight,
        string AuthType,
        byte[]? AuthConfigEnc,
        string CredentialStorageMode,
        string? KvSecretName,
        bool Active,
        DateTimeOffset CreatedAt);

    public sealed class MigrateTargetToKVMarker { }
}
