using System.Text.Json;

namespace APIGateway.Admin.Api.Features.Endpoints;

/// <summary>
/// Conversao Row → Response pra endpoints e targets. Paridade com
/// <c>internal/api/admin/handlers/endpoints.go:toEndpointResponse</c> e
/// <c>toTargetResponse</c>.
/// </summary>
internal static class EndpointMapper
{
    /// <summary>
    /// Constrói um <see cref="EndpointResponse"/> agrupando os targets
    /// fornecidos pelo <c>endpoint_id</c>.
    /// </summary>
    public static EndpointResponse ToResponse(EndpointRow row, IEnumerable<TargetRow> targets)
    {
        var provCfg = ParseProviderConfig(row.ProviderConfigJson);
        var targetResponses = targets
            .Where(t => t.EndpointId == row.Id)
            .Select(ToResponse)
            .ToArray();

        return new EndpointResponse(
            row.Id,
            row.Slug,
            row.Name,
            row.ProviderKind,
            provCfg,
            row.LbStrategy,
            row.MaxRps,
            row.MaxMonthlyRequests,
            row.Active,
            targetResponses,
            row.CreatedAt,
            row.UpdatedAt);
    }

    public static TargetResponse ToResponse(TargetRow row)
    {
        // Normaliza credential_storage_mode vazio pra "aes" (paridade Go).
        var mode = string.IsNullOrEmpty(row.CredentialStorageMode) ? "aes" : row.CredentialStorageMode;
        return new TargetResponse(
            row.Id,
            row.EndpointId,
            row.Url,
            row.Weight,
            row.AuthType,
            mode,
            row.KvSecretName,
            row.Active,
            row.CreatedAt);
    }

    /// <summary>
    /// Parseia provider_config JSON. Em ausencia/erro, devolve objeto vazio
    /// <c>{}</c> (paridade Go: handler nunca emite <c>null</c>).
    /// </summary>
    public static JsonElement ParseProviderConfig(string? json)
    {
        if (string.IsNullOrWhiteSpace(json))
        {
            return EmptyObject();
        }
        try
        {
            using var doc = JsonDocument.Parse(json);
            return doc.RootElement.Clone();
        }
        catch (JsonException)
        {
            return EmptyObject();
        }
    }

    private static JsonElement EmptyObject()
    {
        using var doc = JsonDocument.Parse("{}");
        return doc.RootElement.Clone();
    }
}
