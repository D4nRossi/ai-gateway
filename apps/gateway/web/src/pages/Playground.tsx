import { useEffect, useMemo, useState, type FormEvent } from "react";
import { BookOpen, Info, Loader2, Play, Wand2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { toast } from "@/components/ui/sonner";
import { api, errMessage, type ProxyEndpoint } from "@/lib/api";
import { providerMeta } from "@/lib/providers";
import { ProviderBadge } from "@/components/ProviderSelector";
import {
  EXAMPLE_CATEGORIES,
  PLAYGROUND_EXAMPLES,
  exampleBody,
  type PlaygroundExample,
} from "@/lib/playgroundExamples";

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

  // azureModel é só usado no modo Azure (provider_kind === "azure_openai").
  // Quando definido, sobrescreve o campo "model" no body enviado.
  const [azureModel, setAzureModel] = useState("");

  // appliedExample mantém o último exemplo carregado pelo catálogo. Usado
  // pra exibir a guidance ao lado do form e pra reaplicar quando o modelo
  // Azure muda. Limpo quando o operador edita manualmente o body.
  const [appliedExample, setAppliedExample] = useState<PlaygroundExample | null>(null);

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

  // Modo Azure: quando o endpoint selecionado tem provider_kind=azure_openai,
  // o gateway aceita path canônico (/chat/completions) e traduz para Azure
  // usando o model_to_deployment do provider_config (ADR-0017). O playground
  // expõe isso como um dropdown de model + body OpenAI-style pré-preenchido,
  // escondendo o campo de path bruto.
  const isAzureMode = selectedEndpoint?.provider_kind === "azure_openai";
  const azureModels = useMemo<string[]>(() => {
    if (!isAzureMode) return [];
    const map = selectedEndpoint?.provider_config?.model_to_deployment as
      | Record<string, unknown>
      | undefined;
    return map ? Object.keys(map).filter((k) => typeof map[k] === "string") : [];
  }, [isAzureMode, selectedEndpoint]);

  // Sempre que troca de endpoint, ajusta o estado pra refletir o modo.
  // Azure: zera path, força POST, pré-seleciona o primeiro model e gera body.
  // Outros: limpa o azureModel.
  useEffect(() => {
    if (isAzureMode) {
      setPath("/chat/completions");
      setMethod("POST");
      const first = azureModels[0] ?? "";
      setAzureModel(first);
      if (first) setBody(defaultAzureBody(first));
    } else {
      setAzureModel("");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [slug, isAzureMode]);

  function fillExampleFromProvider() {
    if (!selectedEndpoint) {
      toast.error("Selecione um endpoint primeiro");
      return;
    }
    if (isAzureMode) {
      // No modo Azure o body já é gerado a partir do dropdown — refresh
      // explícito reaplica o template caso o operador tenha rabiscado o body.
      const first = azureModels[0] ?? azureModel;
      if (first) {
        setAzureModel(first);
        setBody(defaultAzureBody(first));
        toast.success("Body OpenAI-style atualizado");
      }
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

  // Atualiza o campo "model" no body sem perder o resto que o operador
  // possivelmente já editou. Se o body não for JSON válido, regenera do zero.
  function setAzureModelAndUpdateBody(model: string) {
    setAzureModel(model);
    try {
      const parsed = JSON.parse(body);
      parsed.model = model;
      setBody(JSON.stringify(parsed, null, 2));
    } catch {
      setBody(defaultAzureBody(model));
    }
  }

  /**
   * applyExample popula method/path/body a partir de um exemplo curado do
   * catálogo. Em modo Azure, o "model" no body é trocado pelo modelo
   * atualmente selecionado no dropdown (ou o primeiro disponível).
   */
  function applyExample(id: string) {
    const ex = PLAYGROUND_EXAMPLES.find((e) => e.id === id);
    if (!ex) return;
    setAppliedExample(ex);

    if (isAzureMode) {
      // Azure: usa o azureModel atual (ou primeiro disponível) como model do body.
      const model = azureModel || azureModels[0] || "MODEL_PLACEHOLDER";
      setBody(exampleBody(ex, model));
      setMethod("POST");
      setPath("/chat/completions");
    } else {
      // Modo raw: usa o que estiver no body.model do exemplo (geralmente
      // o placeholder, que o operador troca manualmente).
      setBody(exampleBody(ex, (ex.body.model as string) ?? ""));
      setMethod(ex.rawMethod ?? "POST");
      setPath(ex.rawPath ?? "/chat/completions");
    }
  }

  // Quando o operador edita o body manualmente, limpa a referência ao
  // exemplo aplicado (a guidance some pra não confundir).
  function handleBodyChange(next: string) {
    setBody(next);
    if (appliedExample) setAppliedExample(null);
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

              {isAzureMode ? (
                /*
                 * Modo Azure: o cliente fala dialecto OpenAI-style canônico.
                 * O playground esconde método/path (sempre POST /chat/completions)
                 * e expõe apenas o seletor de model, populado a partir do
                 * provider_config do endpoint (ADR-0017).
                 */
                <div className="space-y-2">
                  <Label>Model</Label>
                  {azureModels.length === 0 ? (
                    <p className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-[11px] text-destructive">
                      Este endpoint Azure não tem nenhum modelo configurado.
                      Edite o endpoint em <strong>Endpoints</strong> e adicione
                      pelo menos um mapeamento <code>model → deployment</code>.
                    </p>
                  ) : (
                    <Select
                      value={azureModel}
                      onValueChange={setAzureModelAndUpdateBody}
                    >
                      <SelectTrigger>
                        <SelectValue placeholder="Escolha um modelo…" />
                      </SelectTrigger>
                      <SelectContent>
                        {azureModels.map((m) => (
                          <SelectItem key={m} value={m}>
                            {m}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  )}
                  <p className="text-[11px] text-muted-foreground">
                    Path fica fixo <code className="font-mono">/chat/completions</code>{" "}
                    — o gateway traduz para a URL Azure.
                  </p>
                </div>
              ) : (
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
              )}

              {method !== "GET" && method !== "DELETE" && (
                <div className="space-y-2">
                  <Label>Body (JSON)</Label>
                  <textarea
                    value={body}
                    onChange={(e) => handleBodyChange(e.target.value)}
                    rows={10}
                    placeholder='{"messages":[{"role":"user","content":"Olá!"}]}'
                    className="w-full rounded-md border border-input bg-background/40 px-3 py-2 font-mono text-xs shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  />
                </div>
              )}

              {appliedExample && (
                <Alert>
                  <Info className="h-4 w-4" />
                  <AlertTitle className="text-xs">{appliedExample.title}</AlertTitle>
                  <AlertDescription className="space-y-1 text-[11px]">
                    <p>{appliedExample.description}</p>
                    <p className="text-muted-foreground">
                      <strong>Esperado:</strong> {appliedExample.tierGuidance}
                    </p>
                    {appliedExample.requires && (
                      <p className="text-muted-foreground">
                        <strong>Requer:</strong> {appliedExample.requires}
                      </p>
                    )}
                  </AlertDescription>
                </Alert>
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

                {/* Catálogo curado de cenários de teste. Cada item popula o
                    body com um prompt que dispara um caminho conhecido do
                    pipeline (PII, prompt injection, etc.). */}
                <Select onValueChange={applyExample} value="">
                  <SelectTrigger className="w-auto gap-2">
                    <BookOpen className="h-4 w-4" />
                    <SelectValue placeholder="Carregar exemplo…" />
                  </SelectTrigger>
                  <SelectContent>
                    {EXAMPLE_CATEGORIES.map((cat) => {
                      const items = PLAYGROUND_EXAMPLES.filter(
                        (e) => e.category === cat.id,
                      );
                      if (items.length === 0) return null;
                      return (
                        <SelectGroup key={cat.id}>
                          <SelectLabel>{cat.label}</SelectLabel>
                          {items.map((ex) => (
                            <SelectItem key={ex.id} value={ex.id}>
                              {ex.title}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      );
                    })}
                  </SelectContent>
                </Select>

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

// defaultAzureBody renders a minimal OpenAI-style chat completion request
// for the given model name. Kept here (not in providers.ts) because it's
// playground-specific scaffolding, not a runtime contract.
function defaultAzureBody(model: string): string {
  return JSON.stringify(
    {
      model,
      messages: [{ role: "user", content: "" }],
      max_tokens: 200,
    },
    null,
    2,
  );
}
