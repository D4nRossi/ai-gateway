using APIGateway.Admin.Api.Infrastructure.Http;
using FluentAssertions;
using Xunit;

namespace APIGateway.Admin.UnitTests.Infrastructure.Http;

/// <summary>
/// Testes do <see cref="RequireRoleFilter"/>. Foco na hierarquia
/// viewer(0) &lt; operator(1) &lt; admin(2) e em rejeitar roles desconhecidas.
/// </summary>
/// <remarks>
/// Testes E2E completos do filter rodam nos IntegrationTests com
/// <see cref="Microsoft.AspNetCore.Mvc.Testing.WebApplicationFactory{TEntryPoint}"/>;
/// aqui testamos apenas o construtor e o erro em role nao reconhecida.
/// </remarks>
public sealed class RequireRoleFilterTests
{
    [Theory]
    [InlineData("viewer")]
    [InlineData("operator")]
    [InlineData("admin")]
    public void Constructor_accepts_valid_role(string role)
    {
        var act = () => new RequireRoleFilter(role);
        act.Should().NotThrow();
    }

    [Theory]
    [InlineData("VIEWER")]   // case-sensitive (paridade ck_admin_users_role lowercase)
    [InlineData("guest")]
    [InlineData("")]
    public void Constructor_rejects_unknown_role(string role)
    {
        var act = () => new RequireRoleFilter(role);
        act.Should().Throw<ArgumentException>().WithMessage("*unknown minimum role*");
    }

    [Fact]
    public void Static_factories_return_filters_with_correct_minimum()
    {
        // Smoke test: as 3 fabricas constroem sem throw.
        var act = () =>
        {
            _ = RequireRoleFilter.Admin();
            _ = RequireRoleFilter.Operator();
            _ = RequireRoleFilter.Viewer();
        };
        act.Should().NotThrow();
    }
}
