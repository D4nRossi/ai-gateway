namespace APIGateway.Admin.Api.Features.Endpoints;

/// <summary>
/// SQL statements e Row records compartilhados pelas slices que tocam
/// <c>gogateway.proxy_endpoints</c> e <c>gogateway.proxy_targets</c>.
/// Reusado por <c>Features/Applications/ListGrants</c> e
/// <c>Features/Endpoints/*</c> (slice ProxyEndpoints).
/// </summary>
internal static class EndpointSql
{
    public const string SelectAllColumns = """
        SELECT
            ep.id                    AS Id,
            ep.slug                  AS Slug,
            ep.name                  AS Name,
            ep.provider_kind         AS ProviderKind,
            ep.provider_config       AS ProviderConfigJson,
            ep.lb_strategy           AS LbStrategy,
            ep.max_rps               AS MaxRps,
            ep.max_monthly_requests  AS MaxMonthlyRequests,
            ep.active                AS Active,
            ep.created_at            AS CreatedAt,
            ep.updated_at            AS UpdatedAt
        FROM gogateway.proxy_endpoints AS ep
        """;

    public const string SelectTargetsColumns = """
        SELECT
            t.id                       AS Id,
            t.endpoint_id              AS EndpointId,
            t.url                      AS Url,
            t.weight                   AS Weight,
            t.auth_type                AS AuthType,
            t.credential_storage_mode  AS CredentialStorageMode,
            t.kv_secret_name           AS KvSecretName,
            t.active                   AS Active,
            t.created_at               AS CreatedAt
        FROM gogateway.proxy_targets AS t
        """;
}

/// <summary>Linha bruta de <c>proxy_endpoints</c>. ProviderConfig fica como
/// string JSON; conversao acontece em <see cref="EndpointMapper"/>.</summary>
internal sealed record EndpointRow(
    long Id,
    string Slug,
    string Name,
    string ProviderKind,
    string ProviderConfigJson,
    string LbStrategy,
    int MaxRps,
    long MaxMonthlyRequests,
    bool Active,
    DateTimeOffset CreatedAt,
    DateTimeOffset UpdatedAt);

/// <summary>Linha bruta de <c>proxy_targets</c>. <c>auth_config_enc</c>
/// nao eh selecionado — segredo nunca volta ao consumidor.</summary>
internal sealed record TargetRow(
    long Id,
    long EndpointId,
    string Url,
    int Weight,
    string AuthType,
    string CredentialStorageMode,
    string? KvSecretName,
    bool Active,
    DateTimeOffset CreatedAt);
