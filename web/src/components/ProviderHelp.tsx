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
 * Mostra três coisas pro operador entender o cadastro:
 *   1. Exemplos reais de URL upstream pra esse provider
 *      (Azure tem N URLs — uma por deploy; OpenAI/Anthropic têm uma só)
 *   2. Exemplo de request final que o consumer vai fazer via gateway
 *      (com o slug substituído quando já existir)
 *   3. Notas específicas do provider (versões de API, formatos diferentes, etc.)
 *
 * O bloco fica colapsado por padrão pra não poluir o form; o operador
 * expande quando precisa.
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
    <div className="space-y-2">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="inline-flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground"
      >
        <Info className="h-3.5 w-3.5" />
        {open ? "Ocultar" : "Mostrar"} exemplo de uso desse provider
      </button>

      {open && (
        <Alert>
          <AlertTitle className="text-sm">
            Como cadastrar e como o consumer faz request — {meta.label}
          </AlertTitle>
          <AlertDescription className="space-y-3 text-xs">
            {meta.exampleURLs && meta.exampleURLs.length > 0 && (
              <div>
                <p className="mb-1 font-medium text-foreground/90">
                  URL upstream (cole no campo URL do target):
                </p>
                <ul className="space-y-1">
                  {meta.exampleURLs.map((url) => (
                    <li
                      key={url}
                      className="flex items-center gap-2 rounded border border-border bg-background/60 px-2 py-1 font-mono text-[11px]"
                    >
                      <CopyableText text={url} />
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {meta.requestExample && (
              <div>
                <p className="mb-1 font-medium text-foreground/90">
                  Request do consumer (via gateway):
                </p>
                <pre className="overflow-x-auto rounded border border-border bg-background/60 p-2 font-mono text-[11px] leading-relaxed">
                  <span className="text-emerald-400">{meta.requestExample.method}</span>{" "}
                  {requestPath}
                  <br />
                  <span className="text-muted-foreground">Authorization:</span>{" "}
                  <span className="text-amber-300">Bearer gwk_{"{prefix}_{secret}"}</span>
                  <br />
                  <span className="text-muted-foreground">Content-Type:</span> application/json
                  {meta.requestExample.body && (
                    <>
                      <br />
                      <br />
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
      <span className="flex-1 truncate">{text}</span>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="h-6 w-6"
        onClick={copy}
        title="Copiar"
      >
        {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
      </Button>
    </>
  );
}
