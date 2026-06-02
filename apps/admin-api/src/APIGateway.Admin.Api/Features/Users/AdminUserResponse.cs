using System.Text.Json.Serialization;

namespace APIGateway.Admin.Api.Features.Users;

/// <summary>
/// Forma JSON segura do <c>admin_users</c> exposta aos consumidores.
/// <see cref="PasswordHash"/> NUNCA aparece nesta projecao (paridade com
/// Go: handlers.go define struct apartada sem campo de senha).
/// </summary>
/// <remarks>
/// JsonPropertyName explicito porque Dapper devolve colunas snake_case
/// (chave de mapeamento) e o serializer .NET default usa camelCase pra
/// propriedades PascalCase. Sem JsonPropertyName o JSON sairia camelCase
/// ("createdAt") e o frontend (parser TS) quebraria.
/// </remarks>
public sealed record AdminUserResponse(
    [property: JsonPropertyName("id")]         long Id,
    [property: JsonPropertyName("username")]   string Username,
    [property: JsonPropertyName("role")]       string Role,
    [property: JsonPropertyName("active")]     bool Active,
    [property: JsonPropertyName("created_at")] DateTimeOffset CreatedAt,
    [property: JsonPropertyName("updated_at")] DateTimeOffset UpdatedAt);
