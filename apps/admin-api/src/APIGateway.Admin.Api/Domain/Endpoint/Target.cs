namespace APIGateway.Admin.Api.Domain.Endpoint;

/// <summary>
/// Target (upstream backend) de um <see cref="ProxyEndpoint"/>.
/// Tabela <c>gogateway.proxy_targets</c> (migrations 004 + 011).
/// </summary>
/// <remarks>
/// <see cref="AuthType"/> ∈ <c>none | bearer_token | api_key_header | basic_auth</c>.
/// <c>auth_config_enc</c> (AES-256-GCM ciphertext) **nao aparece** neste record
/// nem nas projecoes JSON: nunca devolve credencial ao consumidor (ADR-0012).
/// <see cref="CredentialStorageMode"/> ∈ <c>aes | kv | both</c> (ADR-0020).
/// </remarks>
public sealed record Target(
    long Id,
    long EndpointId,
    string Url,
    int Weight,
    string AuthType,
    string CredentialStorageMode,
    string? KvSecretName,
    bool Active,
    DateTimeOffset CreatedAt);
