import { Check, Sparkles } from "lucide-react";
import { cn } from "@/lib/utils";
import {
  PROVIDER_LIST,
  PROVIDERS,
  providerBadgeClass,
  type ProviderKind,
} from "@/lib/providers";

interface Props {
  value: ProviderKind;
  onChange: (kind: ProviderKind) => void;
  disabled?: boolean;
}

/**
 * ProviderSelector — grid de cards selecionável.
 *
 * Substitui o dropdown genérico por uma visualização que mostra ao operador,
 * de bate-pronto, quais providers o gateway suporta nativamente e qual ele
 * está escolhendo. O card "Personalizado" deixa explícito que dá pra usar
 * qualquer API HTTP — sem esconder essa opção.
 */
export function ProviderSelector({ value, onChange, disabled }: Props) {
  return (
    // grid responsivo + items-stretch (default) garante alturas uniformes por linha.
    // min-w-0 + overflow-hidden no botão + truncate na URL é o pattern padrão
    // pra evitar que conteúdo longo (URLs do Gemini/Azure) estoure a célula
    // — bug reportado em 2026-05-22 (cards "vazando" o texto pra fora).
    <div className="grid w-full grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
      {PROVIDER_LIST.map((p) => {
        const selected = p.kind === value;
        return (
          <button
            type="button"
            key={p.kind}
            onClick={() => onChange(p.kind)}
            disabled={disabled}
            className={cn(
              "group relative flex h-full min-w-0 flex-col items-start gap-1.5 overflow-hidden rounded-md border p-3 text-left transition-all",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              "disabled:cursor-not-allowed disabled:opacity-50",
              selected
                ? "border-primary/70 bg-primary/10 shadow-inner"
                : "border-border bg-card/40 hover:border-border/80 hover:bg-card/70",
            )}
            aria-pressed={selected}
          >
            <div className="flex w-full min-w-0 items-center justify-between gap-2">
              <span
                className={cn(
                  "inline-flex max-w-full items-center gap-1.5 rounded-md border px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide",
                  providerBadgeClass(p.kind),
                )}
              >
                {p.kind === "custom" && <Sparkles className="h-3 w-3 shrink-0" />}
                <span className="truncate">{p.label}</span>
              </span>
              {selected && <Check className="h-4 w-4 shrink-0 text-primary" />}
            </div>
            <p className="line-clamp-2 w-full text-[11px] leading-snug text-muted-foreground">
              {p.tagline}
            </p>
            {p.baseURL && (
              <p
                className="mt-auto block w-full truncate font-mono text-[10px] text-muted-foreground/70"
                title={p.baseURL}
              >
                {p.baseURL}
              </p>
            )}
          </button>
        );
      })}
    </div>
  );
}

/**
 * ProviderBadge — versão compacta para listagens e detail headers.
 */
export function ProviderBadge({ kind }: { kind: ProviderKind | string }) {
  const meta =
    PROVIDERS[(kind as ProviderKind) in PROVIDERS ? (kind as ProviderKind) : "custom"];
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-md border px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide",
        providerBadgeClass(meta.kind),
      )}
    >
      {meta.label}
    </span>
  );
}
