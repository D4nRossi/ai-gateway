namespace APIGateway.Admin.Api.Domain.Application;

/// <summary>
/// Representa uma application multi-tenant (tabela <c>gogateway.applications</c>).
/// Paridade exata com <c>internal/domain/application/application.go</c> do Go.
/// </summary>
/// <remarks>
/// <see cref="Tier"/> validado pelo CHECK constraint da migration 003:
/// <c>tier_1 | tier_2 | tier_3</c>. <see cref="AllowedModels"/> eh persistido
/// como JSON array em <c>NVARCHAR(MAX)</c> (T-SQL nao tem array tipado).
/// <see cref="MonthlyBudgetBrl"/> usa <c>decimal(12,2)</c> no schema; aqui
/// fica como <see cref="decimal"/> pra preservar precisao monetaria.
/// </remarks>
public sealed record Application(
    long Id,
    string Name,
    string Tier,
    IReadOnlyList<string> AllowedModels,
    bool StreamingAllowed,
    int MaxRpm,
    int MaxTpm,
    decimal MonthlyBudgetBrl,
    bool Active,
    DateTimeOffset CreatedAt,
    DateTimeOffset UpdatedAt);
