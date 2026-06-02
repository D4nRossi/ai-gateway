/**
 * Validação e normalização de slug para endpoints proxy.
 *
 * Slug é o identificador curto que vira parte da URL pública do gateway:
 *   /v1/proxy/{slug}/...
 *
 * Por isso ele precisa ser URL-safe, curto e legível. NÃO é a URL real do
 * upstream — essa vive em Target, com credenciais cifradas.
 *
 * Regras:
 *   - 2–64 caracteres
 *   - letras minúsculas (a-z), dígitos (0-9), hífen (-), underline (_)
 *   - não pode conter ":", "/", " ", ou "://" (rejeita URL coladinha)
 *
 * suggestSlug() converte um nome livre ("Endpoint MetLife BR") em um slug
 * válido ("endpoint-metlife-br"), facilitando o cadastro.
 */

const VALID_SLUG = /^[a-z0-9][a-z0-9-_]{0,62}[a-z0-9]$|^[a-z0-9]$/;

export interface SlugIssue {
  kind: "looks_like_url" | "has_space" | "has_invalid_char" | "too_short" | "too_long" | "starts_or_ends_with_separator";
  message: string;
}

/**
 * Inspeciona um slug e retorna a primeira violação encontrada (ou null se OK).
 * Mensagens em pt-BR prontas para exibição no form.
 */
export function validateSlug(s: string): SlugIssue | null {
  const trimmed = s.trim();

  if (trimmed.includes("://") || trimmed.startsWith("http")) {
    return {
      kind: "looks_like_url",
      message:
        "Isso parece uma URL. O identificador é só um nome curto (ex: metlife). A URL real vai depois, na aba Targets.",
    };
  }
  if (/\s/.test(trimmed)) {
    return { kind: "has_space", message: "Não pode conter espaços. Use hífen (-) ou underline (_)." };
  }
  if (trimmed.length < 2) {
    return { kind: "too_short", message: "Pelo menos 2 caracteres." };
  }
  if (trimmed.length > 64) {
    return { kind: "too_long", message: "Máximo 64 caracteres." };
  }
  if (/^[-_]|[-_]$/.test(trimmed)) {
    return {
      kind: "starts_or_ends_with_separator",
      message: "Não pode começar nem terminar com hífen ou underline.",
    };
  }
  if (!VALID_SLUG.test(trimmed)) {
    return {
      kind: "has_invalid_char",
      message: "Apenas letras minúsculas, dígitos, hífen (-) e underline (_).",
    };
  }
  return null;
}

/**
 * Converte um texto livre em um slug candidato.
 *   "Endpoint MetLife BR" → "endpoint-metlife-br"
 *   "https://exemplo.com" → "" (limpa o protocolo) — caller decide o que fazer
 *   "  Olá!  "            → "ola"
 */
export function suggestSlug(input: string): string {
  return input
    .toLowerCase()
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "") // strip diacritics
    .replace(/^https?:\/\//, "")     // strip protocol
    .replace(/[^a-z0-9-_]+/g, "-")   // collapse other chars to hyphen
    .replace(/^-+|-+$/g, "")          // trim leading/trailing hyphens
    .slice(0, 64);
}
