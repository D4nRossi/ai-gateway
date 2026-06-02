using System.Text.Json.Serialization;

namespace APIGateway.Admin.Api.Features.Applications;

/// <summary>
/// Forma JSON publica de uma Application. Paridade byte-a-byte com
/// <c>internal/api/admin/handlers/applications.go:applicationResponse</c>.
/// </summary>
public sealed record ApplicationResponse(
    [property: JsonPropertyName("id")]                  long Id,
    [property: JsonPropertyName("name")]                string Name,
    [property: JsonPropertyName("tier")]                string Tier,
    [property: JsonPropertyName("allowed_models")]      IReadOnlyList<string> AllowedModels,
    [property: JsonPropertyName("streaming_allowed")]   bool StreamingAllowed,
    [property: JsonPropertyName("max_rpm")]             int MaxRpm,
    [property: JsonPropertyName("max_tpm")]             int MaxTpm,
    [property: JsonPropertyName("monthly_budget_brl")]  decimal MonthlyBudgetBrl,
    [property: JsonPropertyName("active")]              bool Active,
    [property: JsonPropertyName("created_at")]          DateTimeOffset CreatedAt,
    [property: JsonPropertyName("updated_at")]          DateTimeOffset UpdatedAt);

/// <summary>
/// Extende <see cref="ApplicationResponse"/> com o raw token exibido uma
/// unica vez no create (ADR-0009). Paridade com <c>createApplicationResponse</c>.
/// </summary>
public sealed record CreateApplicationResponse(
    [property: JsonPropertyName("id")]                  long Id,
    [property: JsonPropertyName("name")]                string Name,
    [property: JsonPropertyName("tier")]                string Tier,
    [property: JsonPropertyName("allowed_models")]      IReadOnlyList<string> AllowedModels,
    [property: JsonPropertyName("streaming_allowed")]   bool StreamingAllowed,
    [property: JsonPropertyName("max_rpm")]             int MaxRpm,
    [property: JsonPropertyName("max_tpm")]             int MaxTpm,
    [property: JsonPropertyName("monthly_budget_brl")]  decimal MonthlyBudgetBrl,
    [property: JsonPropertyName("active")]              bool Active,
    [property: JsonPropertyName("created_at")]          DateTimeOffset CreatedAt,
    [property: JsonPropertyName("updated_at")]          DateTimeOffset UpdatedAt,
    [property: JsonPropertyName("token")]               string Token,
    [property: JsonPropertyName("key_prefix")]          string KeyPrefix);
