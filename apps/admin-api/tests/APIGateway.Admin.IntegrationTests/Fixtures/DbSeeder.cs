using Dapper;
using Microsoft.Data.SqlClient;

namespace APIGateway.Admin.IntegrationTests.Fixtures;

/// <summary>
/// Helpers de DB pra setup/teardown rapido entre tests.
/// </summary>
internal static class DbSeeder
{
    /// <summary>
    /// Apaga TODAS as linhas das tabelas admin (preservando o root user da
    /// migration 010 se desejado). Pra tests, removemos tudo e deixamos o
    /// caller cuidar do que precisa.
    /// </summary>
    public static async Task TruncateAllAsync(string connectionString)
    {
        using var conn = new SqlConnection(connectionString);
        await conn.OpenAsync();
        // Ordem importa por FK CASCADE — sessions primeiro, depois users.
        // Como CASCADE esta declarado, DELETE em admin_users derruba sessions.
        await conn.ExecuteAsync("""
            DELETE FROM gogateway.admin_sessions;
            DELETE FROM gogateway.admin_users;
            DELETE FROM gogateway.audit_events;
            """);
    }

    /// <summary>
    /// Cria um admin_user com bcrypt cost=12 e devolve o id. Senha em plain
    /// pra que tests possam tentar login com ela.
    /// </summary>
    public static async Task<long> CreateAdminUserAsync(
        string connectionString, string username, string plainPassword, string role = "admin")
    {
        using var conn = new SqlConnection(connectionString);
        await conn.OpenAsync();
        var hash = BCrypt.Net.BCrypt.HashPassword(plainPassword, workFactor: 12);
        return await conn.ExecuteScalarAsync<long>("""
            INSERT INTO gogateway.admin_users (username, password_hash, role, active)
            OUTPUT INSERTED.id
            VALUES (@Username, @Hash, @Role, 1);
            """, new { Username = username, Hash = hash, Role = role });
    }
}
