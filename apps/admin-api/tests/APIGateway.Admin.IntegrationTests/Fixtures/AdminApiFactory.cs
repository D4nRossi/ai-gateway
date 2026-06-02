using Microsoft.AspNetCore.Hosting;
using Microsoft.AspNetCore.Mvc.Testing;
using Microsoft.Extensions.Configuration;

namespace APIGateway.Admin.IntegrationTests.Fixtures;

/// <summary>
/// <see cref="WebApplicationFactory{TEntryPoint}"/> que reconfigura o
/// Kestrel pra apontar pro <see cref="SqlServerFixture.ConnectionString"/>.
/// </summary>
/// <remarks>
/// Pra que <c>Program</c> seja acessivel via generic, o Program.cs declara
/// <c>public partial class Program;</c> no final do arquivo.
///
/// Connection string + encryption key sao injetadas via in-memory config
/// (override do appsettings.json default). KeyVault:Uri fica vazio, registrando
/// <c>UnavailableKeyVaultWriter</c> — tests que precisam de KV ativam
/// explicitamente.
/// </remarks>
public sealed class AdminApiFactory : WebApplicationFactory<Program>
{
    private readonly string _connectionString;
    private readonly string _encryptionKeyHex;

    public AdminApiFactory(string connectionString, string? encryptionKeyHex = null)
    {
        _connectionString = connectionString;
        _encryptionKeyHex = encryptionKeyHex
            ?? "4b3d200ab31e8ede05af67a70632db4e02c01630eac9d1d2e5f51935117bbea1";
    }

    protected override void ConfigureWebHost(IWebHostBuilder builder)
    {
        builder.UseEnvironment("Test");

        builder.ConfigureAppConfiguration((_, cfg) =>
        {
            cfg.AddInMemoryCollection(new Dictionary<string, string?>
            {
                ["ConnectionStrings:Gateway"] = _connectionString,
                ["Database:EncryptionKeyHex"] = _encryptionKeyHex,
                ["Auth:SessionTtlHours"] = "8",
                ["Audit:ApplicationName"] = "admin-api-tests",
                ["KeyVault:Uri"] = "",
            });
        });
    }
}
