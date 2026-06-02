using System.Text.Json;
using System.Text.Json.Serialization;

namespace APIGateway.Admin.Api.Infrastructure.Crypto;

/// <summary>
/// Formato do plaintext serializado dentro de <c>auth_config_enc</c>.
/// Paridade com <c>endpoint.TargetAuth</c> Go (<c>internal/domain/endpoint/endpoint.go</c>).
/// </summary>
/// <remarks>
/// Critico: Go <c>json.Marshal(TargetAuth)</c> emite <b>PascalCase</b> com
/// TODOS os campos (struct sem JSON tags). Pra runtime Go conseguir
/// <c>json.Unmarshal</c> credentials cifradas pelo .NET, este formato precisa
/// ser preservado byte-a-byte: nomes PascalCase, ordem
/// <c>Type, Token, Header, Value, Username, Password</c>, todos os campos
/// sempre emitidos (sem omitempty).
///
/// <see cref="TargetAuthPlaintextConverter"/> garante a ordem (System.Text.Json
/// nao garante por default em records).
/// </remarks>
[JsonConverter(typeof(TargetAuthPlaintextConverter))]
public sealed record TargetAuthPlaintext(
    string Type,
    string Token,
    string Header,
    string Value,
    string Username,
    string Password)
{
    public static readonly TargetAuthPlaintext None =
        new(Type: "none", Token: "", Header: "", Value: "", Username: "", Password: "");

    /// <summary>
    /// Constroi a partir do request DTO (<see cref="Features.Endpoints.Targets.TargetAuthRequest"/>).
    /// Campos ausentes viram string vazia — paridade Go <c>authFromRequest</c>.
    /// </summary>
    public static TargetAuthPlaintext FromRequest(string? type, string? token, string? header,
                                                  string? value, string? username, string? password)
        => new(
            Type: type ?? "",
            Token: token ?? "",
            Header: header ?? "",
            Value: value ?? "",
            Username: username ?? "",
            Password: password ?? "");
}

/// <summary>
/// Custom converter que serializa <see cref="TargetAuthPlaintext"/> com a ordem
/// EXATA dos campos do struct Go (Type, Token, Header, Value, Username, Password).
/// </summary>
/// <remarks>
/// System.Text.Json garante ordem de declaracao em records normais via reflection
/// (.NET 8+), mas confiar nisso eh fragil — bump de versao pode mudar. Custom
/// converter elimina o risco e deixa a paridade explicita.
///
/// Read aceita qualquer ordem (deserializacao eh tolerante; so a serializacao
/// precisa ser determinista).
/// </remarks>
public sealed class TargetAuthPlaintextConverter : JsonConverter<TargetAuthPlaintext>
{
    public override TargetAuthPlaintext Read(ref Utf8JsonReader reader, Type typeToConvert, JsonSerializerOptions options)
    {
        string type = "", token = "", header = "", value = "", username = "", password = "";

        if (reader.TokenType != JsonTokenType.StartObject)
        {
            throw new JsonException("expected object");
        }

        while (reader.Read())
        {
            if (reader.TokenType == JsonTokenType.EndObject) break;
            if (reader.TokenType != JsonTokenType.PropertyName)
            {
                throw new JsonException("expected property name");
            }
            var prop = reader.GetString()!;
            reader.Read();
            var s = reader.TokenType == JsonTokenType.String ? reader.GetString() ?? "" : "";
            switch (prop)
            {
                case "Type":     type = s; break;
                case "Token":    token = s; break;
                case "Header":   header = s; break;
                case "Value":    value = s; break;
                case "Username": username = s; break;
                case "Password": password = s; break;
                // Unknown fields silenciosamente ignorados (paridade Go json.Unmarshal default).
            }
        }
        return new TargetAuthPlaintext(type, token, header, value, username, password);
    }

    public override void Write(Utf8JsonWriter writer, TargetAuthPlaintext v, JsonSerializerOptions options)
    {
        writer.WriteStartObject();
        writer.WriteString("Type",     v.Type);
        writer.WriteString("Token",    v.Token);
        writer.WriteString("Header",   v.Header);
        writer.WriteString("Value",    v.Value);
        writer.WriteString("Username", v.Username);
        writer.WriteString("Password", v.Password);
        writer.WriteEndObject();
    }
}
