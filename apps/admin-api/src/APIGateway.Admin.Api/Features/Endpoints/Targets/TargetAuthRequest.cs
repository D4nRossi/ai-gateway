using System.Text.Json.Serialization;
using APIGateway.Admin.Api.Infrastructure.Crypto;

namespace APIGateway.Admin.Api.Features.Endpoints.Targets;

/// <summary>
/// Sub-objeto JSON <c>auth</c> dos requests <c>AddTarget</c>/<c>UpdateTarget</c>.
/// Paridade com <c>handlers/endpoints.go:targetAuthRequest</c>.
/// </summary>
/// <remarks>
/// Os campos omitempty no Go viram nullable strings aqui — frontend pode mandar
/// apenas os campos relevantes pro <see cref="Type"/> escolhido
/// (ex.: bearer_token usa so <see cref="Token"/>).
/// </remarks>
public sealed record TargetAuthRequest(
    [property: JsonPropertyName("type")]     string? Type,
    [property: JsonPropertyName("token")]    string? Token,
    [property: JsonPropertyName("header")]   string? Header,
    [property: JsonPropertyName("value")]    string? Value,
    [property: JsonPropertyName("username")] string? Username,
    [property: JsonPropertyName("password")] string? Password)
{
    /// <summary>Converte pra <see cref="TargetAuthPlaintext"/> pronto pra cifrar.</summary>
    public TargetAuthPlaintext ToPlaintext()
        => TargetAuthPlaintext.FromRequest(Type, Token, Header, Value, Username, Password);
}
