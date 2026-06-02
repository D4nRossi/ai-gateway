using System.Text.Json.Serialization;

namespace APIGateway.Admin.Api.Features.Endpoints;

/// <summary>
/// Forma JSON publica de Target. Paridade byte-a-byte com
/// <c>internal/api/admin/handlers/endpoints.go:targetResponse</c>.
/// </summary>
/// <remarks>
/// <see cref="KvSecretName"/> usa <c>omitempty</c> equivalente — quando NULL,
/// some do JSON (frontend espera ausencia, nao string vazia).
/// <c>auth_config_enc</c> nunca aparece — proteccao em camada (mesmo se o
/// row vier completo de Dapper, projection nao tem o campo).
/// </remarks>
public sealed record TargetResponse(
    [property: JsonPropertyName("id")]                       long Id,
    [property: JsonPropertyName("endpoint_id")]              long EndpointId,
    [property: JsonPropertyName("url")]                      string Url,
    [property: JsonPropertyName("weight")]                   int Weight,
    [property: JsonPropertyName("auth_type")]                string AuthType,
    [property: JsonPropertyName("credential_storage_mode")]  string CredentialStorageMode,
    [property: JsonPropertyName("kv_secret_name"),
        JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
        string? KvSecretName,
    [property: JsonPropertyName("active")]                   bool Active,
    [property: JsonPropertyName("created_at")]               DateTimeOffset CreatedAt);
