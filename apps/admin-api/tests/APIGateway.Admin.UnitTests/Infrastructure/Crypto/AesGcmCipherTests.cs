using System.Text;
using APIGateway.Admin.Api.Infrastructure.Crypto;
using FluentAssertions;
using Xunit;

namespace APIGateway.Admin.UnitTests.Infrastructure.Crypto;

/// <summary>
/// Testes do <see cref="AesGcmCipher"/>. Foco em paridade ADR-0012 com Go:
/// nonce 12B, tag 16B, layout <c>nonce||ciphertext||tag</c>, sem AAD.
/// </summary>
public sealed class AesGcmCipherTests
{
    private const string ValidKeyHex = "4b3d200ab31e8ede05af67a70632db4e02c01630eac9d1d2e5f51935117bbea1";

    [Fact]
    public void Constructor_rejects_key_too_short()
    {
        var act = () => new AesGcmCipher("4b3d");
        act.Should().Throw<ArgumentException>()
            .WithMessage("*64-character lowercase hex*");
    }

    [Fact]
    public void Constructor_rejects_uppercase_hex()
    {
        var upper = ValidKeyHex.ToUpperInvariant();
        var act = () => new AesGcmCipher(upper);
        act.Should().Throw<ArgumentException>("regex eh case-sensitive (paridade Go hexAES256Re)");
    }

    [Fact]
    public void Constructor_rejects_non_hex()
    {
        var act = () => new AesGcmCipher(new string('z', 64));
        act.Should().Throw<ArgumentException>();
    }

    [Fact]
    public void Roundtrip_preserves_plaintext()
    {
        using var cipher = new AesGcmCipher(ValidKeyHex);
        var plain = Encoding.UTF8.GetBytes("oi mundo, isso eh um secret");
        var enc = cipher.Encrypt(plain);
        var dec = cipher.Decrypt(enc);
        dec.Should().BeEquivalentTo(plain);
    }

    [Fact]
    public void Encrypt_produces_layout_nonce12_plus_ciphertext_plus_tag16()
    {
        using var cipher = new AesGcmCipher(ValidKeyHex);
        var plain = new byte[32];
        var enc = cipher.Encrypt(plain);
        enc.Length.Should().Be(AesGcmCipher.NonceSize + plain.Length + AesGcmCipher.TagSize,
            "paridade ADR-0012: nonce(12) || ciphertext(N) || tag(16)");
    }

    [Fact]
    public void Encrypt_uses_random_nonces_per_call()
    {
        using var cipher = new AesGcmCipher(ValidKeyHex);
        var plain = Encoding.UTF8.GetBytes("same plaintext");
        var enc1 = cipher.Encrypt(plain);
        var enc2 = cipher.Encrypt(plain);
        enc1.Should().NotEqual(enc2, "nonce random por chamada — ciphertexts devem diferir");
    }

    [Fact]
    public void Decrypt_throws_on_tampered_ciphertext()
    {
        using var cipher = new AesGcmCipher(ValidKeyHex);
        var plain = Encoding.UTF8.GetBytes("attack at dawn");
        var enc = cipher.Encrypt(plain);
        // flip um bit no meio do ciphertext
        enc[20] ^= 0x01;
        var act = () => cipher.Decrypt(enc);
        act.Should().Throw<CipherAuthenticationFailedException>();
    }

    [Fact]
    public void Decrypt_throws_on_truncated_blob()
    {
        using var cipher = new AesGcmCipher(ValidKeyHex);
        var tooShort = new byte[10];
        var act = () => cipher.Decrypt(tooShort);
        act.Should().Throw<ArgumentException>().WithMessage("*ciphertext too short*");
    }

    [Fact]
    public void Decrypt_throws_on_wrong_key()
    {
        var otherKeyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";
        using var cipherA = new AesGcmCipher(ValidKeyHex);
        using var cipherB = new AesGcmCipher(otherKeyHex);
        var plain = Encoding.UTF8.GetBytes("secret");
        var enc = cipherA.Encrypt(plain);
        var act = () => cipherB.Decrypt(enc);
        act.Should().Throw<CipherAuthenticationFailedException>();
    }
}
