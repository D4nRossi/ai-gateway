namespace APIGateway.Admin.Api.Domain.Admin;

/// <summary>
/// AdminUser representa um operador autenticável do admin plane.
/// Paridade exata com a tabela <c>gogateway.admin_users</c> (migration 002).
/// </summary>
/// <remarks>
/// Comparação de senha é feita via bcrypt cost=12 (ADR-0011) contra
/// <see cref="PasswordHash"/>. <see cref="Role"/> usa o vocabulário do
/// <c>ck_admin_users_role</c>: <c>admin | operator | viewer</c>.
/// </remarks>
public sealed record AdminUser(
    long Id,
    string Username,
    string PasswordHash,
    string Role,
    bool Active,
    DateTimeOffset CreatedAt,
    DateTimeOffset UpdatedAt);
