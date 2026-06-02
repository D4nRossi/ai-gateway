import { useState } from "react";
import { Check, Copy, ExternalLink, Info } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { providerMeta, type ProviderKind } from "@/lib/providers";
import { toast } from "@/components/ui/sonner";

interface Props {
  kind: ProviderKind;
  /** Slug do endpoint (preview do path final). Vazio → mostra {slug}. */
  slug?: string;
}

/**
 * ProviderHelp — bloco "Como usar" exibido no form de endpoint.
 *
 * Pattern de não-vazamento (lições do bug 2026-05-22):
 *
 *   - Toda string longa usa `break-all` (quebra em qualquer char). Sem
 *     truncate, sem ellipsis: o texto fica visível em múltiplas linhas.
 *     Essa decisão prioriza legibilidade sobre estética — em telas
 *     estreitas, ver a URL inteira embolada é melhor do que ver "..." e
 *     ter que passar o mouse para descobrir.
 *
 *   - `<pre>` recebe `whitespace-pre-wrap break-all` para que o exemplo do
 *     request quebre em qualquer caractere quando estreito, em vez de
 *     forçar scroll horizontal que estoura o modal pai.
 *
 *   - Containers raiz têm `min-w-0` para que filhos `break-all` consigam
 *     encolher abaixo do tamanho natural (CSS flex item default tem
 *     `min-width: auto`, que é o tamanho do conteúdo).
 */
export function ProviderHelp({ kind, slug }: Props) {
  const meta = providerMeta(kind);
  const [open, setOpen] = useState(false);

  if (!meta.exampleURLs && !meta.requestExample) {
    return null;
  }

  const previewSlug = slug?.trim() ? slug.trim() : "{slug}";
  const requestPath = meta.requestExample
    ? `/v1/proxy/${previewSlug}${meta.requestExample.path}`
    : "";

  return (
    <div className="min-w-0 space-y-2">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="inline-flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground"
      >
        <Info className="h-3.5 w-3.5" />
        {open ? "Ocultar" : "Mostrar"} exemplo de uso desse provider
      </button>

      {open && (
        <Alert className="min-w-0 max-w-full overflow-hidden">
          <AlertTitle className="text-sm">
            Como cadastrar e como o consumer faz request — {meta.label}
          </AlertTitle>
          <AlertDescription className="space-y-4 text-xs">
            {meta.exampleURLs && meta.exampleURLs.length > 0 && (
              <section className="min-w-0">
                <p className="mb-2 font-medium text-foreground/90">
                  URL upstream (cole no campo URL do target):
                </p>
                <ul className="space-y-1.5">
                  {meta.exampleURLs.map((url) => (
                    <li key={url}>
                      <UrlLine text={url} />
                    </li>
                  ))}
                </ul>
              </section>
            )}

            {meta.requestExample && (
              <section className="min-w-0">
                <p className="mb-2 font-medium text-foreground/90">
                  Request do consumer (via gateway):
                </p>
                <div className="space-y-2 rounded border border-border bg-background/60 p-3 font-mono text-[11px]">
                  <div className="min-w-0">
                    <span className="font-semibold text-emerald-400">
                      {meta.requestExample.method}
                    </span>{" "}
                    <span className="break-all">{requestPath}</span>
                  </div>
                  <div className="min-w-0">
                    <span className="text-muted-foreground">Authorization:</span>{" "}
                    <span className="break-all text-amber-300">
                      Bearer gwk_{"{prefix}_{secret}"}
                    </span>
                  </div>
                  <div className="min-w-0">
                    <span className="text-muted-foreground">Content-Type:</span>{" "}
                    application/json
                  </div>
                  {meta.requestExample.body && (
                    <pre className="mt-2 min-w-0 max-w-full whitespace-pre-wrap break-all border-t border-border pt-2 leading-relaxed">
                      {meta.requestExample.body}
                    </pre>
                  )}
                </div>
                <p className="mt-1.5 text-[10px] text-muted-foreground">
                  O gateway aceita o token <code>gwk_…</code> da aplicação e injeta
                  a chave do upstream automaticamente; o cliente nunca vê a
                  credencial real.
                </p>
              </section>
            )}

            {meta.requestExample?.notes && meta.requestExample.notes.length > 0 && (
              <section>
                <p className="mb-1 font-medium text-foreground/90">Notas:</p>
                <ul className="list-disc space-y-0.5 pl-4">
                  {meta.requestExample.notes.map((n) => (
                    <li key={n} className="break-words">
                      {n}
                    </li>
                  ))}
                </ul>
              </section>
            )}

            {meta.docs && (
              <a
                href={meta.docs}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-1 text-primary hover:underline"
              >
                Documentação oficial <ExternalLink className="h-3 w-3" />
              </a>
            )}
          </AlertDescription>
        </Alert>
      )}
    </div>
  );
}

/**
 * UrlLine — exibe uma URL longa em uma linha com botão de copiar. `break-all`
 * garante que URLs gigantes (Azure deploy names) fiquem visíveis quebrando
 * em múltiplas linhas em vez de cortar com elipse.
 */
function UrlLine({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      toast.success("Copiado");
      setTimeout(() => setCopied(false), 2000);
    } catch {
      toast.error("Falha ao copiar — selecione manualmente.");
    }
  }
  return (
    <div className="flex min-w-0 items-start gap-2 rounded border border-border bg-background/60 px-2 py-1.5">
      <code className="min-w-0 flex-1 break-all font-mono text-[11px] text-foreground/90">
        {text}
      </code>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="h-6 w-6 shrink-0"
        onClick={copy}
        title="Copiar"
      >
        {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
      </Button>
    </div>
  );
}
