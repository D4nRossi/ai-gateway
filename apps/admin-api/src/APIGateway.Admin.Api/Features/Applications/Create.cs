using System.Data;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Infrastructure.Auditing;
using APIGateway.Admin.Api.Infrastructure.Crypto;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;
using Microsoft.Data.SqlClient;

namespace APIGateway.Admin.Api.Features.Applications;

/// <summary>
/// POST <c>/admin/v1/applications</c> — cria application com api_key inicial
/// atomicamente. Retorna 201 com raw token exibido uma unica vez (ADR-0009).
/// Requer role <c>operator</c>.
/// </summary>
/// <remarks>
/// Paridade com <c>internal/api/admin/handlers/applications.go:CreateApplication</c>
/// e <c>adminservice.go:265 CreateApplication</c>:
/// <list type="number">
///   <item>Valida body (name nao vazio, tier no enum)</item>
///   <item>Gera 32 bytes random hex como raw secret</item>
///   <item>Deriva <c>gwk_{name24chars}</c> via <see cref="KeyPrefixDeriver"/></item>
///   <item>fullToken = prefix + "_" + rawSecret</item>
///   <item>keyHash = SHA-256 hex (<see cref="OpaqueTokenGenerator.Hash"/>) do fullToken</item>
///   <item>Atomico: INSERT applications OUTPUT id + INSERT api_keys (mesma transaction)</item>
/// </list>
///
/// Username/name duplicado vira 409 (SqlException 2627/2601 — <c>uq_applications_name</c>).
///
/// References:
///   - ADR-0009 — raw token exibido uma vez
///   - ADR-0011 — opaque tokens
///   - SPEC §9.1 — gwk_ format
/// </remarks>
public static class Create
{
    public static RouteGroupBuilder MapApplicationsCreate(this RouteGroupBuilder group)
    {
        group.MapPost("/", HandleAsync)
            .WithName("ApplicationsCreate")
            .WithTags("Applications")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("operator"));
        return group;
    }

    public sealed record CreateRequest(
        string? Name,
        string? Tier,
        IReadOnlyList<string>? AllowedModels,
        bool StreamingAllowed,
        int MaxRpm,
        int MaxTpm,
        decimal MonthlyBudgetBrl);

    public static async Task<IResult> HandleAsync(
        CreateRequest req,
        HttpContext http,
        ConnectionFactory connections,
        IAuditEventWriter audit,
        IConfiguration config,
        ILogger<CreateMarker> logger)
    {
        if (string.IsNullOrWhiteSpace(req.Name))
        {
            return ApiErrorResults.BadRequest("bad_request", "name is required");
        }

        var tier = (req.Tier ?? string.Empty).Trim().ToLowerInvariant();
        if (tier is not ("tier_1" or "tier_2" or "tier_3"))
        {
            return ApiErrorResults.BadRequest("invalid_tier",
                "tier must be tier_1, tier_2, or tier_3");
        }

        var ct = http.RequestAborted;

        var (rawSecret, _) = OpaqueTokenGenerator.Generate();   // 32B random hex
        var prefix = KeyPrefixDeriver.Derive(req.Name);
        var fullToken = $"{prefix}_{rawSecret}";
        var keyHash = OpaqueTokenGenerator.Hash(fullToken);
        var allowedModelsJson = ApplicationMapper.SerializeModels(req.AllowedModels);

        try
        {
            using IDbConnection conn = await connections.CreateOpenAsync(ct);
            using var tx = conn.BeginTransaction();

            // INSERT application + OUTPUT pra obter id + valores defaultados pelo schema.
            var appRow = await conn.QuerySingleAsync<ApplicationRow>(
                new CommandDefinition(
                    """
                    INSERT INTO gogateway.applications
                        (name, tier, allowed_models, streaming_allowed,
                         max_rpm, max_tpm, monthly_budget_brl, active)
                    OUTPUT
                        INSERTED.id                  AS Id,
                        INSERTED.name                AS Name,
                        INSERTED.tier                AS Tier,
                        INSERTED.allowed_models      AS AllowedModelsJson,
                        INSERTED.streaming_allowed   AS StreamingAllowed,
                        INSERTED.max_rpm             AS MaxRpm,
                        INSERTED.max_tpm             AS MaxTpm,
                        INSERTED.monthly_budget_brl  AS MonthlyBudgetBrl,
                        INSERTED.active              AS Active,
                        INSERTED.created_at          AS CreatedAt,
                        INSERTED.updated_at          AS UpdatedAt
                    VALUES
                        (@Name, @Tier, @AllowedModels, @StreamingAllowed,
                         @MaxRpm, @MaxTpm, @MonthlyBudgetBrl, 1);
                    """,
                    new
                    {
                        req.Name,
                        Tier = tier,
                        AllowedModels = allowedModelsJson,
                        req.StreamingAllowed,
                        req.MaxRpm,
                        req.MaxTpm,
                        req.MonthlyBudgetBrl,
                    },
                    transaction: tx,
                    cancellationToken: ct));

            // INSERT api_key associada na mesma transaction. Se falhar, rollback
            // limpa a application — sem orfaos.
            await conn.ExecuteAsync(
                new CommandDefinition(
                    """
                    INSERT INTO gogateway.api_keys (application_id, key_prefix, key_hash)
                    VALUES (@AppId, @Prefix, @Hash);
                    """,
                    new { AppId = appRow.Id, Prefix = prefix, Hash = keyHash },
                    transaction: tx,
                    cancellationToken: ct));

            tx.Commit();

            await audit.WriteAsync(
                new AuditEvent(
                    RequestId: http.TraceIdentifier,
                    ApplicationName: config.GetValue("Audit:ApplicationName", "admin-api")!,
                    EventType: "application_created",
                    Severity: "info",
                    Metadata: new { app_id = appRow.Id, name = appRow.Name, tier, key_prefix = prefix }),
                ct);

            logger.LogInformation("application_created app_id={AppId} name={Name} tier={Tier} key_prefix={Prefix}",
                appRow.Id, appRow.Name, tier, prefix);

            var resp = new CreateApplicationResponse(
                appRow.Id, appRow.Name, appRow.Tier,
                ApplicationMapper.DeserializeModels(appRow.AllowedModelsJson),
                appRow.StreamingAllowed, appRow.MaxRpm, appRow.MaxTpm,
                appRow.MonthlyBudgetBrl, appRow.Active,
                appRow.CreatedAt, appRow.UpdatedAt,
                Token: fullToken,
                KeyPrefix: prefix);

            return Results.Created($"/admin/v1/applications/{appRow.Id}", resp);
        }
        catch (SqlException ex) when (ex.Number is 2627 or 2601)
        {
            // uq_applications_name colidiu.
            return ApiErrorResults.Error(StatusCodes.Status409Conflict,
                "conflict", "application name already exists", ex.Message);
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "application_create_failed name={Name}", req.Name);
            return ApiErrorResults.Internal("falha ao criar aplicação", ex.Message);
        }
    }

    public sealed class CreateMarker { }
}
