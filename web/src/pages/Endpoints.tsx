import { useEffect, useMemo, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import {
  ChevronRight,
  Eye,
  Loader2,
  MoreHorizontal,
  Network,
  Pencil,
  Plus,
  Trash2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Separator } from "@/components/ui/separator";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { toast } from "@/components/ui/sonner";
import { DataTableToolbar } from "@/components/DataTableToolbar";
import { ProviderSelector, ProviderBadge } from "@/components/ProviderSelector";
import { ProviderHelp } from "@/components/ProviderHelp";
import {
  api,
  errMessage,
  errToast,
  type AuthType,
  type LBStrategy,
  type ProviderKind,
  type ProxyEndpoint,
  type TargetAuthInput,
} from "@/lib/api";
import { PROVIDERS } from "@/lib/providers";
import { filterByText } from "@/lib/filter";
import { formatDateTime, formatNumber } from "@/lib/utils";
import { suggestSlug, validateSlug } from "@/lib/slug";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Info, Lightbulb, Sparkle } from "lucide-react";

const STRATEGIES: { value: LBStrategy; label: string }[] = [
  { value: "round_robin", label: "round_robin" },
  { value: "weighted_round_robin", label: "weighted_round_robin" },
  { value: "random", label: "random" },
  { value: "least_connections", label: "least_connections" },
  { value: "ip_hash", label: "ip_hash" },
];

export default function Endpoints() {
  const [endpoints, setEndpoints] = useState<ProxyEndpoint[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState("");
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<ProxyEndpoint | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<ProxyEndpoint | null>(null);

  async function refresh() {
    setLoading(true);
    try {
      // List returns endpoints without targets; targets are loaded per-endpoint on demand.
      const list = await api.listEndpoints();
      setEndpoints(list);
    } catch (err) {
      toast.error(errMessage(err, "Falha ao carregar endpoints"));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  const filtered = useMemo(
    () =>
      filterByText(endpoints, search, (e) => [
        e.slug,
        e.name,
        e.lb_strategy,
        e.provider_kind,
        PROVIDERS[(e.provider_kind ?? "custom") as ProviderKind]?.label,
      ]),
    [endpoints, search],
  );

  return (
    <div className="space-y-6">
      <p className="text-sm text-muted-foreground">
        Endpoints HTTP genéricos proxied pelo gateway, com targets e estratégia de balanceamento.
      </p>

      <DataTableToolbar
        search={search}
        onSearchChange={setSearch}
        onRefresh={refresh}
        refreshing={loading}
        placeholder="Buscar por slug, nome ou estratégia…"
        rightSlot={
          <Button onClick={() => setCreating(true)}>
            <Plus className="h-4 w-4" />
            Novo endpoint
          </Button>
        }
      />

      <Card>
        <CardContent className="p-0">
          {loading ? (
            <div className="space-y-3 p-6">
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
            </div>
          ) : endpoints.length === 0 ? (
            <div className="flex flex-col items-center justify-center px-6 py-16 text-center">
              <Network className="mb-3 h-8 w-8 text-muted-foreground" />
              <p className="text-sm text-muted-foreground">Nenhum endpoint cadastrado ainda.</p>
              <Button className="mt-4" onClick={() => setCreating(true)}>
                <Plus className="h-4 w-4" />
                Criar primeiro endpoint
              </Button>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Slug</TableHead>
                  <TableHead>Nome</TableHead>
                  <TableHead>Provider</TableHead>
                  <TableHead>Estratégia</TableHead>
                  <TableHead>Max RPS</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Atualizado em</TableHead>
                  <TableHead className="w-[160px]" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {filtered.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={8} className="px-4 py-10 text-center text-sm text-muted-foreground">
                      Nenhum endpoint corresponde ao filtro.
                    </TableCell>
                  </TableRow>
                ) : null}
                {filtered.map((ep) => (
                  <TableRow key={ep.id}>
                    <TableCell className="font-mono text-xs">
                      <Link to={`/endpoints/${ep.id}`} className="hover:text-primary hover:underline">
                        {ep.slug}
                      </Link>
                    </TableCell>
                    <TableCell className="font-medium">{ep.name}</TableCell>
                    <TableCell>
                      <ProviderBadge kind={ep.provider_kind ?? "custom"} />
                    </TableCell>
                    <TableCell>
                      <Badge variant="outline" className="font-mono text-[10px]">
                        {ep.lb_strategy}
                      </Badge>
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {ep.max_rps > 0 ? formatNumber(ep.max_rps) : "∞"}
                    </TableCell>
                    <TableCell>
                      {ep.active ? (
                        <Badge variant="success">Ativo</Badge>
                      ) : (
                        <Badge variant="muted">Inativo</Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {formatDateTime(ep.updated_at)}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center justify-end gap-1">
                        <Button asChild variant="ghost" size="sm" className="h-8">
                          <Link to={`/endpoints/${ep.id}`}>
                            Detalhes
                            <ChevronRight className="h-3 w-3" />
                          </Link>
                        </Button>
                        <DropdownMenu>
                          <DropdownMenuTrigger asChild>
                            <Button variant="ghost" size="icon" className="h-8 w-8">
                              <MoreHorizontal className="h-4 w-4" />
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            <DropdownMenuItem asChild>
                              <Link to={`/endpoints/${ep.id}`}>
                                <Eye className="h-4 w-4" />
                                Ver detalhes
                              </Link>
                            </DropdownMenuItem>
                            <DropdownMenuItem onSelect={() => setEditing(ep)}>
                              <Pencil className="h-4 w-4" />
                              Editar
                            </DropdownMenuItem>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem
                              variant="destructive"
                              onSelect={() => setConfirmDelete(ep)}
                            >
                              <Trash2 className="h-4 w-4" />
                              Desativar
                            </DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <EndpointFormDialog
        open={creating || editing !== null}
        existing={editing}
        onClose={() => {
          setCreating(false);
          setEditing(null);
        }}
        onSaved={() => {
          setCreating(false);
          setEditing(null);
          void refresh();
        }}
      />

      <Dialog
        open={confirmDelete !== null}
        onOpenChange={(o) => {
          if (!o) setConfirmDelete(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Desativar endpoint</DialogTitle>
            <DialogDescription>
              O endpoint <strong>{confirmDelete?.slug}</strong> deixa de aceitar chamadas
              imediatamente.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmDelete(null)}>
              Cancelar
            </Button>
            <Button
              variant="destructive"
              onClick={async () => {
                if (!confirmDelete) return;
                try {
                  await api.deleteEndpoint(confirmDelete.id);
                  toast.success("Endpoint desativado");
                  setConfirmDelete(null);
                  void refresh();
                } catch (err) {
                  toast.error(errMessage(err, "Falha ao desativar"));
                }
              }}
            >
              Desativar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

interface EndpointForm {
  slug: string;
  name: string;
  provider_kind: ProviderKind;
  lb_strategy: LBStrategy;
  max_rps: number;
  max_monthly_requests: number;
  active: boolean;
}

const EMPTY_EP_FORM: EndpointForm = {
  slug: "",
  name: "",
  provider_kind: "custom",
  lb_strategy: "round_robin",
  max_rps: 0,
  max_monthly_requests: 0,
  active: true,
};

/**
 * Auto-preenche name + lb_strategy quando o operador escolhe um provider.
 * Slug é deixado em branco (operador define o nome lógico que faz sentido
 * para o domínio dele — "chat", "embeddings", "transcricao", etc.).
 */
function formForProvider(provider: ProviderKind, prev: EndpointForm): EndpointForm {
  const meta = PROVIDERS[provider];
  return {
    ...prev,
    provider_kind: provider,
    // Mantém nome do usuário se já tiver digitado; senão usa label do provider.
    name: prev.name || meta.label,
    // Estratégia default sugerida pelo provider (e.g., least_connections para
    // Ollama/vLLM locais que costumam ser single-instance).
    lb_strategy: meta.defaultStrategy ?? prev.lb_strategy,
  };
}

/**
 * Estado opcional do "primeiro target" — se o operador marcar
 * "Já cadastrar o primeiro target agora", esses campos aparecem no form
 * e o submit cria endpoint + target em sequência.
 */
interface FirstTargetForm {
  enabled: boolean;
  url: string;
  auth: TargetAuthInput;
}

function emptyFirstTarget(provider: ProviderKind): FirstTargetForm {
  const meta = PROVIDERS[provider];
  return {
    enabled: provider !== "custom", // se provider conhecido, vem ligado por default
    url: meta.baseURL,
    auth: { type: meta.authType, header: meta.authHeader },
  };
}

function EndpointFormDialog({
  open,
  existing,
  onClose,
  onSaved,
}: {
  open: boolean;
  existing: ProxyEndpoint | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [form, setForm] = useState<EndpointForm>(EMPTY_EP_FORM);
  const [firstTarget, setFirstTarget] = useState<FirstTargetForm>(emptyFirstTarget("custom"));
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (existing) {
      setForm({
        slug: existing.slug,
        name: existing.name,
        provider_kind: existing.provider_kind ?? "custom",
        lb_strategy: existing.lb_strategy,
        max_rps: existing.max_rps,
        max_monthly_requests: existing.max_monthly_requests,
        active: existing.active,
      });
      setFirstTarget({ enabled: false, url: "", auth: { type: "none" } });
    } else {
      setForm(EMPTY_EP_FORM);
      setFirstTarget(emptyFirstTarget("custom"));
    }
  }, [existing, open]);

  // Quando o provider muda, atualiza tanto o form quanto o pre-fill do target.
  function pickProvider(kind: ProviderKind) {
    setForm(formForProvider(kind, form));
    if (!existing) {
      setFirstTarget(emptyFirstTarget(kind));
    }
  }

  // Validação client-side do slug — feedback inline imediato.
  const slugIssue = form.slug ? validateSlug(form.slug) : null;
  const slugSuggested = form.slug ? suggestSlug(form.slug) : "";
  const showSuggestion =
    !!slugIssue && !!slugSuggested && slugSuggested !== form.slug && slugSuggested.length >= 2;

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (slugIssue) {
      toast.error("Identificador inválido", { description: slugIssue.message });
      return;
    }
    setSubmitting(true);
    try {
      if (existing) {
        await api.updateEndpoint(existing.id, form);
        toast.success("Endpoint atualizado");
        onSaved();
        return;
      }

      // Cria endpoint.
      const created = await api.createEndpoint(form);

      // Opcional: já cria primeiro target em sequência se o usuário marcou.
      if (firstTarget.enabled && firstTarget.url.trim()) {
        try {
          await api.addTarget(created.id, {
            url: firstTarget.url.trim(),
            weight: 1,
            auth: firstTarget.auth,
          });
          toast.success(`Endpoint criado com 1 target em ${created.slug}`);
        } catch (err) {
          // Endpoint OK, target falhou — informa mas não derruba o fluxo.
          toast.error(...errToast(err, "Endpoint criado, mas falha ao adicionar target"));
        }
      } else {
        toast.success("Endpoint criado", {
          description: "Próximo passo: adicione um Target com a URL real e a credencial.",
        });
      }
      onSaved();
    } catch (err) {
      toast.error(...errToast(err, "Falha ao salvar"));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
    >
      <DialogContent
        className="max-w-none sm:max-w-none"
        style={{ width: "min(1100px, 95vw)", maxWidth: "min(1100px, 95vw)" }}
      >
        <DialogHeader>
          <DialogTitle>{existing ? "Editar endpoint" : "Novo endpoint"}</DialogTitle>
          <DialogDescription>
            Um endpoint é um <strong>nome lógico</strong> (um slug curto) que vira
            uma rota no gateway: <code className="font-mono">/v1/proxy/{"{slug}"}</code>.
            A URL real do upstream vive nos <strong>Targets</strong> do endpoint —
            você pode ter mais de um, com balanceamento entre eles.
          </DialogDescription>
        </DialogHeader>

        {!existing && (
          <Alert>
            <Lightbulb className="h-4 w-4" />
            <AlertTitle>Como funciona</AlertTitle>
            <AlertDescription className="text-xs leading-relaxed">
              <strong>1.</strong> Aqui você define o <strong>nome lógico</strong>
              {" "}da rota (ex: <code className="font-mono">metlife</code>). Os consumers
              chamarão <code className="font-mono">/v1/proxy/metlife/…</code> no gateway.
              <br />
              <strong>2.</strong> O <strong>endereço real</strong> do Azure/OpenAI/etc.
              é um <strong>Target</strong> — uma URL com credencial cifrada. Você
              pode cadastrar agora ou depois, na aba Targets.
            </AlertDescription>
          </Alert>
        )}

        <form className="space-y-4" onSubmit={onSubmit}>
          <div className="space-y-2">
            <Label>Provider</Label>
            <ProviderSelector
              value={form.provider_kind}
              onChange={pickProvider}
              disabled={submitting}
            />
            <ProviderHelp kind={form.provider_kind} slug={form.slug} />
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="slug">
                Identificador da rota <span className="text-muted-foreground">(slug)</span>
              </Label>
              <Input
                id="slug"
                value={form.slug}
                onChange={(e) => setForm({ ...form, slug: e.target.value })}
                placeholder="ex: metlife"
                disabled={!!existing || submitting}
                aria-invalid={slugIssue ? true : undefined}
                required
              />
              {slugIssue ? (
                <div className="space-y-1">
                  <p className="text-[11px] text-destructive">{slugIssue.message}</p>
                  {showSuggestion && (
                    <button
                      type="button"
                      onClick={() => setForm({ ...form, slug: slugSuggested })}
                      className="inline-flex items-center gap-1 rounded border border-primary/40 bg-primary/10 px-2 py-0.5 text-[11px] text-primary hover:bg-primary/20"
                    >
                      <Sparkle className="h-3 w-3" />
                      Usar <code className="font-mono">{slugSuggested}</code>
                    </button>
                  )}
                </div>
              ) : form.slug ? (
                <p className="text-[11px] text-muted-foreground">
                  Vai ficar disponível em{" "}
                  <code className="font-mono text-foreground/80">
                    /v1/proxy/{form.slug}/…
                  </code>
                </p>
              ) : (
                <p className="text-[11px] text-muted-foreground">
                  Use letras minúsculas, dígitos e hífen. Ex: <code className="font-mono">metlife</code>,{" "}
                  <code className="font-mono">chat-default</code>,{" "}
                  <code className="font-mono">embeddings-prod</code>.
                </p>
              )}
            </div>
            <div className="space-y-2">
              <Label htmlFor="name">Nome de exibição</Label>
              <Input
                id="name"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="ex: Azure OpenAI — MetLife"
                disabled={submitting}
                required
              />
              <p className="text-[11px] text-muted-foreground">
                Apenas pra exibição no console — pode ter espaços, acentos, etc.
              </p>
            </div>
          </div>

          <div className="space-y-2">
            <Label>Estratégia de balanceamento</Label>
            <Select
              value={form.lb_strategy}
              onValueChange={(v) => setForm({ ...form, lb_strategy: v as LBStrategy })}
              disabled={submitting}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {STRATEGIES.map((s) => (
                  <SelectItem key={s.value} value={s.value}>
                    {s.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-[11px] text-muted-foreground">
              Distribui requisições entre múltiplos Targets do mesmo endpoint.
              Irrelevante quando há só um target.
            </p>
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="rps">Max RPS (0 = ilimitado)</Label>
              <Input
                id="rps"
                type="number"
                min={0}
                value={form.max_rps}
                onChange={(e) => setForm({ ...form, max_rps: Number(e.target.value) })}
                disabled={submitting}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="monthly">Limite mensal (0 = ilimitado)</Label>
              <Input
                id="monthly"
                type="number"
                min={0}
                value={form.max_monthly_requests}
                onChange={(e) => setForm({ ...form, max_monthly_requests: Number(e.target.value) })}
                disabled={submitting}
              />
            </div>
          </div>

          {/* Primeiro target inline — opcional, mas pré-marcado em providers conhecidos. */}
          {!existing && (
            <>
              <Separator />
              <div className="rounded-md border border-border bg-card/40 p-4 space-y-3">
                <div className="flex items-start justify-between gap-3">
                  <div className="flex-1 min-w-0">
                    <Label className="text-sm font-medium">
                      Já cadastrar o primeiro Target agora?
                    </Label>
                    <p className="mt-0.5 text-[11px] text-muted-foreground">
                      Recomendado. Sem pelo menos um target o endpoint não consegue
                      rotear chamadas. Você pode adicionar/editar depois.
                    </p>
                  </div>
                  <Switch
                    checked={firstTarget.enabled}
                    onCheckedChange={(c) => setFirstTarget({ ...firstTarget, enabled: c })}
                    disabled={submitting}
                  />
                </div>

                {firstTarget.enabled && (
                  <div className="space-y-3 border-t border-border pt-3">
                    <div className="space-y-2">
                      <Label htmlFor="target-url">URL upstream</Label>
                      <Input
                        id="target-url"
                        value={firstTarget.url}
                        onChange={(e) =>
                          setFirstTarget({ ...firstTarget, url: e.target.value })
                        }
                        placeholder="https://td-openai-tpcore-metlife-...openai.azure.com"
                        disabled={submitting}
                      />
                      <p className="text-[11px] text-muted-foreground">
                        Endereço REAL do serviço de IA. Aqui sim vai a URL completa.
                      </p>
                    </div>

                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                      <div className="space-y-2">
                        <Label>Tipo de autenticação</Label>
                        <Select
                          value={firstTarget.auth.type}
                          onValueChange={(v) =>
                            setFirstTarget({
                              ...firstTarget,
                              auth: { type: v as AuthType, header: firstTarget.auth.header },
                            })
                          }
                          disabled={submitting}
                        >
                          <SelectTrigger>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="none">none</SelectItem>
                            <SelectItem value="bearer_token">bearer_token</SelectItem>
                            <SelectItem value="api_key_header">api_key_header</SelectItem>
                            <SelectItem value="basic_auth">basic_auth</SelectItem>
                          </SelectContent>
                        </Select>
                      </div>
                      {firstTarget.auth.type === "api_key_header" && (
                        <div className="space-y-2">
                          <Label>Nome do header</Label>
                          <Input
                            value={firstTarget.auth.header ?? ""}
                            onChange={(e) =>
                              setFirstTarget({
                                ...firstTarget,
                                auth: { ...firstTarget.auth, header: e.target.value },
                              })
                            }
                            placeholder="api-key"
                            disabled={submitting}
                          />
                        </div>
                      )}
                    </div>

                    {firstTarget.auth.type === "bearer_token" && (
                      <div className="space-y-2">
                        <Label>Token</Label>
                        <Input
                          type="password"
                          value={firstTarget.auth.token ?? ""}
                          onChange={(e) =>
                            setFirstTarget({
                              ...firstTarget,
                              auth: { ...firstTarget.auth, token: e.target.value },
                            })
                          }
                          placeholder="sk-..."
                          disabled={submitting}
                        />
                      </div>
                    )}

                    {firstTarget.auth.type === "api_key_header" && (
                      <div className="space-y-2">
                        <Label>Valor do header (chave)</Label>
                        <Input
                          type="password"
                          value={firstTarget.auth.value ?? ""}
                          onChange={(e) =>
                            setFirstTarget({
                              ...firstTarget,
                              auth: { ...firstTarget.auth, value: e.target.value },
                            })
                          }
                          placeholder="cole aqui a chave do Azure / provider"
                          disabled={submitting}
                        />
                      </div>
                    )}

                    {firstTarget.auth.type === "basic_auth" && (
                      <div className="grid grid-cols-2 gap-3">
                        <div className="space-y-2">
                          <Label>Usuário</Label>
                          <Input
                            value={firstTarget.auth.username ?? ""}
                            onChange={(e) =>
                              setFirstTarget({
                                ...firstTarget,
                                auth: { ...firstTarget.auth, username: e.target.value },
                              })
                            }
                            disabled={submitting}
                          />
                        </div>
                        <div className="space-y-2">
                          <Label>Senha</Label>
                          <Input
                            type="password"
                            value={firstTarget.auth.password ?? ""}
                            onChange={(e) =>
                              setFirstTarget({
                                ...firstTarget,
                                auth: { ...firstTarget.auth, password: e.target.value },
                              })
                            }
                            disabled={submitting}
                          />
                        </div>
                      </div>
                    )}

                    <p className="flex items-start gap-2 text-[11px] text-muted-foreground">
                      <Info className="mt-0.5 h-3 w-3 shrink-0" />
                      Credenciais são cifradas em repouso com AES-256-GCM (ADR-0012).
                      O cliente do gateway nunca recebe a chave real.
                    </p>
                  </div>
                )}
              </div>
            </>
          )}

          {existing && (
            <>
              <Separator />
              <div className="flex items-center justify-between rounded-md border border-input bg-background/40 px-3 py-2">
                <div>
                  <p className="text-sm font-medium">Endpoint ativo</p>
                  <p className="text-xs text-muted-foreground">
                    Desativar bloqueia todas as chamadas para este slug.
                  </p>
                </div>
                <Switch
                  checked={form.active}
                  onCheckedChange={(c) => setForm({ ...form, active: c })}
                  disabled={submitting}
                />
              </div>
            </>
          )}

          <DialogFooter className="mt-2">
            <Button type="button" variant="outline" onClick={onClose} disabled={submitting}>
              Cancelar
            </Button>
            <Button type="submit" disabled={submitting || !!slugIssue}>
              {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
              {existing ? "Salvar" : "Criar endpoint"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ── Targets dialog ────────────────────────────────────────────────────────────
