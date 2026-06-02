using System.Text;

namespace APIGateway.Admin.Api.Features.Applications;

/// <summary>
/// Deriva o key prefix <c>gwk_{name24chars}</c> a partir do nome da application.
/// Paridade BIT-A-BIT com <c>adminservice/service.go:deriveKeyPrefix</c>.
/// </summary>
/// <remarks>
/// Algoritmo (preservar exatamente):
/// <list type="bullet">
///   <item>Prefixo fixo "gwk_"</item>
///   <item>Itera bytes ASCII do nome (NAO Unicode case folding)</item>
///   <item>A-Z vira a-z (fold ASCII puro)</item>
///   <item>Mantem apenas <c>[a-z0-9]</c> — demais bytes silenciosamente descartados</item>
///   <item>Para quando atinge <c>4 + 24 = 28</c> chars total</item>
/// </list>
///
/// Exemplos: "Acme Test" -> "gwk_acmetest", "App-1" -> "gwk_app1".
/// Bytes Unicode multibyte sao pulados em vez de causar erro (paridade Go).
///
/// References:
///   - SPEC §9.1 — prefix-based lookup
///   - ADR-0009 — gwk_ format
/// </remarks>
public static class KeyPrefixDeriver
{
    public const int MaxNameLen = 24;
    private const string Prefix = "gwk_";
    private const int MaxTotalLen = 4 + MaxNameLen;

    public static string Derive(string name)
    {
        var sb = new StringBuilder(MaxTotalLen);
        sb.Append(Prefix);

        // Iteracao byte-a-byte de UTF-8 — paridade exata com `for i := 0; i < len(name)`
        // do Go (que itera bytes, nao runes). Bytes multibyte (>= 0x80) nunca caem
        // no range [a-z0-9], entao sao naturalmente descartados.
        var bytes = Encoding.UTF8.GetBytes(name);
        for (int i = 0; i < bytes.Length; i++)
        {
            byte c = bytes[i];
            if (c is >= (byte)'A' and <= (byte)'Z')
            {
                c = (byte)(c + ('a' - 'A'));
            }
            if (c is >= (byte)'a' and <= (byte)'z' or >= (byte)'0' and <= (byte)'9')
            {
                sb.Append((char)c);
                if (sb.Length >= MaxTotalLen)
                {
                    break;
                }
            }
        }
        return sb.ToString();
    }
}
