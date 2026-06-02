using System.Text.Json;
using APIGateway.Admin.Api.Features.Endpoints;
using FluentAssertions;
using Xunit;

namespace APIGateway.Admin.UnitTests.Features.Endpoints;

/// <summary>
/// Testes do <see cref="ProviderConfigValidator"/>. Foco em
/// <c>azure_openai</c> (unico kind com regras hoje, ADR-0017).
/// </summary>
public sealed class ProviderConfigValidatorTests
{
    private static JsonElement Parse(string json)
    {
        using var doc = JsonDocument.Parse(json);
        return doc.RootElement.Clone();
    }

    [Theory]
    [InlineData("custom")]
    [InlineData("openai")]
    [InlineData("anthropic")]
    [InlineData("gemini")]
    [InlineData("mistral")]
    public void Non_azure_kinds_accept_any_config(string kind)
    {
        ProviderConfigValidator.Validate(kind, null).Ok.Should().BeTrue();
        ProviderConfigValidator.Validate(kind, Parse("{}")).Ok.Should().BeTrue();
        ProviderConfigValidator.Validate(kind, Parse("""{"foo":"bar"}""")).Ok.Should().BeTrue();
    }

    [Fact]
    public void Azure_openai_requires_api_version()
    {
        var result = ProviderConfigValidator.Validate("azure_openai", Parse("""{"model_to_deployment":{"a":"b"}}"""));
        result.Ok.Should().BeFalse();
        result.ErrorMessage.Should().Contain("api_version");
    }

    [Fact]
    public void Azure_openai_rejects_null_config()
    {
        var result = ProviderConfigValidator.Validate("azure_openai", null);
        result.Ok.Should().BeFalse();
        result.ErrorMessage.Should().Contain("api_version");
    }

    [Fact]
    public void Azure_openai_requires_model_to_deployment_field()
    {
        var result = ProviderConfigValidator.Validate("azure_openai",
            Parse("""{"api_version":"2025-01-01-preview"}"""));
        result.Ok.Should().BeFalse();
        result.ErrorMessage.Should().Contain("model_to_deployment");
    }

    [Fact]
    public void Azure_openai_rejects_empty_model_map()
    {
        var result = ProviderConfigValidator.Validate("azure_openai",
            Parse("""{"api_version":"2025-01-01-preview","model_to_deployment":{}}"""));
        result.Ok.Should().BeFalse();
        result.ErrorMessage.Should().Contain("at least one model");
    }

    [Fact]
    public void Azure_openai_rejects_model_map_with_empty_value()
    {
        var result = ProviderConfigValidator.Validate("azure_openai",
            Parse("""{"api_version":"2025-01-01-preview","model_to_deployment":{"gpt-4.1":""}}"""));
        result.Ok.Should().BeFalse();
        result.ErrorMessage.Should().Contain("gpt-4.1");
    }

    [Fact]
    public void Azure_openai_rejects_model_to_deployment_as_array()
    {
        var result = ProviderConfigValidator.Validate("azure_openai",
            Parse("""{"api_version":"2025-01-01-preview","model_to_deployment":["gpt-4.1"]}"""));
        result.Ok.Should().BeFalse();
        result.ErrorMessage.Should().Contain("must be an object");
    }

    [Fact]
    public void Azure_openai_accepts_valid_config()
    {
        var json = """{"api_version":"2025-01-01-preview","model_to_deployment":{"gpt-4.1":"gpt-4.1","gpt-4.1-mini":"gpt-4.1-mini"}}""";
        ProviderConfigValidator.Validate("azure_openai", Parse(json)).Ok.Should().BeTrue();
    }
}
