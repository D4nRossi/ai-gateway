using System.Data;
using APIGateway.Admin.Api.Domain.Admin;
using APIGateway.Admin.Api.Infrastructure.Auditing;
using APIGateway.Admin.Api.Infrastructure.Crypto;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;

namespace APIGateway.Admin.Api.Features.Auth;

/// <summary>
/// POST <c>/admin/v1/auth/login</c> — autentica username/password e devolve
/// um token opaco de sessão exibido uma única vez.
/// </summary>
/// <remarks>
/// Paridade exata com <c>internal/api/admin/handlers/auth.go:Login</c>:
/// <list type="number">
///   <item>SELECT user by username (active = 1)</item>
///   <item>Em miss, roda bcrypt num hash dummy pra evitar timing attack</item>
///   <item>bcrypt.Verify(password, hash) — cost=12</item>
///   <item>Gera raw token (32 bytes random → hex) + token_hash (SHA-256 hex)</item>
///   <item>INSERT em admin_sessions e devolve <see cref="LoginResponse"/></item>
/// </list>
///
/// Erros: <c>400 bad_request</c> pra body inválido,
/// <c>401 invalid_credentials</c> pra user/pwd errados (sem enumeração),
/// <c>500 internal</c> pra falha de DB.
///
/// References:
///   - ADR-0011 — sessão opaca + bcrypt cost=12
///   - apps/gateway/internal/app/adminservice/service.go:139 (Login Go)
/// </remarks>
public static class Login
{
    /// <summary>
    /// Bcrypt dummy hash de valor irrelevante — usado pra equalizar o tempo
    /// de resposta quando o username não existe. Cost=12 = mesmo da senha real.
    /// O conteúdo da senha não importa porque <see cref="BCrypt.Net.BCrypt.Verify"/>
    /// vai sempre retornar false; o que importa é o tempo de CPU consumido.
    /// </summary>
    private const string DummyHash = "$2a$12$.YS7s2k3O0fyZdW9b9d4XOCQXgFx7wXp1IzPv8tVmzRgWiZkPaGYG";

    public static RouteGroupBuilder MapLogin(this RouteGroupBuilder group)
    {
        group.MapPost("/login", HandleAsync)
            .WithName("AuthLogin")
            .WithTags("Auth")
            .AllowAnonymous();
        return group;
    }

    public sealed record LoginRequest(string? Username, string? Password);

    public sealed record LoginResponse(string Token, string ExpiresAt, string Role);

    public static async Task<IResult> HandleAsync(
        LoginRequest req,
        ConnectionFactory connections,
        IAuditEventWriter audit,
        IConfiguration config,
        ILogger<LoginRequestMarker> logger,
        HttpContext http)
    {
        if (string.IsNullOrWhiteSpace(req.Username) || string.IsNullOrWhiteSpace(req.Password))
        {
            return ApiErrorResults.BadRequest("bad_request", "username and password are required");
        }

        var ct = http.RequestAborted;

        AdminUser? user;
        try
        {
            using IDbConnection conn = await connections.CreateOpenAsync(ct);
            user = await conn.QuerySingleOrDefaultAsync<AdminUser>(
                new CommandDefinition(
                    """
                    SELECT
                        id            AS Id,
                        username      AS Username,
                        password_hash AS PasswordHash,
                        role          AS Role,
                        active        AS Active,
                        created_at    AS CreatedAt,
                        updated_at    AS UpdatedAt
                    FROM gogateway.admin_users
                    WHERE username = @Username AND active = 1;
                    """,
                    new { req.Username },
                    cancellationToken: ct));

            if (user is null)
            {
                // timing-attack mitigation: roda bcrypt no dummy mesmo no miss
                _ = BCrypt.Net.BCrypt.Verify(req.Password, DummyHash);
                return ApiErrorResults.Error(StatusCodes.Status401Unauthorized,
                    "invalid_credentials", "invalid username or password");
            }

            if (!BCrypt.Net.BCrypt.Verify(req.Password, user.PasswordHash))
            {
                return ApiErrorResults.Error(StatusCodes.Status401Unauthorized,
                    "invalid_credentials", "invalid username or password");
            }

            var (raw, hash) = OpaqueTokenGenerator.Generate();
            var ttlHours = config.GetValue("Auth:SessionTtlHours", 8);
            var expiresAt = DateTimeOffset.UtcNow.AddHours(ttlHours);

            var sessionId = await conn.ExecuteScalarAsync<long>(
                new CommandDefinition(
                    """
                    INSERT INTO gogateway.admin_sessions (admin_user_id, token_hash, expires_at)
                    OUTPUT INSERTED.id
                    VALUES (@AdminUserId, @TokenHash, @ExpiresAt);
                    """,
                    new { AdminUserId = user.Id, TokenHash = hash, ExpiresAt = expiresAt },
                    cancellationToken: ct));

            await audit.WriteAsync(
                new AuditEvent(
                    RequestId: http.TraceIdentifier,
                    ApplicationName: config.GetValue("Audit:ApplicationName", "admin-api")!,
                    EventType: "admin_login_succeeded",
                    Severity: "info",
                    Metadata: new { user.Username, user.Role, session_id = sessionId }),
                ct);

            logger.LogInformation(
                "admin_login username={Username} role={Role} session_id={SessionId} expires_at={ExpiresAt:o}",
                user.Username, user.Role, sessionId, expiresAt);

            return Results.Ok(new LoginResponse(
                Token: raw,
                ExpiresAt: expiresAt.UtcDateTime.ToString("yyyy-MM-ddTHH:mm:ssZ"),
                Role: user.Role));
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "admin_login_failed username={Username}", req.Username);
            return ApiErrorResults.Internal("login failed");
        }
    }

    /// <summary>Marker type só pra <c>ILogger&lt;T&gt;</c> ficar com categoria sensata.</summary>
    public sealed class LoginRequestMarker { }
}
