using APIGateway.Admin.Api.Features.Applications;
using FluentAssertions;
using Xunit;

namespace APIGateway.Admin.UnitTests.Features.Applications;

/// <summary>
/// Testes do algoritmo de derivacao de key prefix. Paridade BIT-A-BIT
/// com <c>deriveKeyPrefix</c> Go.
/// </summary>
public sealed class KeyPrefixDeriverTests
{
    [Theory]
    [InlineData("Acme",          "gwk_acme")]
    [InlineData("ACME",          "gwk_acme")]                // uppercase ASCII fold
    [InlineData("Acme Test",     "gwk_acmetest")]            // espaco descartado
    [InlineData("App-01_Foo",    "gwk_app01foo")]            // hyphen/underscore descartados
    [InlineData("App #1!",       "gwk_app1")]                // simbolos descartados
    [InlineData("",              "gwk_")]                    // sem nome — so prefixo
    public void Produces_expected_prefix(string name, string expected)
    {
        KeyPrefixDeriver.Derive(name).Should().Be(expected);
    }

    [Fact]
    public void Truncates_to_max_24_chars_after_gwk_prefix()
    {
        var longName = new string('a', 100);
        var result = KeyPrefixDeriver.Derive(longName);
        result.Should().Be("gwk_" + new string('a', KeyPrefixDeriver.MaxNameLen));
        result.Length.Should().Be(4 + KeyPrefixDeriver.MaxNameLen);
    }

    [Fact]
    public void Skips_unicode_multibyte_characters()
    {
        // Paridade Go: itera bytes (nao runes), bytes >= 0x80 nao caem em [a-z0-9].
        KeyPrefixDeriver.Derive("café").Should().Be("gwk_caf",
            "bytes UTF-8 do 'é' (0xc3 0xa9) sao silenciosamente descartados");
    }

    [Fact]
    public void Skips_emoji()
    {
        KeyPrefixDeriver.Derive("app🚀prod").Should().Be("gwk_appprod");
    }
}
