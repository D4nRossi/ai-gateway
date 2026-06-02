using System.Text.Json;

namespace APIGateway.Admin.Api.Features.Endpoints;

/// <summary>
/// Validador semantico de <c>provider_config</c> por <c>provider_kind</c>.
/// Paridade exata com
/// <c>internal/app/adminservice/service.go:validateProviderConfig</c>.
/// </summary>
/// <remarks>
/// Hoje apenas <c>azure_openai</c> tem requisitos. Demais kinds aceitam
/// qualquer config (ou ausencia). Mantido como classe pra centralizar
/// adicionar cases quando novos providers entrarem.
///
/// References:
///   - ADR-0017 — path translation por provider_kind
/// </remarks>
internal static class ProviderConfigValidator
{
    /// <summary>
    /// Resultado da validacao. <see cref="Ok"/> = <see langword="null"/> em
    /// <see cref="ErrorMessage"/>; em falha, contem mensagem pronta pra
    /// surfacear no JSON de erro.
    /// </summary>
    public sealed record Result(bool Ok, string? ErrorMessage)
    {
        public static Result Success() => new(true, null);
        public static Result Fail(string message) => new(false, message);
    }

    public static Result Validate(string providerKind, JsonElement? cfg)
    {
        switch (providerKind)
        {
            case "azure_openai":
                return ValidateAzureOpenAi(cfg);
            default:
                // Outros kinds aceitam qualquer config (ou nenhum) — paridade Go.
                return Result.Success();
        }
    }

    private static Result ValidateAzureOpenAi(JsonElement? cfg)
    {
        if (cfg is not JsonElement c || c.ValueKind != JsonValueKind.Object)
        {
            return Result.Fail("azure_openai requires \"api_version\" (e.g. \"2025-01-01-preview\")");
        }

        if (!c.TryGetProperty("api_version", out var ver)
            || ver.ValueKind != JsonValueKind.String
            || string.IsNullOrEmpty(ver.GetString()))
        {
            return Result.Fail("azure_openai requires \"api_version\" (e.g. \"2025-01-01-preview\")");
        }

        if (!c.TryGetProperty("model_to_deployment", out var map))
        {
            return Result.Fail("azure_openai requires \"model_to_deployment\" mapping");
        }
        if (map.ValueKind != JsonValueKind.Object)
        {
            return Result.Fail("\"model_to_deployment\" must be an object");
        }

        var hasAny = false;
        foreach (var prop in map.EnumerateObject())
        {
            hasAny = true;
            if (prop.Value.ValueKind != JsonValueKind.String
                || string.IsNullOrEmpty(prop.Value.GetString()))
            {
                return Result.Fail($"model_to_deployment[\"{prop.Name}\"] must be a non-empty string");
            }
        }
        if (!hasAny)
        {
            return Result.Fail("\"model_to_deployment\" must list at least one model");
        }

        return Result.Success();
    }
}
