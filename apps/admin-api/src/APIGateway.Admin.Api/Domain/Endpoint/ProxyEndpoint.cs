using System.Text.Json;

namespace APIGateway.Admin.Api.Domain.Endpoint;

/// <summary>
/// Representa um proxy endpoint (tabela <c>gogateway.proxy_endpoints</c>,
/// migrations 004 + 005 + 006). Paridade com
/// <c>internal/domain/endpoint/endpoint.go</c>.
/// </summary>
/// <remarks>
/// <see cref="LbStrategy"/> ∈ <c>round_robin | weighted_round_robin | random
/// | least_connections | ip_hash</c> (CHECK ck_proxy_endpoints_lb_strategy).
/// <see cref="ProviderKind"/> ∈ enumeracao da migration 005 (azure_openai,
/// openai, anthropic, gemini, mistral, cohere, groq, together, ollama, vllm,
/// custom). <see cref="ProviderConfig"/> eh JSON arbitrario validado por
/// kind no application layer (ADR-0017).
/// </remarks>
public sealed record ProxyEndpoint(
    long Id,
    string Slug,
    string Name,
    string ProviderKind,
    JsonElement ProviderConfig,
    string LbStrategy,
    int MaxRps,
    long MaxMonthlyRequests,
    bool Active,
    DateTimeOffset CreatedAt,
    DateTimeOffset UpdatedAt,
    IReadOnlyList<Target> Targets);
