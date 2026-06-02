using Azure.Identity;
using Azure.Security.KeyVault.Secrets;

namespace APIGateway.Admin.Api.Infrastructure.KeyVault;

/// <summary>
/// Wrapper sobre <see cref="SecretClient"/> pra escrita de credentials de target
/// (handler <c>MigrateTargetToKV</c>, ADR-0020).
/// </summary>
/// <remarks>
/// Paridade com a interface <c>keyvault.SecretSetter</c> Go.
/// Implementacoes registradas via DI condicional:
/// <see cref="KeyVaultSecretWriter"/> quando <c>KeyVault:Uri</c> esta setado,
/// <see cref="UnavailableKeyVaultWriter"/> caso contrario. Handler injeta
/// <see cref="IKeyVaultSecretWriter"/> e checa <see cref="IsAvailable"/>
/// pra decidir entre prosseguir ou retornar 503.
/// </remarks>
public interface IKeyVaultSecretWriter
{
    bool IsAvailable { get; }
    Task SetAsync(string secretName, string secretValue, CancellationToken ct);
}

public sealed class KeyVaultSecretWriter : IKeyVaultSecretWriter
{
    private readonly SecretClient _client;

    /// <summary>
    /// Constroi cliente apontando pra um Key Vault. Auth via
    /// <see cref="DefaultAzureCredential"/>: tenta Managed Identity primeiro,
    /// depois CLI/env vars (AZURE_TENANT_ID + AZURE_CLIENT_ID + AZURE_CLIENT_SECRET).
    /// </summary>
    public KeyVaultSecretWriter(Uri vaultUri)
    {
        _client = new SecretClient(vaultUri, new DefaultAzureCredential());
    }

    public bool IsAvailable => true;

    public async Task SetAsync(string secretName, string secretValue, CancellationToken ct)
    {
        await _client.SetSecretAsync(secretName, secretValue, ct).ConfigureAwait(false);
    }
}

/// <summary>
/// Stub que indica que KV nao esta configurado. Handlers checam
/// <see cref="IsAvailable"/> e retornam 503 quando false (paridade
/// <c>ErrKVUnavailable</c> Go).
/// </summary>
public sealed class UnavailableKeyVaultWriter : IKeyVaultSecretWriter
{
    public bool IsAvailable => false;

    public Task SetAsync(string secretName, string secretValue, CancellationToken ct)
        => throw new InvalidOperationException(
            "key vault not configured; set KeyVault:Uri (env KeyVault__Uri) and restart the service");
}
