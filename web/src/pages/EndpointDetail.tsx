import { useEffect, useMemo, useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  ArrowLeft,
  Loader2,
  MoreHorizontal,
  Pencil,
  Plus,
  Trash2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
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
import { ProviderBadge } from "@/components/ProviderSelector";
import {
  api,
  ApiError,
  errMessage,
  type Application,
  type AuthType,
  type ProxyEndpoint,
  type Target,
  type TargetAuthInput,
} from "@/lib/api";
import { providerMeta } from "@/lib/providers";
import { formatDateTime, formatNumber } from "@/lib/utils";

export default function EndpointDetail() {
  const params = useParams<{ id: string }>();
  const id = params.id ? Number(params.id) : NaN;
  const navigate = useNavigate();
  const [ep, setEp] = useState<ProxyEndpoint | null>(null);
  const [loading, setLoading] = useState(true);
  const [tab, setTab] = useState("info");
  const [confirmDelete, setConfirmDelete] = useState(false);

  async function refresh() {
    if (!Number.isFinite(id)) return;
    setLoading(true);
    try {
      setEp(await api.getEndpoint(id));
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        toast.error("Endpoint não encontrado");
        navigate("/endpoints", { replace: true });
        return;
      }
      toast.error(errMessage(err, "Falha ao carregar endpoint"));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  if (!Number.isFinite(id)) {
    return (
      <Alert variant="destructive">
        <AlertTitle>ID inválido</AlertTitle>
        <AlertDescription>
          <Link to="/endpoints" className="underline">
            Voltar para a lista
          </Link>
        </AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-1">
          <Link
            to="/endpoints"
            className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
          >
            <ArrowLeft className="h-3 w-3" />
            Endpoints
          </Link>
          {loading ? (
            <Skeleton className="h-7 w-48" />
          ) : (
            <div className="flex items-center gap-3">
              <h2 className="text-2xl font-semibold tracking-tight">{ep?.name}</h2>
              {ep && (
                <>
                  <ProviderBadge kind={ep.provider_kind ?? "custom"} />
                  <Badge variant="outline" className="font-mono text-[10px]">
                    {ep.lb_strategy}
                  </Badge>
                  {ep.active ? (
                    <Badge variant="success">Ativo</Badge>
                  ) : (
                    <Badge variant="muted">Inativo</Badge>
                  )}
                </>
              )}
            </div>
          )}
          {ep && (
            <p className="font-mono text-xs text-muted-foreground">
              /v1/proxy/{ep.slug}
            </p>
          )}
        </div>

        {ep && (
          <Button variant="destructive" onClick={() => setConfirmDelete(true)} disabled={!ep.active}>
            <Trash2 className="h-4 w-4" />
            Desativar
          </Button>
        )}
      </div>

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value="info">Detalhes</TabsTrigger>
          <TabsTrigger value="targets">
            Targets {ep && <span className="ml-1 text-xs text-muted-foreground">· {ep.targets.length}</span>}
          </TabsTrigger>
          <TabsTrigger value="grants">Acessos</TabsTrigger>
        </TabsList>

        <TabsContent value="info">
          {loading || !ep ? (
            <Card>
              <CardContent className="space-y-3 p-6">
                <Skeleton className="h-8 w-full" />
                <Skeleton className="h-8 w-full" />
              </CardContent>
            </Card>
          ) : (
            <InfoCard ep={ep} />
          )}
        </TabsContent>

        <TabsContent value="targets">
          {ep && <TargetsPanel ep={ep} onChanged={() => void refresh()} />}
        </TabsContent>

        <TabsContent value="grants">
          {ep && <GrantsPanel endpointId={ep.id} />}
        </TabsContent>
      </Tabs>

      <Dialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Desativar endpoint</DialogTitle>
            <DialogDescription>
              O slug <code className="font-mono">{ep?.slug}</code> deixa de aceitar chamadas
              imediatamente.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmDelete(false)}>
              Cancelar
            </Button>
            <Button
              variant="destructive"
              onClick={async () => {
                try {
                  await api.deleteEndpoint(id);
                  toast.success("Endpoint desativado");
                  navigate("/endpoints", { replace: true });
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

// ── Info tab ─────────────────────────────────────────────────────────────────

function InfoCard({ ep }: { ep: ProxyEndpoint }) {
  const meta = providerMeta(ep.provider_kind);
  return (
    <Card>
      <CardContent className="grid grid-cols-1 gap-x-6 gap-y-4 p-6 md:grid-cols-2">
        <Field label="ID" value={String(ep.id)} />
        <Field label="Slug" value={ep.slug} mono />
        <div>
          <Label className="text-xs uppercase tracking-wide text-muted-foreground">
            Provider
          </Label>
          <div className="mt-1 flex items-center gap-2">
            <ProviderBadge kind={ep.provider_kind ?? "custom"} />
            {meta.docs && (
              <a
                href={meta.docs}
                target="_blank"
                rel="noreferrer"
                className="text-[11px] text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
              >
                docs ↗
              </a>
            )}
          </div>
        </div>
        <Field label="Estratégia LB" value={ep.lb_strategy} mono />
        <Field
          label="Max RPS"
          value={ep.max_rps > 0 ? formatNumber(ep.max_rps) : "∞"}
        />
        <Field
          label="Limite mensal"
          value={ep.max_monthly_requests > 0 ? formatNumber(ep.max_monthly_requests) : "∞"}
        />
        <Field label="Targets ativos" value={String(ep.targets.length)} />
        <Field label="Criado em" value={formatDateTime(ep.created_at)} mono />
        <Field label="Atualizado em" value={formatDateTime(ep.updated_at)} mono />
        <div className="md:col-span-2">
          <Button asChild variant="outline">
            <Link to="/endpoints" state={{ editId: ep.id }}>
              <Pencil className="h-4 w-4" />
              Editar configurações
            </Link>
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

function Field({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <Label className="text-xs uppercase tracking-wide text-muted-foreground">{label}</Label>
      <p className={`mt-1 text-sm ${mono ? "font-mono" : ""}`}>{value}</p>
    </div>
  );
}

// ── Targets tab ──────────────────────────────────────────────────────────────

interface TargetForm {
  url: string;
  weight: number;
  active: boolean;
  auth: TargetAuthInput;
}

const EMPTY_TARGET: TargetForm = {
  url: "",
  weight: 1,
  active: true,
  auth: { type: "none" },
};

/**
 * Pré-preenche o form de target com a URL base e o tipo de auth sugeridos pelo
 * provider do endpoint. Reduz fricção: criar target Azure só pede a chave, não
 * exige decorar URL pattern + header. Para `custom`, mantém os defaults vazios.
 */
function targetFormForProvider(endpointKind: string): TargetForm {
  const meta = providerMeta(endpointKind);
  return {
    url: meta.baseURL,
    weight: 1,
    active: true,
    auth: {
      type: meta.authType,
      header: meta.authHeader,
    },
  };
}

function TargetsPanel({
  ep,
  onChanged,
}: {
  ep: ProxyEndpoint;
  onChanged: () => void;
}) {
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<Target | null>(null);
  const [removing, setRemoving] = useState<Target | null>(null);

  return (
    <Card>
      <CardContent className="space-y-4 p-4">
        <div className="flex items-center justify-between">
          <p className="text-sm text-muted-foreground">
            {ep.targets.length === 0
              ? "Sem targets cadastrados — o endpoint não consegue rotear chamadas."
              : `${ep.targets.length} target(s) ativo(s).`}
          </p>
          <Button onClick={() => setAdding(true)}>
            <Plus className="h-4 w-4" />
            Adicionar target
          </Button>
        </div>

        {ep.targets.length === 0 ? (
          <div className="rounded-md border border-dashed border-border px-4 py-10 text-center text-sm text-muted-foreground">
            Nenhum target ainda.
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>URL</TableHead>
                <TableHead>Peso</TableHead>
                <TableHead>Auth</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="w-[60px]" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {ep.targets.map((t) => (
                <TableRow key={t.id}>
                  <TableCell className="font-mono text-xs">{t.url}</TableCell>
                  <TableCell className="font-mono text-xs">{t.weight}</TableCell>
                  <TableCell>
                    <Badge variant="outline" className="font-mono text-[10px]">
                      {t.auth_type}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    {t.active ? (
                      <Badge variant="success">Ativo</Badge>
                    ) : (
                      <Badge variant="muted">Inativo</Badge>
                    )}
                  </TableCell>
                  <TableCell>
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="ghost" size="icon" className="h-8 w-8">
                          <MoreHorizontal className="h-4 w-4" />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem onSelect={() => setEditing(t)}>
                          <Pencil className="h-4 w-4" />
                          Editar
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem
                          variant="destructive"
                          onSelect={() => setRemoving(t)}
                        >
                          <Trash2 className="h-4 w-4" />
                          Remover
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>

      <TargetFormDialog
        open={adding || editing !== null}
        endpointId={ep.id}
        endpointProvider={ep.provider_kind ?? "custom"}
        existing={editing}
        onClose={() => {
          setAdding(false);
          setEditing(null);
        }}
        onSaved={() => {
          setAdding(false);
          setEditing(null);
          onChanged();
        }}
      />

      <Dialog
        open={removing !== null}
        onOpenChange={(o) => {
          if (!o) setRemoving(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Remover target</DialogTitle>
            <DialogDescription>
              O target <code className="font-mono">{removing?.url}</code> deixa de ser elegível
              imediatamente.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRemoving(null)}>
              Cancelar
            </Button>
            <Button
              variant="destructive"
              onClick={async () => {
                if (!removing) return;
                try {
                  await api.removeTarget(ep.id, removing.id);
                  toast.success("Target removido");
                  setRemoving(null);
                  onChanged();
                } catch (err) {
                  toast.error(errMessage(err, "Falha ao remover"));
                }
              }}
            >
              Remover
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}

function TargetFormDialog({
  open,
  endpointId,
  endpointProvider,
  existing,
  onClose,
  onSaved,
}: {
  open: boolean;
  endpointId: number;
  endpointProvider: string;
  existing: Target | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [form, setForm] = useState<TargetForm>(EMPTY_TARGET);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (existing) {
      setForm({
        url: existing.url,
        weight: existing.weight,
        active: existing.active,
        auth: { type: existing.auth_type },
      });
    } else {
      // Novo target → pré-preenche com base no provider do endpoint.
      setForm(targetFormForProvider(endpointProvider));
    }
  }, [existing, open, endpointProvider]);

  // Hint visual mostrado abaixo do form, vindo do catálogo de providers.
  const providerHint = providerMeta(endpointProvider).authHint;

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    try {
      if (existing) {
        await api.updateTarget(endpointId, existing.id, {
          url: form.url,
          weight: form.weight,
          active: form.active,
          auth: form.auth,
        });
        toast.success("Target atualizado");
      } else {
        await api.addTarget(endpointId, {
          url: form.url,
          weight: form.weight,
          auth: form.auth,
        });
        toast.success("Target adicionado");
      }
      onSaved();
    } catch (err) {
      toast.error(errMessage(err, "Falha ao salvar"));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>{existing ? "Editar target" : "Novo target"}</DialogTitle>
          <DialogDescription>
            Credenciais são cifradas em repouso com AES-256-GCM (ADR-0012).
            {!existing && endpointProvider !== "custom" && (
              <>
                {" "}URL e tipo de auth foram pré-preenchidos a partir do provider{" "}
                <strong>{providerMeta(endpointProvider).label}</strong>.
              </>
            )}
          </DialogDescription>
        </DialogHeader>
        <form className="space-y-4" onSubmit={onSubmit}>
          <div className="space-y-2">
            <Label>URL upstream</Label>
            <Input
              value={form.url}
              onChange={(e) => setForm({ ...form, url: e.target.value })}
              placeholder="https://upstream.example.com"
              required
              disabled={submitting}
            />
            {!existing && providerHint && (
              <p className="text-[11px] text-muted-foreground">{providerHint}</p>
            )}
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label>Peso</Label>
              <Input
                type="number"
                min={1}
                value={form.weight}
                onChange={(e) => setForm({ ...form, weight: Number(e.target.value) })}
                disabled={submitting}
              />
            </div>
            <div className="space-y-2">
              <Label>Auth</Label>
              <Select
                value={form.auth.type}
                onValueChange={(v) => setForm({ ...form, auth: { type: v as AuthType } })}
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
          </div>

          {form.auth.type === "bearer_token" && (
            <div className="space-y-2">
              <Label>Token</Label>
              <Input
                value={form.auth.token ?? ""}
                onChange={(e) =>
                  setForm({ ...form, auth: { ...form.auth, token: e.target.value } })
                }
                disabled={submitting}
                required
              />
            </div>
          )}

          {form.auth.type === "api_key_header" && (
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>Header</Label>
                <Input
                  value={form.auth.header ?? ""}
                  onChange={(e) =>
                    setForm({ ...form, auth: { ...form.auth, header: e.target.value } })
                  }
                  placeholder="Ocp-Apim-Subscription-Key"
                  disabled={submitting}
                  required
                />
              </div>
              <div className="space-y-2">
                <Label>Valor</Label>
                <Input
                  value={form.auth.value ?? ""}
                  onChange={(e) =>
                    setForm({ ...form, auth: { ...form.auth, value: e.target.value } })
                  }
                  disabled={submitting}
                  required
                />
              </div>
            </div>
          )}

          {form.auth.type === "basic_auth" && (
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>Usuário</Label>
                <Input
                  value={form.auth.username ?? ""}
                  onChange={(e) =>
                    setForm({ ...form, auth: { ...form.auth, username: e.target.value } })
                  }
                  disabled={submitting}
                  required
                />
              </div>
              <div className="space-y-2">
                <Label>Senha</Label>
                <Input
                  type="password"
                  value={form.auth.password ?? ""}
                  onChange={(e) =>
                    setForm({ ...form, auth: { ...form.auth, password: e.target.value } })
                  }
                  disabled={submitting}
                  required
                />
              </div>
            </div>
          )}

          {existing && (
            <div className="flex items-center justify-between rounded-md border border-input bg-background/40 px-3 py-2">
              <p className="text-sm font-medium">Target ativo</p>
              <Switch
                checked={form.active}
                onCheckedChange={(c) => setForm({ ...form, active: c })}
                disabled={submitting}
              />
            </div>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose} disabled={submitting}>
              Cancelar
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
              {existing ? "Salvar" : "Adicionar"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ── Grants tab ───────────────────────────────────────────────────────────────

function GrantsPanel({ endpointId }: { endpointId: number }) {
  const [apps, setApps] = useState<Application[]>([]);
  const [granted, setGranted] = useState<Set<number>>(new Set());
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState("");

  async function refresh() {
    setLoading(true);
    try {
      const allApps = await api.listApplications();
      setApps(allApps);
      // Inverter: pra cada app, ver se tem grant pra esse endpoint.
      // Não temos endpoint admin que diz "quem tem grant pra este endpoint",
      // mas temos ListGrantedApplicationIDs no backend — exposto como
      // chamada paralela aqui via listGrants(app.id) seria O(N).
      // Workaround leve: usamos /endpoints/{id} que já carrega o endpoint;
      // grants vivem em outra tabela. Solução simples: consultar por aplicação
      // (lista de grants pra ela inclui este endpoint?).
      const grantsByApp = await Promise.all(
        allApps.map((a) => api.listGrants(a.id).then((eps) => ({ appId: a.id, eps }))),
      );
      const next = new Set<number>();
      for (const g of grantsByApp) {
        if (g.eps.some((ep) => ep.id === endpointId)) {
          next.add(g.appId);
        }
      }
      setGranted(next);
    } catch (e) {
      toast.error(errMessage(e, "Falha ao carregar grants"));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [endpointId]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return apps;
    return apps.filter((a) => a.name.toLowerCase().includes(q));
  }, [apps, search]);

  async function toggle(appId: number, current: boolean) {
    try {
      if (current) {
        await api.revokeAccess(appId, endpointId);
        setGranted((s) => {
          const next = new Set(s);
          next.delete(appId);
          return next;
        });
        toast.success("Acesso revogado");
      } else {
        await api.grantAccess(appId, endpointId);
        setGranted((s) => new Set(s).add(appId));
        toast.success("Acesso concedido");
      }
    } catch (e) {
      toast.error(errMessage(e, "Falha ao alterar acesso"));
    }
  }

  return (
    <Card>
      <CardContent className="space-y-4 p-4">
        <Input
          placeholder="Filtrar aplicações…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="max-w-sm"
        />
        {loading ? (
          <div className="space-y-2">
            <Skeleton className="h-10 w-full" />
            <Skeleton className="h-10 w-full" />
          </div>
        ) : filtered.length === 0 ? (
          <div className="rounded-md border border-dashed border-border px-4 py-10 text-center text-sm text-muted-foreground">
            Nenhuma aplicação corresponde ao filtro.
          </div>
        ) : (
          <ul className="divide-y divide-border/60">
            {filtered.map((a) => {
              const isGranted = granted.has(a.id);
              return (
                <li key={a.id} className="flex items-center justify-between py-3">
                  <div>
                    <p className="text-sm font-medium">{a.name}</p>
                    <p className="text-[11px] text-muted-foreground">
                      {a.tier} · {a.active ? "ativa" : "inativa"}
                    </p>
                  </div>
                  <Switch
                    checked={isGranted}
                    onCheckedChange={() => toggle(a.id, isGranted)}
                    disabled={!a.active}
                  />
                </li>
              );
            })}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
