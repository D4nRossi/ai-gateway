using System.Text;
using APIGateway.Admin.Api.Infrastructure.Crypto;
using FluentAssertions;
using Xunit;

namespace APIGateway.Admin.UnitTests.Infrastructure.Crypto;

/// <summary>
/// Testes do <see cref="OpaqueTokenGenerator"/>. Foco em paridade Go
/// (32B random hex + SHA-256 hex deterministico).
/// </summary>
public sealed class OpaqueTokenGeneratorTests
{
    [Fact]
    public void Generate_produces_64_char_hex_raw_and_hash()
    {
        var (raw, hash) = OpaqueTokenGenerator.Generate();
        raw.Length.Should().Be(64, "32 bytes random encoded como hex lowercase");
        hash.Length.Should().Be(64, "SHA-256 hex lowercase");
        raw.Should().MatchRegex("^[0-9a-f]{64}$");
        hash.Should().MatchRegex("^[0-9a-f]{64}$");
    }

    [Fact]
    public void Generate_returns_unique_tokens()
    {
        var (raw1, _) = OpaqueTokenGenerator.Generate();
        var (raw2, _) = OpaqueTokenGenerator.Generate();
        raw1.Should().NotBe(raw2, "32 bytes random — colisao improvavel");
    }

    [Fact]
    public void Hash_is_deterministic()
    {
        const string raw = "gwk_acmetest_0123456789abcdef0123456789abcdef0123456789abcdef0123";
        OpaqueTokenGenerator.Hash(raw).Should().Be(OpaqueTokenGenerator.Hash(raw));
    }

    [Fact]
    public void Hash_matches_known_sha256()
    {
        // Confirma que a impl roda SHA-256 sobre os bytes ASCII (paridade Go
        // crypto/sha256.Sum256([]byte(raw))).
        const string raw = "hello";
        var expected = Convert.ToHexStringLower(
            System.Security.Cryptography.SHA256.HashData(Encoding.ASCII.GetBytes(raw)));
        OpaqueTokenGenerator.Hash(raw).Should().Be(expected);
    }

    [Fact]
    public void Generate_hash_matches_recomputed_hash()
    {
        var (raw, hash) = OpaqueTokenGenerator.Generate();
        OpaqueTokenGenerator.Hash(raw).Should().Be(hash,
            "hash retornado pelo Generate deve ser identico a re-aplicar Hash no raw");
    }
}
