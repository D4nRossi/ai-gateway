using System.Globalization;
using APIGateway.Admin.Api.Infrastructure.Http;

namespace APIGateway.Admin.Api.Features.Observability;

/// <summary>
/// Helpers compartilhados pelas 3 slices read-only (Usage, Audit, Budget).
/// Paridade com <c>handlers/observability.go:parseTimeRange</c> e formato
/// <c>time.RFC3339</c> usado pelo Go.
/// </summary>
internal static class QueryHelpers
{
    private const int DefaultLimit = 100;
    private const int MaxLimit = 1000;

    /// <summary>
    /// Le <c>?from=&amp;to=&amp;limit=</c> da query string. Defaults:
    /// from = now-24h, to = now, limit = 100. Limit deve estar em 1..1000.
    /// </summary>
    public static (IResult? Error, DateTimeOffset From, DateTimeOffset To, int Limit) ParseTimeRange(HttpRequest req)
    {
        var now = DateTimeOffset.UtcNow;
        var from = now.AddHours(-24);
        var to = now;
        var limit = DefaultLimit;

        var fromStr = req.Query["from"].ToString();
        if (!string.IsNullOrEmpty(fromStr))
        {
            if (!TryParseRfc3339(fromStr, out from))
            {
                return (ApiErrorResults.BadRequest("invalid_param",
                    "from must be RFC3339 (e.g. 2006-01-02T15:04:05Z)"), default, default, 0);
            }
        }

        var toStr = req.Query["to"].ToString();
        if (!string.IsNullOrEmpty(toStr))
        {
            if (!TryParseRfc3339(toStr, out to))
            {
                return (ApiErrorResults.BadRequest("invalid_param",
                    "to must be RFC3339 (e.g. 2006-01-02T15:04:05Z)"), default, default, 0);
            }
        }

        var limitStr = req.Query["limit"].ToString();
        if (!string.IsNullOrEmpty(limitStr))
        {
            if (!int.TryParse(limitStr, NumberStyles.Integer, CultureInfo.InvariantCulture, out var n)
                || n < 1 || n > MaxLimit)
            {
                return (ApiErrorResults.BadRequest("invalid_param",
                    "limit must be 1–1000"), default, default, 0);
            }
            limit = n;
        }

        return (null, from, to, limit);
    }

    /// <summary>
    /// Parser RFC3339 estrito alinhado com <c>time.Parse(time.RFC3339, ...)</c> Go.
    /// Aceita "2006-01-02T15:04:05Z" e "2006-01-02T15:04:05+07:00".
    /// </summary>
    public static bool TryParseRfc3339(string s, out DateTimeOffset value)
    {
        return DateTimeOffset.TryParse(s, CultureInfo.InvariantCulture,
            DateTimeStyles.RoundtripKind, out value);
    }

    /// <summary>
    /// Formata em "2006-01-02T15:04:05Z" (UTC, sem ms, sufixo Z literal) —
    /// idem <c>t.UTC().Format(time.RFC3339)</c> Go.
    /// </summary>
    public static string FormatRfc3339Z(DateTimeOffset t)
        => t.UtcDateTime.ToString("yyyy-MM-ddTHH:mm:ssZ", CultureInfo.InvariantCulture);
}
