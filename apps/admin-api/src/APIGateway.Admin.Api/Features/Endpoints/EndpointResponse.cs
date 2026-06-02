using System.Text.Json;
using System.Text.Json.Serialization;

namespace APIGateway.Admin.Api.Features.Endpoints;

/// <summary>
/// Forma JSON publica de ProxyEndpoint. Paridade byte-a-byte com
/// <c>internal/api/admin/handlers/endpoints.go:endpointResponse</c>.
/// </summary>
/// <remarks>
/// <see cref="ProviderConfig"/> eh objeto JSON arbitrario (validado por
/// <c>provider_kind</c> no application layer, ADR-0017). Vem do DB como
/// <c>NVARCHAR(MAX)</c> JSON; <see cref="EndpointMapper"/> faz o parse.
/// Nunca emitir <c>null</c> — handler Go normaliza pra <c>{}</c> em ausencia.
/// </remarks>
public sealed record EndpointResponse(
    [property: JsonPropertyName("id")]                    long Id,
    [property: JsonPropertyName("slug")]                  string Slug,
    [property: JsonPropertyName("name")]                  string Name,
    [property: JsonPropertyName("provider_kind")]         string ProviderKind,
    [property: JsonPropertyName("provider_config")]       JsonElement ProviderConfig,
    [property: JsonPropertyName("lb_strategy")]           string LbStrategy,
    [property: JsonPropertyName("max_rps")]               int MaxRps,
    [property: JsonPropertyName("max_monthly_requests")]  long MaxMonthlyRequests,
    [property: JsonPropertyName("active")]                bool Active,
    [property: JsonPropertyName("targets")]               IReadOnlyList<TargetResponse> Targets,
    [property: JsonPropertyName("created_at")]            DateTimeOffset CreatedAt,
    [property: JsonPropertyName("updated_at")]            DateTimeOffset UpdatedAt);
