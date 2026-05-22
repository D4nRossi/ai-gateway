import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  ArrowLeft,
  CheckCircle2,
  Circle,
  KeyRound,
  Pencil,
  ShieldAlert,
  Trash2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
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
import { Switch } from "@/components/ui/switch";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { toast } from "@/components/ui/sonner";
import {
  api,
  ApiError,
  errMessage,
  type Application,
  type AuditEvent,
  type ProxyEndpoint,
  type UsageEvent,
} from "@/lib/api";
import { formatBRL, formatDateTime, formatNumber } from "@/lib/utils";

export default function ApplicationDetail() {
  const params = useParams<{ id: string }>();
  const id = params.id ? Number(params.id) : NaN;
  const navigate = useNavigate();
  const [app, setApp] = useState<Application | null>(null);
  const [loading, setLoading] = useState(true);
  const [tab, setTab] = useState("info");
  const [tokenReveal, setTokenReveal] = useState<string | null>(null);
  const [confirmRotate, setConfirmRotate] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  async function refresh() {
    if (!Number.isFinite(id)) return;
    setLoading(true);
    try {
      setApp(await api.getApplication(id));
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        toast.error("Aplicação não encontrada");
        navigate("/applications", { replace: true });
        return;
      }
      toast.error(errMessage(err, "Falha ao carregar aplicação"));
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
          <Link to="/applications" className="underline">
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
            to="/applications"
            className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
          >
            <ArrowLeft className="h-3 w-3" />
            Aplicações
          </Link>
          {loading ? (
            <Skeleton className="h-7 w-48" />
          ) : (
            <div className="flex items-center gap-3">
              <h2 className="text-2xl font-semibold tracking-tight">{app?.name}</h2>
              {app && (
                <>
                  <Badge variant="outline" className="font-mono text-[10px]">
                    {app.tier}
                  </Badge>
                  {app.active ? (
                    <Badge variant="success">Ativa</Badge>
                  ) : (
                    <Badge variant="muted">Inativa</Badge>
                  )}
                </>
              )}
            </div>
          )}
        </div>

        {app && (
          <div className="flex flex-wrap items-center gap-2">
            <Button variant="outline" onClick={() => setConfirmRotate(true)}>
              <KeyRound className="h-4 w-4" />
              Rotacionar chave
            </Button>
            <Button variant="destructive" onClick={() => setConfirmDelete(true)} disabled={!app.active}>
              <Trash2 className="h-4 w-4" />
              Desativar
            </Button>
          </div>
        )}
      </div>

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value="info">Detalhes</TabsTrigger>
          <TabsTrigger value="usage">Uso recente</TabsTrigger>
          <TabsTrigger value="audit">Auditoria</TabsTrigger>
          <TabsTrigger value="grants">Acessos</TabsTrigger>
        </TabsList>

        <TabsContent value="info">
          {loading || !app ? (
            <Card>
              <CardContent className="space-y-3 p-6">
                <Skeleton className="h-8 w-full" />
                <Skeleton className="h-8 w-full" />
                <Skeleton className="h-8 w-full" />
              </CardContent>
            </Card>
          ) : (
            <InfoCard app={app} />
          )}
        </TabsContent>

        <TabsContent value="usage">
          {app && <UsagePanel appName={app.name} />}
        </TabsContent>
        <TabsContent value="audit">
          {app && <AuditPanel appName={app.name} />}
        </TabsContent>
        <TabsContent value="grants">
          {app && <GrantsPanel appId={app.id} />}
        </TabsContent>
      </Tabs>

      <Dialog open={confirmRotate} onOpenChange={setConfirmRotate}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Rotacionar chave</DialogTitle>
            <DialogDescription>
              A chave anterior deixa de funcionar imediatamente. O novo token aparece uma única vez.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmRotate(false)}>
              Cancelar
            </Button>
            <Button
              onClick={async () => {
                try {
                  const res = await api.rotateKey(id);
                  setTokenReveal(res.token);
                  setConfirmRotate(false);
                  toast.success("Chave rotacionada");
                } catch (err) {
                  toast.error(errMessage(err, "Falha ao rotacionar"));
                }
              }}
            >
              <KeyRound className="h-4 w-4" />
              Gerar nova chave
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Desativar aplicação</DialogTitle>
            <DialogDescription>
              Tokens existentes deixam de funcionar imediatamente.
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
                  await api.deleteApplication(id);
                  toast.success("Aplicação desativada");
                  navigate("/applications", { replace: true });
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

      <TokenRevealDialog token={tokenReveal} onClose={() => setTokenReveal(null)} />
    </div>
  );
}

// ── Tabs ──────────────────────────────────────────────────────────────────────

function InfoCard({ app }: { app: Application }) {
  return (
    <Card>
      <CardContent className="grid grid-cols-1 gap-x-6 gap-y-4 p-6 md:grid-cols-2">
        <Field label="ID" value={String(app.id)} />
        <Field label="Tier" value={app.tier} mono />
        <Field
          label="Streaming"
          value={app.streaming_allowed ? "Permitido" : "Bloqueado"}
        />
        <Field label="Max RPM" value={formatNumber(app.max_rpm)} />
        <Field label="Max TPM" value={formatNumber(app.max_tpm)} />
        <Field
          label="Budget mensal"
          value={formatBRL(app.monthly_budget_brl)}
        />
        <div className="md:col-span-2">
          <Label className="text-xs uppercase tracking-wide text-muted-foreground">
            Modelos permitidos
          </Label>
          <div className="mt-1 flex flex-wrap gap-1.5">
            {app.allowed_models.length === 0 ? (
              <span className="text-sm text-muted-foreground">—</span>
            ) : (
              app.allowed_models.map((m) => (
                <Badge key={m} variant="outline" className="font-mono text-[10px]">
                  {m}
                </Badge>
              ))
            )}
          </div>
        </div>
        <Field label="Criada em" value={formatDateTime(app.created_at)} mono />
        <Field label="Atualizada em" value={formatDateTime(app.updated_at)} mono />
        <div className="md:col-span-2">
          <Button asChild variant="outline">
            <Link to="/applications" state={{ editId: app.id }}>
              <Pencil className="h-4 w-4" />
              Editar configurações
            </Link>
          </Button>
          <span className="ml-3 text-xs text-muted-foreground">
            (edição inline será adicionada no Lote D)
          </span>
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

// ── Usage tab ─────────────────────────────────────────────────────────────────

function UsagePanel({ appName }: { appName: string }) {
  const [rows, setRows] = useState<UsageEvent[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    api
      .listUsage({ application: appName, limit: 100 })
      .then(setRows)
      .catch((e) =>
        toast.error(errMessage(e, "Falha ao carregar uso")),
      )
      .finally(() => setLoading(false));
  }, [appName]);

  return (
    <Card>
      <CardContent className="p-0">
        {loading ? (
          <div className="space-y-2 p-6">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-full" />
          </div>
        ) : rows.length === 0 ? (
          <Empty msg="Nenhuma requisição registrada nas últimas 24h." />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Quando</TableHead>
                <TableHead>Modelo</TableHead>
                <TableHead>Tokens</TableHead>
                <TableHead>Latência</TableHead>
                <TableHead>Custo</TableHead>
                <TableHead>Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((u) => (
                <TableRow key={u.id}>
                  <TableCell className="text-xs text-muted-foreground">
                    {formatDateTime(u.created_at)}
                  </TableCell>
                  <TableCell className="font-mono text-xs">{u.model}</TableCell>
                  <TableCell className="font-mono text-xs">
                    {u.total_tokens != null ? formatNumber(u.total_tokens) : "—"}
                  </TableCell>
                  <TableCell className="font-mono text-xs">{u.latency_ms} ms</TableCell>
                  <TableCell className="font-mono text-xs">
                    {u.estimated_cost_brl != null ? formatBRL(u.estimated_cost_brl) : "—"}
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={u.status_code >= 500 ? "destructive" : u.status_code >= 400 ? "warning" : "success"}
                      className="font-mono text-[10px]"
                    >
                      {u.status_code}
                    </Badge>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}

// ── Audit tab ─────────────────────────────────────────────────────────────────

function AuditPanel({ appName }: { appName: string }) {
  const [rows, setRows] = useState<AuditEvent[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    api
      .listAudit({ application: appName, limit: 100 })
      .then(setRows)
      .catch((e) =>
        toast.error(errMessage(e, "Falha ao carregar auditoria")),
      )
      .finally(() => setLoading(false));
  }, [appName]);

  return (
    <Card>
      <CardContent className="p-0">
        {loading ? (
          <div className="space-y-2 p-6">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-full" />
          </div>
        ) : rows.length === 0 ? (
          <Empty msg="Nenhum evento de auditoria nas últimas 24h." />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Quando</TableHead>
                <TableHead>Evento</TableHead>
                <TableHead>Severidade</TableHead>
                <TableHead>Metadata</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((a) => (
                <TableRow key={a.id}>
                  <TableCell className="text-xs text-muted-foreground">
                    {formatDateTime(a.created_at)}
                  </TableCell>
                  <TableCell className="font-mono text-xs">{a.event_type}</TableCell>
                  <TableCell>
                    <Badge
                      variant={a.severity === "error" ? "destructive" : a.severity === "warn" ? "warning" : "outline"}
                      className="font-mono text-[10px]"
                    >
                      {a.severity}
                    </Badge>
                  </TableCell>
                  <TableCell className="font-mono text-[11px] text-muted-foreground">
                    {a.metadata ?? "—"}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}

// ── Grants tab ────────────────────────────────────────────────────────────────

function GrantsPanel({ appId }: { appId: number }) {
  const [allEndpoints, setAllEndpoints] = useState<ProxyEndpoint[]>([]);
  const [granted, setGranted] = useState<Set<number>>(new Set());
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState("");
  const [pendingId, setPendingId] = useState<number | null>(null);

  async function refresh() {
    setLoading(true);
    try {
      const [eps, gs] = await Promise.all([
        api.listEndpoints(),
        api.listGrants(appId),
      ]);
      setAllEndpoints(eps);
      setGranted(new Set(gs.map((g) => g.id)));
    } catch (e) {
      toast.error(errMessage(e, "Falha ao carregar acessos"));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [appId]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return allEndpoints;
    return allEndpoints.filter(
      (e) => e.slug.toLowerCase().includes(q) || e.name.toLowerCase().includes(q),
    );
  }, [allEndpoints, search]);

  async function toggle(epId: number, currentlyGranted: boolean) {
    setPendingId(epId);
    try {
      if (currentlyGranted) {
        await api.revokeAccess(appId, epId);
        setGranted((s) => {
          const next = new Set(s);
          next.delete(epId);
          return next;
        });
        toast.success("Acesso revogado");
      } else {
        await api.grantAccess(appId, epId);
        setGranted((s) => new Set(s).add(epId));
        toast.success("Acesso concedido");
      }
    } catch (e) {
      toast.error(errMessage(e, "Falha ao alterar acesso"));
    } finally {
      setPendingId(null);
    }
  }

  return (
    <Card>
      <CardContent className="space-y-4 p-4">
        <Input
          placeholder="Filtrar endpoints…"
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
          <Empty msg="Nenhum endpoint corresponde ao filtro." />
        ) : (
          <ul className="divide-y divide-border/60">
            {filtered.map((ep) => {
              const isGranted = granted.has(ep.id);
              const pending = pendingId === ep.id;
              return (
                <li key={ep.id} className="flex items-center justify-between py-3">
                  <div className="flex items-center gap-3">
                    {isGranted ? (
                      <CheckCircle2 className="h-4 w-4 text-success" />
                    ) : (
                      <Circle className="h-4 w-4 text-muted-foreground/50" />
                    )}
                    <div>
                      <p className="text-sm font-medium">{ep.name}</p>
                      <p className="font-mono text-[11px] text-muted-foreground">{ep.slug}</p>
                    </div>
                  </div>
                  <Switch
                    checked={isGranted}
                    onCheckedChange={() => toggle(ep.id, isGranted)}
                    disabled={pending || !ep.active}
                    aria-label={`${isGranted ? "Revogar" : "Conceder"} acesso a ${ep.slug}`}
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

// ── Misc ──────────────────────────────────────────────────────────────────────

function Empty({ msg }: { msg: string }) {
  return (
    <div className="px-6 py-10 text-center text-sm text-muted-foreground">{msg}</div>
  );
}

function TokenRevealDialog({
  token,
  onClose,
}: {
  token: string | null;
  onClose: () => void;
}) {
  const [copied, setCopied] = useState(false);
  async function copy() {
    if (!token) return;
    try {
      await navigator.clipboard.writeText(token);
      setCopied(true);
      toast.success("Token copiado");
      setTimeout(() => setCopied(false), 2500);
    } catch {
      toast.error("Não foi possível copiar — selecione manualmente.");
    }
  }
  return (
    <Dialog open={token !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>Token gerado</DialogTitle>
          <DialogDescription>
            Esta é a única vez que o token completo aparece.
          </DialogDescription>
        </DialogHeader>
        <Alert variant="warning">
          <ShieldAlert className="h-4 w-4" />
          <AlertTitle>Mostrado uma única vez</AlertTitle>
          <AlertDescription>
            Guarde agora — após fechar não há como recuperar.
          </AlertDescription>
        </Alert>
        <div className="space-y-2">
          <Label>Token</Label>
          <Input
            readOnly
            value={token ?? ""}
            className="font-mono text-xs"
            onFocus={(e) => e.currentTarget.select()}
          />
          <Button type="button" variant="outline" size="sm" onClick={copy}>
            {copied ? "Copiado ✓" : "Copiar"}
          </Button>
        </div>
        <DialogFooter>
          <Button onClick={onClose}>Já guardei</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
