import { useState } from "react";
import { Check, Copy, ExternalLink, Info } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { providerMeta, type ProviderKind } from "@/lib/providers";
import { toast } from "@/components/ui/sonner";

interface Props {
  kind: ProviderKind;
  /** Slug do endpoint (preview do path final no proxy). Se vazio, mostra {slug}. */
  slug?: string;
}

/**
 * ProviderHelp — bloco "Como usar" exibido no form de endpoint.
 *
 * Constraints visuais críticas (resolvem bug reportado 2026-05-22 — URLs
 * Azure vazando do modal):
 *
 *   - O Alert é flex container vertical; conteúdo precisa de min-w-0 para
 *     que filhos truncate consigam encolher abaixo do tamanho natural
 *   - <pre> do request usa whitespace-pre-wrap + break-all em vez de scroll-x;
 *     URLs longas quebram em qualquer caractere, evitando bandeira horizontal
 *   - Cada URL na lista é um flex row com span truncate + botão copy;
 *     truncate na span requer min-w-0 no irmão flex
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
        <Alert className="min-w-0 overflow-hidden">
          <AlertTitle className="text-sm">
            Como cadastrar e como o consumer faz request — {meta.label}
          </AlertTitle>
          <AlertDescription className="space-y-3 text-xs">
            {meta.exampleURLs && meta.exampleURLs.length > 0 && (
              <div className="min-w-0">
                <p className="mb-1 font-medium text-foreground/90">
                  URL upstream (cole no campo URL do target):
                </p>
                <ul className="space-y-1">
                  {meta.exampleURLs.map((url) => (
                    <li
                      key={url}
                      className="flex min-w-0 items-center gap-2 overflow-hidden rounded border border-border bg-background/60 px-2 py-1 font-mono text-[11px]"
                    >
                      <CopyableText text={url} />
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {meta.requestExample && (
              <div className="min-w-0">
                <p className="mb-1 font-medium text-foreground/90">
                  Request do consumer (via gateway):
                </p>
                <pre className="min-w-0 max-w-full whitespace-pre-wrap break-all rounded border border-border bg-background/60 p-2 font-mono text-[11px] leading-relaxed">
                  <span className="text-emerald-400">{meta.requestExample.method}</span>{" "}
                  {requestPath}
                  <br />
                  <span className="text-muted-foreground">Authorization:</span>{" "}
                  <span className="text-amber-300">Bearer gwk_{"{prefix}_{secret}"}</span>
                  <br />
                  <span className="text-muted-foreground">Content-Type:</span> application/json
                  {meta.requestExample.body && (
                    <>
                      {"\n\n"}
                      {meta.requestExample.body}
                    </>
                  )}
                </pre>
                <p className="mt-1 text-[10px] text-muted-foreground">
                  O gateway aceita o token <code>gwk_…</code> da aplicação e injeta
                  a chave do upstream automaticamente; o cliente nunca vê a
                  credencial real.
                </p>
              </div>
            )}

            {meta.requestExample?.notes && meta.requestExample.notes.length > 0 && (
              <div>
                <p className="mb-1 font-medium text-foreground/90">Notas:</p>
                <ul className="list-disc space-y-0.5 pl-4">
                  {meta.requestExample.notes.map((n) => (
                    <li key={n}>{n}</li>
                  ))}
                </ul>
              </div>
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
 * CopyableText — span com truncate + botão copy.
 *
 * O span TEM que ter min-w-0 (não basta o flex-1) porque o conteúdo mínimo
 * dele é a string completa da URL — sem min-w-0 ele se recusa a encolher
 * abaixo desse mínimo e estoura o container.
 */
function CopyableText({ text }: { text: string }) {
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
    <>
      <span className="min-w-0 flex-1 truncate" title={text}>
        {text}
      </span>
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
    </>
  );
}
