using System.Security.Cryptography;
using System.Text;

namespace APIGateway.Admin.Api.Infrastructure.Crypto;

/// <summary>
/// Gera tokens opacos de sessão admin e seu hash de persistência.
/// Paridade BIT-A-BIT com <c>internal/app/adminservice/service.go</c>:
/// <list type="bullet">
///   <item>raw = 32 random bytes encoded as lowercase hex (64 chars)</item>
///   <item>hash = SHA-256 do raw como string ASCII, hex lowercase (64 chars)</item>
/// </list>
/// </summary>
/// <remarks>
/// Reasoning: manter o algoritmo idêntico ao do gateway Go é o que permite a
/// migração slice a slice — sessões emitidas pela admin .NET continuam válidas
/// caso futuras leituras passem pelo Go (e vice-versa, enquanto Auth coexiste).
///
/// References:
///   - ADR-0011 — opaque session tokens (32 bytes + SHA-256)
///   - apps/gateway/internal/app/adminservice/service.go:719 (generateRawToken)
///   - apps/gateway/internal/app/adminservice/service.go:729 (hashToken)
/// </remarks>
public static class OpaqueTokenGenerator
{
    /// <summary>
    /// Gera um par <c>(raw, hash)</c>: raw vai pro cliente uma única vez,
    /// hash é persistido em <c>admin_sessions.token_hash</c>.
    /// </summary>
    public static (string Raw, string Hash) Generate()
    {
        Span<byte> bytes = stackalloc byte[32];
        RandomNumberGenerator.Fill(bytes);
        var raw = Convert.ToHexStringLower(bytes);
        return (raw, Hash(raw));
    }

    /// <summary>
    /// Recalcula o hash SHA-256 (lowercase hex) de um raw token recebido em
    /// header <c>Authorization: Bearer ...</c>. Usado pelo SessionAuthFilter
    /// pra fazer lookup constante em <c>admin_sessions</c>.
    /// </summary>
    public static string Hash(string raw)
    {
        var bytes = Encoding.ASCII.GetBytes(raw);
        Span<byte> digest = stackalloc byte[32];
        SHA256.HashData(bytes, digest);
        return Convert.ToHexStringLower(digest);
    }
}
