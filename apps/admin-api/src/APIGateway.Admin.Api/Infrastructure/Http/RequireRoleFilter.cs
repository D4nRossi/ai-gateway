using APIGateway.Admin.Api.Domain.Admin;
using APIGateway.Admin.Api.Features.Auth;

namespace APIGateway.Admin.Api.Infrastructure.Http;

/// <summary>
/// Endpoint filter que exige role minima do <see cref="AdminUser"/> autenticado.
/// Composto APOS <see cref="SessionAuthFilter"/> na pipeline — depende dos
/// items injetados por aquele filtro.
/// </summary>
/// <remarks>
/// Paridade comportamental com
/// <c>internal/api/admin/middleware/roles.go:RequireRole</c>. Mesmo ranking
/// numerico: viewer(0) &lt; operator(1) &lt; admin(2). Roles desconhecidas
/// caem em -1 (deny implicito).
///
/// Uso em Minimal API:
/// <code>
/// group.MapPost("/users", Handler)
///      .AddEndpointFilter&lt;SessionAuthFilter&gt;()
///      .AddEndpointFilter(new RequireRoleFilter("admin"));
/// </code>
///
/// References:
///   - docs/v2-alignment.md — hierarquia de roles
///   - ADR-0011 — admin auth opaca
/// </remarks>
public sealed class RequireRoleFilter : IEndpointFilter
{
    private static readonly Dictionary<string, int> RoleRank = new(StringComparer.OrdinalIgnoreCase)
    {
        ["viewer"]   = 0,
        ["operator"] = 1,
        ["admin"]    = 2,
    };

    private readonly int _minimumRank;
    private readonly string _minimumLabel;

    public RequireRoleFilter(string minimum)
    {
        if (!RoleRank.TryGetValue(minimum, out var rank))
        {
            throw new ArgumentException($"unknown minimum role: {minimum}", nameof(minimum));
        }
        _minimumLabel = minimum;
        _minimumRank = rank;
    }

    public ValueTask<object?> InvokeAsync(
        EndpointFilterInvocationContext ctx,
        EndpointFilterDelegate next)
    {
        var http = ctx.HttpContext;

        if (http.Items[SessionAuthFilter.UserItemKey] is not AdminUser user)
        {
            // SessionAuthFilter nao rodou (ou nao autenticou). Paridade Go:
            // 403 forbidden + "authentication required".
            return new ValueTask<object?>(ApiErrorResults.Error(
                StatusCodes.Status403Forbidden, "forbidden", "authentication required"));
        }

        var userRank = RoleRank.TryGetValue(user.Role, out var r) ? r : -1;
        if (userRank < _minimumRank)
        {
            return new ValueTask<object?>(ApiErrorResults.Error(
                StatusCodes.Status403Forbidden, "forbidden",
                "insufficient permissions for this operation"));
        }

        return next(ctx);
    }

    /// <summary>Fabrica de instancias por role pra reuso em DI.</summary>
    public static RequireRoleFilter Admin() => new("admin");
    public static RequireRoleFilter Operator() => new("operator");
    public static RequireRoleFilter Viewer() => new("viewer");
}
