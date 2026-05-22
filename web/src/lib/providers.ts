/**
 * Provider catalog (ADR-0016).
 *
 * Catálogo de providers de modelos suportados pela UI. Cada entrada carrega:
 *
 *   - label:     nome amigável para exibição
 *   - kind:      identificador persistido em `proxy_endpoints.provider_kind`
 *   - baseURL:   sugestão de URL upstream (placeholder visual; user pode editar)
 *   - authType:  tipo padrão de autenticação para o primeiro target
 *   - authHint:  texto explicando como obter a credencial
 *   - color:     cor tailwind para badges/cards (consistência visual)
 *   - tagline:   descrição curta mostrada nos cards
 *   - docs:      URL canônica da documentação oficial
 *
 * Adicionar um novo provider = adicionar uma entrada aqui + entrada no enum
 * `domain/endpoint.ProviderKind` + entrada no CHECK constraint da migration
 * 005 (ou superior).
 *
 * `custom` continua sendo o fallback "qualquer HTTP API".
 */

import type { AuthType, LBStrategy } from "./api";

export type ProviderKind =
  | "azure_openai"
  | "openai"
  | "anthropic"
  | "gemini"
  | "mistral"
  | "cohere"
  | "groq"
  | "together"
  | "ollama"
  | "vllm"
  | "custom";

export interface ProviderMeta {
  kind: ProviderKind;
  label: string;
  tagline: string;
  baseURL: string;
  authType: AuthType;
  /** Apenas para api_key_header: nome do header a injetar. */
  authHeader?: string;
  /** Hint visual no form de target. */
  authHint: string;
  docs: string;
  /** Cor base do badge — usa tokens do tailwind config. */
  accent:
    | "violet"
    | "blue"
    | "amber"
    | "emerald"
    | "rose"
    | "sky"
    | "indigo"
    | "fuchsia"
    | "teal"
    | "zinc";
  /** Default LBStrategy sugerida ao criar endpoint deste provider. */
  defaultStrategy?: LBStrategy;
}

export const PROVIDERS: Record<ProviderKind, ProviderMeta> = {
  azure_openai: {
    kind: "azure_openai",
    label: "Azure OpenAI",
    tagline: "OpenAI hospedado em Azure (chat completions, embeddings).",
    baseURL: "https://{recurso}.cognitiveservices.azure.com",
    authType: "api_key_header",
    authHeader: "api-key",
    authHint: "Use a chave do Azure (KeyVault → cognitive-services-key).",
    docs: "https://learn.microsoft.com/azure/ai-services/openai/reference",
    accent: "blue",
    defaultStrategy: "round_robin",
  },
  openai: {
    kind: "openai",
    label: "OpenAI",
    tagline: "API oficial da OpenAI (GPT-4o, GPT-4, GPT-3.5).",
    baseURL: "https://api.openai.com/v1",
    authType: "bearer_token",
    authHint: "Use sua sk-… key da plataforma OpenAI.",
    docs: "https://platform.openai.com/docs/api-reference",
    accent: "emerald",
    defaultStrategy: "round_robin",
  },
  anthropic: {
    kind: "anthropic",
    label: "Anthropic",
    tagline: "Claude (Opus, Sonnet, Haiku).",
    baseURL: "https://api.anthropic.com/v1",
    authType: "api_key_header",
    authHeader: "x-api-key",
    authHint: "Use sua chave sk-ant-… do console Anthropic.",
    docs: "https://docs.anthropic.com/en/api/getting-started",
    accent: "amber",
    defaultStrategy: "round_robin",
  },
  gemini: {
    kind: "gemini",
    label: "Google Gemini",
    tagline: "Gemini 2.0 Flash, Pro e Ultra.",
    baseURL: "https://generativelanguage.googleapis.com/v1beta",
    authType: "api_key_header",
    authHeader: "x-goog-api-key",
    authHint:
      "API key do Google AI Studio. Header alternativo: ?key= na query.",
    docs: "https://ai.google.dev/api/rest",
    accent: "sky",
    defaultStrategy: "round_robin",
  },
  mistral: {
    kind: "mistral",
    label: "Mistral AI",
    tagline: "Mistral Large/Medium/Small — OpenAI-compatible.",
    baseURL: "https://api.mistral.ai/v1",
    authType: "bearer_token",
    authHint: "Use sua chave do console Mistral.",
    docs: "https://docs.mistral.ai/api/",
    accent: "rose",
    defaultStrategy: "round_robin",
  },
  cohere: {
    kind: "cohere",
    label: "Cohere",
    tagline: "Command R+, embeddings e rerankers.",
    baseURL: "https://api.cohere.com/v1",
    authType: "bearer_token",
    authHint: "Use a chave da Cohere dashboard.",
    docs: "https://docs.cohere.com/reference/about",
    accent: "indigo",
    defaultStrategy: "round_robin",
  },
  groq: {
    kind: "groq",
    label: "Groq",
    tagline: "Inferência de Llama/Mixtral em LPUs — OpenAI-compatible.",
    baseURL: "https://api.groq.com/openai/v1",
    authType: "bearer_token",
    authHint: "Use sua chave gsk_… do console Groq.",
    docs: "https://console.groq.com/docs",
    accent: "fuchsia",
    defaultStrategy: "round_robin",
  },
  together: {
    kind: "together",
    label: "Together AI",
    tagline: "Modelos open-source em SaaS — OpenAI-compatible.",
    baseURL: "https://api.together.xyz/v1",
    authType: "bearer_token",
    authHint: "Use sua chave do console Together.",
    docs: "https://docs.together.ai/reference",
    accent: "teal",
    defaultStrategy: "round_robin",
  },
  ollama: {
    kind: "ollama",
    label: "Ollama",
    tagline: "Modelos open-source localmente (Llama, Mistral, Phi).",
    baseURL: "http://localhost:11434",
    authType: "none",
    authHint:
      "Tipicamente sem autenticação em ambiente local. Configure firewall na frente.",
    docs: "https://github.com/ollama/ollama/blob/main/docs/api.md",
    accent: "violet",
    defaultStrategy: "least_connections",
  },
  vllm: {
    kind: "vllm",
    label: "vLLM",
    tagline: "Servidor de inferência open-source — OpenAI-compatible.",
    baseURL: "http://host:8000/v1",
    authType: "none",
    authHint:
      "Por padrão sem autenticação; ative --api-key no vllm serve para Bearer.",
    docs: "https://docs.vllm.ai/en/latest/serving/openai_compatible_server.html",
    accent: "violet",
    defaultStrategy: "least_connections",
  },
  custom: {
    kind: "custom",
    label: "Personalizado",
    tagline: "Qualquer API HTTP — passthrough genérico, sem defaults.",
    baseURL: "",
    authType: "none",
    authHint: "Configure os headers de auth como o upstream exigir.",
    docs: "",
    accent: "zinc",
    defaultStrategy: "round_robin",
  },
};

/** Lista ordenada para renderização (custom por último). */
export const PROVIDER_LIST: ProviderMeta[] = [
  PROVIDERS.azure_openai,
  PROVIDERS.openai,
  PROVIDERS.anthropic,
  PROVIDERS.gemini,
  PROVIDERS.mistral,
  PROVIDERS.cohere,
  PROVIDERS.groq,
  PROVIDERS.together,
  PROVIDERS.ollama,
  PROVIDERS.vllm,
  PROVIDERS.custom,
];

/**
 * providerMeta — lookup defensivo. Endpoints antigos podem ter provider_kind
 * desconhecido (migração ou bug de seed). Retorna `custom` em vez de undefined.
 */
export function providerMeta(kind: string | undefined | null): ProviderMeta {
  if (kind && kind in PROVIDERS) {
    return PROVIDERS[kind as ProviderKind];
  }
  return PROVIDERS.custom;
}

/**
 * Tailwind classes para badges/cards. Encapsulado aqui para que a paleta
 * fique consistente entre listagem, detail page e selector.
 */
export function providerBadgeClass(kind: ProviderKind): string {
  switch (PROVIDERS[kind].accent) {
    case "blue":
      return "bg-blue-500/15 text-blue-300 border-blue-500/30";
    case "emerald":
      return "bg-emerald-500/15 text-emerald-300 border-emerald-500/30";
    case "amber":
      return "bg-amber-500/15 text-amber-200 border-amber-500/30";
    case "sky":
      return "bg-sky-500/15 text-sky-300 border-sky-500/30";
    case "rose":
      return "bg-rose-500/15 text-rose-300 border-rose-500/30";
    case "indigo":
      return "bg-indigo-500/15 text-indigo-300 border-indigo-500/30";
    case "fuchsia":
      return "bg-fuchsia-500/15 text-fuchsia-300 border-fuchsia-500/30";
    case "teal":
      return "bg-teal-500/15 text-teal-300 border-teal-500/30";
    case "violet":
      return "bg-violet-500/15 text-violet-300 border-violet-500/30";
    case "zinc":
    default:
      return "bg-zinc-700/40 text-zinc-300 border-zinc-600/40";
  }
}
