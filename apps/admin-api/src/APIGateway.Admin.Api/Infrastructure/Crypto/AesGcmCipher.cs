using System.Security.Cryptography;
using System.Text.RegularExpressions;

namespace APIGateway.Admin.Api.Infrastructure.Crypto;

/// <summary>
/// AES-256-GCM symmetric cipher pra credenciais at-rest (ADR-0012).
/// Paridade EXATA com <c>apps/gateway/internal/infra/crypto/crypto.go</c>:
/// <list type="bullet">
///   <item>Key = 32 bytes (64 hex chars)</item>
///   <item>Nonce = 12 bytes random gerados a cada Encrypt</item>
///   <item>Output = <c>nonce || ciphertext || tag(16B)</c> num único <c>byte[]</c></item>
///   <item>Sem associated data (AAD = null)</item>
/// </list>
/// </summary>
/// <remarks>
/// Compatibilidade bidirectional: blobs gerados aqui sao decifrados pelo
/// gateway Go em runtime e vice-versa, contanto que ambos usem a mesma
/// chave (config <c>Database:EncryptionKeyHex</c> = <c>cfg.Database.EncryptionKeyHex</c> Go).
///
/// Safe for concurrent use — <see cref="AesGcm"/> eh thread-safe.
///
/// References:
///   - ADR-0012 — encryption at rest for proxy target credentials
///   - NIST SP 800-38D — GCM specification
///   - https://learn.microsoft.com/en-us/dotnet/api/system.security.cryptography.aesgcm
/// </remarks>
public sealed class AesGcmCipher : IDisposable
{
    public const int NonceSize = 12;
    public const int TagSize = 16;
    public const int KeyBytes = 32;
    public const int KeyHexChars = KeyBytes * 2;

    private static readonly Regex HexAes256 = new(
        "^[0-9a-f]{64}$",
        RegexOptions.Compiled | RegexOptions.CultureInvariant);

    private readonly AesGcm _aes;

    /// <summary>
    /// Constroi cipher a partir de 64-char lowercase hex (32 bytes = AES-256).
    /// Paridade com <c>config.go:hexAES256Re</c>.
    /// </summary>
    /// <exception cref="ArgumentException">Quando <paramref name="keyHex"/> nao
    /// eh 64-char hex lowercase.</exception>
    public AesGcmCipher(string keyHex)
    {
        if (string.IsNullOrWhiteSpace(keyHex) || !HexAes256.IsMatch(keyHex))
        {
            throw new ArgumentException(
                "Database:EncryptionKeyHex must be a 64-character lowercase hex string (32 bytes for AES-256)",
                nameof(keyHex));
        }

        Span<byte> key = stackalloc byte[KeyBytes];
        if (!Convert.TryFromHexString(keyHex, key, out var written) || written != KeyBytes)
        {
            throw new ArgumentException("invalid hex key", nameof(keyHex));
        }

        _aes = new AesGcm(key, TagSize);
    }

    /// <summary>
    /// Cifra <paramref name="plaintext"/> e devolve <c>nonce || ciphertext || tag</c>
    /// num unico array. Paridade com <c>AESGCMEncrypter.Encrypt</c> Go.
    /// </summary>
    public byte[] Encrypt(ReadOnlySpan<byte> plaintext)
    {
        var output = new byte[NonceSize + plaintext.Length + TagSize];
        Span<byte> nonce = output.AsSpan(0, NonceSize);
        Span<byte> ciphertext = output.AsSpan(NonceSize, plaintext.Length);
        Span<byte> tag = output.AsSpan(NonceSize + plaintext.Length, TagSize);

        RandomNumberGenerator.Fill(nonce);
        _aes.Encrypt(nonce, plaintext, ciphertext, tag);
        return output;
    }

    /// <summary>
    /// Decifra blob no formato <c>nonce || ciphertext || tag</c>. Lanca
    /// <see cref="CipherAuthenticationFailedException"/> em falha de tag
    /// (tampered/corrompido). Paridade <c>AESGCMEncrypter.Decrypt</c> Go.
    /// </summary>
    public byte[] Decrypt(ReadOnlySpan<byte> blob)
    {
        if (blob.Length < NonceSize + TagSize)
        {
            throw new ArgumentException(
                $"ciphertext too short: {blob.Length} bytes (minimum {NonceSize + TagSize})",
                nameof(blob));
        }

        var nonce = blob[..NonceSize];
        var tag = blob.Slice(blob.Length - TagSize, TagSize);
        var ciphertext = blob.Slice(NonceSize, blob.Length - NonceSize - TagSize);

        var plaintext = new byte[ciphertext.Length];
        try
        {
            _aes.Decrypt(nonce, ciphertext, tag, plaintext);
        }
        catch (AuthenticationTagMismatchException ex)
        {
            throw new CipherAuthenticationFailedException(
                "AES-GCM authentication tag mismatch — ciphertext was tampered or wrong key", ex);
        }
        return plaintext;
    }

    public void Dispose() => _aes.Dispose();
}

/// <summary>
/// Levantada quando a tag GCM nao bate — paridade com <c>ErrAuthFailed</c> Go.
/// Indica tampering ou chave errada (sem distincao por design GCM).
/// </summary>
public sealed class CipherAuthenticationFailedException : Exception
{
    public CipherAuthenticationFailedException(string message, Exception inner)
        : base(message, inner) { }
}
