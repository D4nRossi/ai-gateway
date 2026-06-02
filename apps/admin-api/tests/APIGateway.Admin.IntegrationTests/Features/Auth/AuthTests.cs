using System.Net;
using System.Net.Http.Json;
using System.Text.Json;
using APIGateway.Admin.IntegrationTests.Fixtures;
using FluentAssertions;
using Xunit;

namespace APIGateway.Admin.IntegrationTests.Features.Auth;

/// <summary>
/// Tests E2E do slice Auth: <c>POST /admin/v1/auth/login</c> e
/// <c>DELETE /admin/v1/auth/logout</c>.
/// </summary>
[Collection(nameof(SqlServerCollection))]
public sealed class AuthTests : IAsyncLifetime
{
    private readonly SqlServerFixture _sql;
    private AdminApiFactory _factory = null!;
    private HttpClient _client = null!;

    public AuthTests(SqlServerFixture sql) => _sql = sql;

    public async Task InitializeAsync()
    {
        await DbSeeder.TruncateAllAsync(_sql.ConnectionString);
        _factory = new AdminApiFactory(_sql.ConnectionString);
        _client = _factory.CreateClient();
    }

    public Task DisposeAsync()
    {
        _client.Dispose();
        _factory.Dispose();
        return Task.CompletedTask;
    }

    [Fact]
    public async Task Login_returns_400_when_body_missing_fields()
    {
        var resp = await _client.PostAsJsonAsync("/admin/v1/auth/login",
            new { username = "x", password = "" });
        resp.StatusCode.Should().Be(HttpStatusCode.BadRequest);

        var body = await resp.Content.ReadFromJsonAsync<JsonElement>();
        body.GetProperty("error").GetProperty("code").GetString().Should().Be("bad_request");
    }

    [Fact]
    public async Task Login_returns_401_invalid_credentials_for_unknown_user()
    {
        var resp = await _client.PostAsJsonAsync("/admin/v1/auth/login",
            new { username = "ghost", password = "whatever" });
        resp.StatusCode.Should().Be(HttpStatusCode.Unauthorized);

        var body = await resp.Content.ReadFromJsonAsync<JsonElement>();
        body.GetProperty("error").GetProperty("code").GetString().Should().Be("invalid_credentials");
    }

    [Fact]
    public async Task Login_returns_401_for_wrong_password()
    {
        await DbSeeder.CreateAdminUserAsync(_sql.ConnectionString, "alice", "real-password");

        var resp = await _client.PostAsJsonAsync("/admin/v1/auth/login",
            new { username = "alice", password = "wrong" });
        resp.StatusCode.Should().Be(HttpStatusCode.Unauthorized);
    }

    [Fact]
    public async Task Login_succeeds_and_returns_64_hex_token_for_valid_credentials()
    {
        await DbSeeder.CreateAdminUserAsync(_sql.ConnectionString, "alice", "real-password");

        var resp = await _client.PostAsJsonAsync("/admin/v1/auth/login",
            new { username = "alice", password = "real-password" });
        resp.StatusCode.Should().Be(HttpStatusCode.OK);

        var body = await resp.Content.ReadFromJsonAsync<JsonElement>();
        body.GetProperty("token").GetString().Should().MatchRegex("^[0-9a-f]{64}$");
        body.GetProperty("role").GetString().Should().Be("admin");
        body.GetProperty("expires_at").GetString()
            .Should().MatchRegex("^\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}Z$",
                "formato exato RFC3339 paridade Go");
    }

    [Fact]
    public async Task Logout_without_session_returns_401()
    {
        var resp = await _client.DeleteAsync("/admin/v1/auth/logout");
        resp.StatusCode.Should().Be(HttpStatusCode.Unauthorized);
    }

    [Fact]
    public async Task Logout_with_valid_session_returns_204_and_subsequent_logout_returns_401()
    {
        await DbSeeder.CreateAdminUserAsync(_sql.ConnectionString, "alice", "real-password");

        // login pra obter o token
        var loginResp = await _client.PostAsJsonAsync("/admin/v1/auth/login",
            new { username = "alice", password = "real-password" });
        var loginBody = await loginResp.Content.ReadFromJsonAsync<JsonElement>();
        var token = loginBody.GetProperty("token").GetString()!;

        // logout valido
        var logoutReq = new HttpRequestMessage(HttpMethod.Delete, "/admin/v1/auth/logout");
        logoutReq.Headers.Add("Authorization", $"Bearer {token}");
        var logoutResp = await _client.SendAsync(logoutReq);
        logoutResp.StatusCode.Should().Be(HttpStatusCode.NoContent);

        // session revogada — segunda tentativa retorna 401
        var logoutReq2 = new HttpRequestMessage(HttpMethod.Delete, "/admin/v1/auth/logout");
        logoutReq2.Headers.Add("Authorization", $"Bearer {token}");
        var logoutResp2 = await _client.SendAsync(logoutReq2);
        logoutResp2.StatusCode.Should().Be(HttpStatusCode.Unauthorized);
    }
}
