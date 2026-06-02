using Microsoft.AspNetCore.Http;

namespace APIGateway.Admin.Api.Infrastructure.Http;

/// <summary>
/// Envelope JSON canônico de erro do admin plane. Paridade exata com
/// <c>apps/gateway/internal/api/admin/handlers/helpers.go:apiError</c>.
/// </summary>
/// <remarks>
/// O frontend (apps/console) já parseia esta forma — <c>src/lib/api.ts</c>
/// constrói <c>ApiError</c> a partir desta resposta. Mudar formato quebra o
/// console; toda admin route nova precisa retornar este envelope em falhas.
/// </remarks>
public sealed record ApiErrorBody(ApiErrorBody.ErrorDetail Error)
{
    public sealed record ErrorDetail(string Code, string Message, string? Details = null);
}

/// <summary>
/// Helpers pra produzir respostas <see cref="IResult"/> que sigam o envelope.
/// </summary>
public static class ApiErrorResults
{
    public static IResult Error(int status, string code, string message, string? details = null)
        => Results.Json(
            new ApiErrorBody(new ApiErrorBody.ErrorDetail(code, message, details)),
            statusCode: status);

    public static IResult BadRequest(string code, string message)
        => Error(StatusCodes.Status400BadRequest, code, message);

    public static IResult Unauthorized(string message)
        => Error(StatusCodes.Status401Unauthorized, "unauthorized", message);

    public static IResult Internal(string message, string? details = null)
        => Error(StatusCodes.Status500InternalServerError, "internal", message, details);
}
