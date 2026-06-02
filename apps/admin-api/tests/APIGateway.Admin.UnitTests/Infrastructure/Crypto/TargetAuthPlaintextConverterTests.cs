using System.Text;
using System.Text.Json;
using APIGateway.Admin.Api.Infrastructure.Crypto;
using FluentAssertions;
using Xunit;

namespace APIGateway.Admin.UnitTests.Infrastructure.Crypto;

/// <summary>
/// Testes de paridade do JSON serializado dentro de <c>auth_config_enc</c>.
/// Garantia: byte-a-byte com <c>json.Marshal(TargetAuth)</c> Go (PascalCase,
/// ordem Type/Token/Header/Value/Username/Password, todos os campos sempre).
/// </summary>
public sealed class TargetAuthPlaintextConverterTests
{
    [Fact]
    public void Serializes_with_PascalCase_keys_in_struct_order()
    {
        var auth = new TargetAuthPlaintext(
            Type: "bearer_token",
            Token: "abc",
            Header: "",
            Value: "",
            Username: "",
            Password: "");

        var json = JsonSerializer.Serialize(auth);
        json.Should().Be(
            """{"Type":"bearer_token","Token":"abc","Header":"","Value":"","Username":"","Password":""}""",
            "Go json.Marshal sem tags JSON emite struct field names PascalCase em ordem de declaracao");
    }

    [Fact]
    public void Always_emits_all_fields_even_when_empty()
    {
        // Paridade: Go nao usa omitempty no TargetAuth.
        var auth = TargetAuthPlaintext.None;
        var json = JsonSerializer.Serialize(auth);
        json.Should().Contain("\"Token\":\"\"");
        json.Should().Contain("\"Password\":\"\"");
    }

    [Fact]
    public void Roundtrip_preserves_all_fields()
    {
        var original = new TargetAuthPlaintext(
            Type: "api_key_header",
            Token: "ignored",
            Header: "X-Api-Key",
            Value: "secret-value-123",
            Username: "ignored",
            Password: "ignored");

        var json = JsonSerializer.Serialize(original);
        var deserialized = JsonSerializer.Deserialize<TargetAuthPlaintext>(json);
        deserialized.Should().Be(original);
    }

    [Fact]
    public void Deserializes_fields_in_any_order()
    {
        // JSON eh unordered — o reader deve aceitar qualquer ordem.
        var jsonReordered = """{"Password":"p","Username":"u","Value":"v","Header":"h","Token":"t","Type":"basic_auth"}""";
        var auth = JsonSerializer.Deserialize<TargetAuthPlaintext>(jsonReordered);
        auth!.Type.Should().Be("basic_auth");
        auth.Token.Should().Be("t");
        auth.Password.Should().Be("p");
    }

    [Fact]
    public void Deserializes_silently_ignores_unknown_fields()
    {
        var jsonWithExtra = """{"Type":"none","Token":"","Header":"","Value":"","Username":"","Password":"","extra":"ignored"}""";
        var act = () => JsonSerializer.Deserialize<TargetAuthPlaintext>(jsonWithExtra);
        act.Should().NotThrow("paridade Go json.Unmarshal default ignora campos desconhecidos");
    }

    [Fact]
    public void SerializeToUtf8Bytes_produces_same_bytes_as_serialize_to_string()
    {
        var auth = new TargetAuthPlaintext("bearer_token", "T", "", "", "", "");
        var asBytes = JsonSerializer.SerializeToUtf8Bytes(auth);
        var asString = JsonSerializer.Serialize(auth);
        Encoding.UTF8.GetString(asBytes).Should().Be(asString);
    }
}
