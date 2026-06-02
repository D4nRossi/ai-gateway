namespace APIGateway.Admin.Api.Domain.Admin;

/// <summary>
/// AdminSession representa uma sessão opaca ativa.
/// Paridade exata com <c>gogateway.admin_sessions</c> (migration 002).
/// </summary>
/// <remarks>
/// <see cref="TokenHash"/> é o hex digest SHA-256 do raw token entregue ao
/// cliente uma única vez (ADR-0011). Sessões expiram quando
/// <see cref="ExpiresAt"/> &lt; agora ou quando <see cref="RevokedAt"/>
/// recebe valor (DELETE /admin/v1/auth/logout).
/// </remarks>
public sealed record AdminSession(
    long Id,
    long AdminUserId,
    string TokenHash,
    DateTimeOffset ExpiresAt,
    DateTimeOffset CreatedAt,
    DateTimeOffset? RevokedAt);
