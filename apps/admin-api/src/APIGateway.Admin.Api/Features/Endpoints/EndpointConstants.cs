namespace APIGateway.Admin.Api.Features.Endpoints;

/// <summary>
/// Constantes de dominio espelhando os CHECK constraints da migration 004 + 005.
/// Mantidos em codigo pra validar em camada de aplicacao com mensagens uteis
/// (em vez de errar so quando o DB rejeitar).
/// </summary>
internal static class EndpointConstants
{
    /// <summary>Default quando o request omite/envia vazio.</summary>
    public const string DefaultProviderKind = "custom";

    /// <summary>Default quando o request omite/envia vazio.</summary>
    public const string DefaultLbStrategy = "round_robin";

    /// <summary>
    /// Valores aceitos por <c>ck_proxy_endpoints_provider_kind</c> (migration 005).
    /// Atualizar junto com a migration quando novos providers entrarem.
    /// </summary>
    public static readonly HashSet<string> ValidProviderKinds = new(StringComparer.Ordinal)
    {
        "azure_openai", "openai", "anthropic", "gemini",
        "mistral", "cohere", "groq", "together",
        "ollama", "vllm", "custom",
    };

    /// <summary>
    /// Valores aceitos por <c>ck_proxy_endpoints_lb_strategy</c> (migration 004).
    /// </summary>
    public static readonly HashSet<string> ValidLbStrategies = new(StringComparer.Ordinal)
    {
        "round_robin", "weighted_round_robin",
        "random", "least_connections", "ip_hash",
    };
}
