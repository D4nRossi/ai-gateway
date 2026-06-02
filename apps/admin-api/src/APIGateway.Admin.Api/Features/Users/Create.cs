using System.Data;
using APIGateway.Admin.Api.Features.Auth;
using APIGateway.Admin.Api.Infrastructure.Auditing;
using APIGateway.Admin.Api.Infrastructure.Database;
using APIGateway.Admin.Api.Infrastructure.Http;
using Dapper;
using Microsoft.Data.SqlClient;

namespace APIGateway.Admin.Api.Features.Users;

/// <summary>
/// POST <c>/admin/v1/users</c> — cria um novo admin user. Requer role <c>admin</c>.
/// </summary>
/// <remarks>
/// Paridade com <c>internal/api/admin/handlers/users.go:CreateUser</c>:
/// <list type="number">
///   <item>Valida body (username/password/role nao vazios)</item>
///   <item>Valida role em <c>admin | operator | viewer</c></item>
///   <item>bcrypt cost=12 do password</item>
///   <item>INSERT em <c>gogateway.admin_users</c> com OUTPUT INSERTED</item>
///   <item>Devolve 201 com <see cref="AdminUserResponse"/></item>
/// </list>
///
/// Username duplicado vira 409 (SQL Server error 2627/2601), nao 500.
/// Paridade com translatePgError do Go que mapeia conflito de UNIQUE.
///
/// References:
///   - ADR-0011 — bcrypt cost=12
///   - apps/gateway/internal/app/adminservice/service.go:209 (CreateAdminUser)
/// </remarks>
public static class Create
{
    public static RouteGroupBuilder MapCreate(this RouteGroupBuilder group)
    {
        group.MapPost("/", HandleAsync)
            .WithName("UsersCreate")
            .WithTags("Users")
            .AddEndpointFilter<SessionAuthFilter>()
            .AddEndpointFilter(new RequireRoleFilter("admin"));
        return group;
    }

    public sealed record CreateRequest(string? Username, string? Password, string? Role);

    public static async Task<IResult> HandleAsync(
        CreateRequest req,
        HttpContext http,
        ConnectionFactory connections,
        IAuditEventWriter audit,
        IConfiguration config,
        ILogger<CreateMarker> logger)
    {
        if (string.IsNullOrWhiteSpace(req.Username)
            || string.IsNullOrWhiteSpace(req.Password)
            || string.IsNullOrWhiteSpace(req.Role))
        {
            return ApiErrorResults.BadRequest("bad_request",
                "username, password, and role are required");
        }

        var role = req.Role.Trim().ToLowerInvariant();
        if (role is not ("admin" or "operator" or "viewer"))
        {
            return ApiErrorResults.BadRequest("invalid_role",
                "role must be admin, operator, or viewer");
        }

        var ct = http.RequestAborted;
        var hash = BCrypt.Net.BCrypt.HashPassword(req.Password, workFactor: 12);

        try
        {
            using IDbConnection conn = await connections.CreateOpenAsync(ct);
            var created = await conn.QuerySingleAsync<AdminUserResponse>(
                new CommandDefinition(
                    """
                    INSERT INTO gogateway.admin_users
                        (username, password_hash, role, active)
                    OUTPUT
                        INSERTED.id            AS id,
                        INSERTED.username      AS username,
                        INSERTED.role          AS role,
                        INSERTED.active        AS active,
                        INSERTED.created_at    AS created_at,
                        INSERTED.updated_at    AS updated_at
                    VALUES
                        (@Username, @PasswordHash, @Role, 1);
                    """,
                    new { req.Username, PasswordHash = hash, Role = role },
                    cancellationToken: ct));

            await audit.WriteAsync(
                new AuditEvent(
                    RequestId: http.TraceIdentifier,
                    ApplicationName: config.GetValue("Audit:ApplicationName", "admin-api")!,
                    EventType: "admin_user_created",
                    Severity: "info",
                    Metadata: new { user_id = created.Id, created.Username, created.Role }),
                ct);

            logger.LogInformation("admin_user_created username={Username} role={Role}",
                created.Username, created.Role);

            return Results.Created($"/admin/v1/users/{created.Id}", created);
        }
        catch (SqlException ex) when (ex.Number is 2627 or 2601)
        {
            // 2627 = PRIMARY KEY violation; 2601 = unique index violation.
            // uq_admin_users_username dispara aqui.
            return ApiErrorResults.Error(StatusCodes.Status409Conflict,
                "conflict", "username already exists", ex.Message);
        }
        catch (Exception ex)
        {
            logger.LogError(ex, "admin_user_create_failed username={Username}", req.Username);
            return ApiErrorResults.Internal("falha ao criar usuário", ex.Message);
        }
    }

    public sealed class CreateMarker { }
}
