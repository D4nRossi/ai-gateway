import { useEffect, useMemo, useState, type FormEvent } from "react";
import { Loader2, Play, Wand2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { toast } from "@/components/ui/sonner";
import { api, errMessage, type ProxyEndpoint } from "@/lib/api";
import { providerMeta } from "@/lib/providers";
import { ProviderBadge } from "@/components/ProviderSelector";

/**
 * Playground — chamadas ad-hoc ao gateway pela UI.
 *
 * Substitui curl/Postman para validação rápida. Permite:
 *   - Escolher um endpoint cadastrado (popula slug + provider)
 *   - Colar o token gwk_… da aplicação consumidora
 *   - Definir método, path (após /v1/proxy/{slug}), body
 *   - Auto-preencher path + body a partir do exemplo do provider
 *   - Disparar via fetch (mesma origem) e mostrar status + latência + headers
 *     + corpo da resposta
 *
 * O token gwk_ NUNCA é persistido (não cai em sessionStorage/localStorage);
 * vive apenas no state local da página. Ao sair da página, evapora.
 */

const METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE"] as const;
type Method = (typeof METHODS)[number];

interface Response {
  status: number;
  statusText: string;
  latencyMs: number;
  headers: Record<string, string>;
  body: string;
  isJson: boolean;
}

export default function Playground() {
  const [endpoints, setEndpoints] = useState<ProxyEndpoint[]>([]);
  const [loading, setLoading] = useState(true);

  const [slug, setSlug] = useState<string>("");
  const [token, setToken] = useState("");
  const [method, setMethod] = useState<Method>("POST");
  const [path, setPath] = useState("");
  const [body, setBody] = useState("");

  const [response, setResponse] = useState<Response | null>(null);
  const [running, setRunning] = useState(false);

  useEffect(() => {
    setLoading(true);
    api
      .listEndpoints()
      .then((eps) => {
        const active = eps.filter((e) => e.active);
        setEndpoints(active);
        if (active.length > 0 && !slug) setSlug(active[0].slug);
      })
      .catch((err) => toast.error(errMessage(err, "Falha ao carregar endpoints")))
      .finally(() => setLoading(false));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const selectedEndpoint = useMemo(
    () => endpoints.find((e) => e.slug === slug),
    [endpoints, slug],
  );

  function fillExampleFromProvider() {
    if (!selectedEndpoint) {
      toast.error("Selecione um endpoint primeiro");
      return;
    }
    const meta = providerMeta(selectedEndpoint.provider_kind);
    if (!meta.requestExample) {
      toast.error(`Sem exemplo cadastrado para o provider ${meta.label}`);
      return;
    }
    setMethod(meta.requestExample.method);
    setPath(meta.requestExample.path);
    setBody(meta.requestExample.body ?? "");
    toast.success("Exemplo carregado");
  }

  async function dispatch(e: FormEvent) {
    e.preventDefault();
    if (!slug) {
      toast.error("Selecione um endpoint");
      return;
    }
    if (!token.trim()) {
      toast.error("Cole o token da aplicação");
      return;
    }

    // Constrói URL final no proxy: /v1/proxy/{slug}{path}.
    const trimmedPath = path.startsWith("/") ? path : `/${path}`;
    const url = `/v1/proxy/${slug}${trimmedPath === "/" ? "" : trimmedPath}`;

    setRunning(true);
    setResponse(null);
    const start = performance.now();
    try {
      const headers: Record<string, string> = {
        Authorization: `Bearer ${token.trim()}`,
      };
      let bodyToSend: BodyInit | undefined;
      if (method !== "GET" && method !== "DELETE" && body.trim()) {
        headers["Content-Type"] = "application/json";
        bodyToSend = body;
      }
      const res = await fetch(url, {
        method,
        headers,
        body: bodyToSend,
        credentials: "same-origin",
      });
      const latency = Math.round(performance.now() - start);

      const text = await res.text();
      const contentType = res.headers.get("content-type") ?? "";
      const isJson = contentType.includes("application/json");

      const allHeaders: Record<string, string> = {};
      res.headers.forEach((value, key) => {
        allHeaders[key] = value;
      });

      setResponse({
        status: res.status,
        statusText: res.statusText,
        latencyMs: latency,
        headers: allHeaders,
        body: isJson ? prettyJSON(text) : text,
        isJson,
      });
    } catch (err) {
      const latency = Math.round(performance.now() - start);
      setResponse({
        status: 0,
        statusText: "network error",
        latencyMs: latency,
        headers: {},
        body: errMessage(err, "Falha de rede"),
        isJson: false,
      });
    } finally {
      setRunning(false);
    }
  }

  return (
    <div className="space-y-6">
      <p className="text-sm text-muted-foreground">
        Dispare chamadas para qualquer endpoint cadastrado direto pela UI — sem
        precisar de curl/Postman. O gateway aplica auth da app, encaminha para o
        target escolhido pelo load balancer e devolve a resposta verbatim.
      </p>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        {/* ── Request ─────────────────────────────────────────────────── */}
        <Card>
          <CardContent className="space-y-4 p-4">
            <h2 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              Request
            </h2>

            <form className="space-y-4" onSubmit={dispatch}>
              <div className="space-y-2">
                <Label>Endpoint</Label>
                <Select value={slug} onValueChange={setSlug} disabled={loading}>
                  <SelectTrigger>
                    <SelectValue placeholder="Escolha um endpoint…" />
                  </SelectTrigger>
                  <SelectContent>
                    {endpoints.length === 0 ? (
                      <div className="px-3 py-2 text-sm text-muted-foreground">
                        Nenhum endpoint ativo
                      </div>
                    ) : (
                      endpoints.map((ep) => (
                        <SelectItem key={ep.id} value={ep.slug}>
                          {ep.name} ({ep.slug}) — {ep.provider_kind}
                        </SelectItem>
                      ))
                    )}
                  </SelectContent>
                </Select>
                {selectedEndpoint && (
                  <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
                    <ProviderBadge kind={selectedEndpoint.provider_kind ?? "custom"} />
                    <span>
                      {selectedEndpoint.targets?.length ?? 0} target(s) ·{" "}
                      {selectedEndpoint.lb_strategy}
                    </span>
                  </div>
                )}
              </div>

              <div className="space-y-2">
                <Label>Token da aplicação</Label>
                <Input
                  type="password"
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                  placeholder="gwk_appdemo_64hex…"
                  className="font-mono text-xs"
                  required
                />
                <p className="text-[11px] text-muted-foreground">
                  Não é persistido — limpa ao sair da página.
                </p>
              </div>

              <div className="grid grid-cols-[120px_1fr] gap-2">
                <div className="space-y-2">
                  <Label>Método</Label>
                  <Select value={method} onValueChange={(v) => setMethod(v as Method)}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {METHODS.map((m) => (
                        <SelectItem key={m} value={m}>
                          {m}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label>Path (após /v1/proxy/{slug || "{slug}"})</Label>
                  <Input
                    value={path}
                    onChange={(e) => setPath(e.target.value)}
                    placeholder="/openai/deployments/gpt-4o/chat/completions?api-version=2024-08-01-preview"
                    className="font-mono text-xs"
                  />
                </div>
              </div>

              {method !== "GET" && method !== "DELETE" && (
                <div className="space-y-2">
                  <Label>Body (JSON)</Label>
                  <textarea
                    value={body}
                    onChange={(e) => setBody(e.target.value)}
                    rows={10}
                    placeholder='{"messages":[{"role":"user","content":"Olá!"}]}'
                    className="w-full rounded-md border border-input bg-background/40 px-3 py-2 font-mono text-xs shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  />
                </div>
              )}

              <div className="flex flex-wrap items-center gap-2">
                <Button type="submit" disabled={running || !slug}>
                  {running ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    <Play className="h-4 w-4" />
                  )}
                  Disparar
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  onClick={fillExampleFromProvider}
                  disabled={!selectedEndpoint}
                >
                  <Wand2 className="h-4 w-4" />
                  Preencher exemplo do provider
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>

        {/* ── Response ────────────────────────────────────────────────── */}
        <Card>
          <CardContent className="space-y-4 p-4">
            <h2 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              Response
            </h2>

            {!response ? (
              <Alert>
                <AlertTitle>Sem resposta ainda</AlertTitle>
                <AlertDescription>
                  Preencha o formulário ao lado e clique em "Disparar" para ver o
                  resultado.
                </AlertDescription>
              </Alert>
            ) : (
              <div className="space-y-3">
                <div className="flex flex-wrap items-center gap-2">
                  <Badge
                    variant={
                      response.status === 0
                        ? "destructive"
                        : response.status >= 500
                          ? "destructive"
                          : response.status >= 400
                            ? "warning"
                            : "success"
                    }
                    className="font-mono"
                  >
                    {response.status === 0
                      ? "ERR"
                      : `${response.status} ${response.statusText}`}
                  </Badge>
                  <Badge variant="outline" className="font-mono">
                    {response.latencyMs} ms
                  </Badge>
                </div>

                <div>
                  <Label className="text-xs">Headers</Label>
                  <pre className="mt-1 max-h-40 overflow-auto rounded border border-border bg-background/60 p-2 font-mono text-[11px]">
                    {Object.entries(response.headers)
                      .map(([k, v]) => `${k}: ${v}`)
                      .join("\n")}
                  </pre>
                </div>

                <div>
                  <Label className="text-xs">Body</Label>
                  <pre className="mt-1 max-h-96 overflow-auto rounded border border-border bg-background/60 p-2 font-mono text-[11px] leading-relaxed">
                    {response.body}
                  </pre>
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function prettyJSON(text: string): string {
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return text;
  }
}
