using APIGateway.Admin.Api.Infrastructure.Migrations;
using Testcontainers.MsSql;
using Xunit;

namespace APIGateway.Admin.IntegrationTests.Fixtures;

/// <summary>
/// Fixture compartilhada que sobe um SQL Server 2022 em container Docker,
/// roda DbUp sobre as 12 migrations embedded, e expoe a connection string
/// pros tests.
/// </summary>
/// <remarks>
/// xUnit <see cref="IAsyncLifetime"/>: <see cref="InitializeAsync"/> roda uma
/// vez por test collection antes do primeiro teste, <see cref="DisposeAsync"/>
/// derruba o container quando todos terminam.
///
/// Lifecycle = collection (mais barato que por classe). Mas tests precisam ser
/// isolados em truncate/seed proprio porque o schema eh compartilhado.
/// </remarks>
public sealed class SqlServerFixture : IAsyncLifetime
{
    public MsSqlContainer Container { get; }
    public string ConnectionString => Container.GetConnectionString();

    public SqlServerFixture()
    {
        Container = new MsSqlBuilder()
            .WithImage("mcr.microsoft.com/mssql/server:2022-latest")
            .WithPassword("YourStrong!Passw0rd")
            .Build();
    }

    public async Task InitializeAsync()
    {
        await Container.StartAsync();

        // Roda DbUp pra criar gogateway schema + tabelas. Output capturado em
        // memoria pra nao poluir o test runner; se falhar, joga exception com
        // detalhes do script que quebrou.
        using var writer = new StringWriter();
        var result = MigrationRunner.Run(ConnectionString, writer);
        if (result != 0)
        {
            throw new InvalidOperationException(
                $"DbUp falhou no startup do fixture:{Environment.NewLine}{writer}");
        }
    }

    public async Task DisposeAsync() => await Container.DisposeAsync();
}

/// <summary>
/// Collection xUnit pra agrupar tests que compartilham o mesmo
/// <see cref="SqlServerFixture"/> (uma instancia por test run).
/// </summary>
[CollectionDefinition(nameof(SqlServerCollection))]
public sealed class SqlServerCollection : ICollectionFixture<SqlServerFixture> { }
