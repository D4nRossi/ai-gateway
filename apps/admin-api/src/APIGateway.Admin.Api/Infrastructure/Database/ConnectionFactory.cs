using System.Data;
using Microsoft.Data.SqlClient;

namespace APIGateway.Admin.Api.Infrastructure.Database;

/// <summary>
/// Cria <see cref="SqlConnection"/> abertas por request (Dapper).
/// Centraliza a connection string e a política de tipos do Dapper.
/// </summary>
/// <remarks>
/// Reasoning: registrando como singleton e expondo <see cref="CreateOpenAsync"/>,
/// cada handler abre/dispose sua própria conexão dentro do scope da request.
/// Dapper opera sobre <see cref="IDbConnection"/>, não precisa de unit-of-work
/// global pra slices CRUD (ADR-0027 — Vertical Slice).
///
/// References:
///   - ADR-0027 — Dapper escolhido sobre EF Core
///   - https://learn.microsoft.com/en-us/sql/connect/ado-net/sql/connection-pooling
/// </remarks>
public sealed class ConnectionFactory
{
    private readonly string _connectionString;

    public ConnectionFactory(string connectionString)
    {
        if (string.IsNullOrWhiteSpace(connectionString))
        {
            throw new ArgumentException("Connection string is empty", nameof(connectionString));
        }
        _connectionString = connectionString;
    }

    /// <summary>
    /// Abre uma conexão SQL Server e retorna pronta para uso.
    /// O caller é dono do dispose (idealmente via <c>await using</c>).
    /// </summary>
    public async Task<IDbConnection> CreateOpenAsync(CancellationToken ct = default)
    {
        var conn = new SqlConnection(_connectionString);
        await conn.OpenAsync(ct).ConfigureAwait(false);
        return conn;
    }
}
