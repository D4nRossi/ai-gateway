import { useEffect, useState, type FormEvent } from "react";
import {
  ChevronRight,
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
import {
  api,
  type AuthType,
  type LBStrategy,
  type ProxyEndpoint,
  type Target,
  type TargetAuthInput,
} from "@/lib/api";
import { formatDateTime, formatNumber } from "@/lib/utils";

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
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<ProxyEndpoint | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<ProxyEndpoint | null>(null);
  const [managingTargets, setManagingTargets] = useState<ProxyEndpoint | null>(null);

  async function refresh() {
    setLoading(true);
    try {
      // List returns endpoints without targets; targets are loaded per-endpoint on demand.
      const list = await api.listEndpoints();
      setEndpoints(list);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Falha ao carregar endpoints");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  return (
    <div className="space-y-6">
      <div className="flex items-end justify-between">
        <p className="text-sm text-muted-foreground">
          Endpoints HTTP genéricos proxied pelo gateway, com targets e estratégia de
          balanceamento.
        </p>
        <Button onClick={() => setCreating(true)}>
          <Plus className="h-4 w-4" />
          Novo endpoint
        </Button>
      </div>

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
                  <TableHead>Estratégia</TableHead>
                  <TableHead>Max RPS</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Atualizado em</TableHead>
                  <TableHead className="w-[160px]" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {endpoints.map((ep) => (
                  <TableRow key={ep.id}>
                    <TableCell className="font-mono text-xs">{ep.slug}</TableCell>
                    <TableCell className="font-medium">{ep.name}</TableCell>
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
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-8"
                          onClick={() => setManagingTargets(ep)}
                        >
                          Targets
                          <ChevronRight className="h-3 w-3" />
                        </Button>
                        <DropdownMenu>
                          <DropdownMenuTrigger asChild>
                            <Button variant="ghost" size="icon" className="h-8 w-8">
                              <MoreHorizontal className="h-4 w-4" />
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
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
                  toast.error(err instanceof Error ? err.message : "Falha ao desativar");
                }
              }}
            >
              Desativar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <TargetsDialog
        endpoint={managingTargets}
        onClose={() => setManagingTargets(null)}
        onUpdated={() => void refresh()}
      />
    </div>
  );
}

interface EndpointForm {
  slug: string;
  name: string;
  lb_strategy: LBStrategy;
  max_rps: number;
  max_monthly_requests: number;
  active: boolean;
}

const EMPTY_EP_FORM: EndpointForm = {
  slug: "",
  name: "",
  lb_strategy: "round_robin",
  max_rps: 0,
  max_monthly_requests: 0,
  active: true,
};

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
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (existing) {
      setForm({
        slug: existing.slug,
        name: existing.name,
        lb_strategy: existing.lb_strategy,
        max_rps: existing.max_rps,
        max_monthly_requests: existing.max_monthly_requests,
        active: existing.active,
      });
    } else {
      setForm(EMPTY_EP_FORM);
    }
  }, [existing, open]);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    try {
      if (existing) {
        await api.updateEndpoint(existing.id, form);
        toast.success("Endpoint atualizado");
      } else {
        await api.createEndpoint(form);
        toast.success("Endpoint criado");
      }
      onSaved();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Falha ao salvar");
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
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>{existing ? "Editar endpoint" : "Novo endpoint"}</DialogTitle>
          <DialogDescription>
            Endpoints são acessíveis em <code className="font-mono">/v1/proxy/{"{slug}"}</code>.
          </DialogDescription>
        </DialogHeader>

        <form className="grid grid-cols-2 gap-4" onSubmit={onSubmit}>
          <div className="space-y-2">
            <Label htmlFor="slug">Slug</Label>
            <Input
              id="slug"
              value={form.slug}
              onChange={(e) => setForm({ ...form, slug: e.target.value })}
              placeholder="speech-to-text"
              disabled={!!existing || submitting}
              required
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="name">Nome</Label>
            <Input
              id="name"
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              placeholder="Azure Speech-to-Text"
              disabled={submitting}
              required
            />
          </div>

          <div className="col-span-2 space-y-2">
            <Label>Estratégia</Label>
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
          </div>

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

          {existing && (
            <>
              <Separator className="col-span-2" />
              <div className="col-span-2 flex items-center justify-between rounded-md border border-input bg-background/40 px-3 py-2">
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

          <DialogFooter className="col-span-2 mt-2">
            <Button type="button" variant="outline" onClick={onClose} disabled={submitting}>
              Cancelar
            </Button>
            <Button type="submit" disabled={submitting}>
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

interface TargetForm {
  url: string;
  weight: number;
  active: boolean;
  auth: TargetAuthInput;
}

const EMPTY_TARGET_FORM: TargetForm = {
  url: "",
  weight: 1,
  active: true,
  auth: { type: "none" },
};

function TargetsDialog({
  endpoint,
  onClose,
  onUpdated,
}: {
  endpoint: ProxyEndpoint | null;
  onClose: () => void;
  onUpdated: () => void;
}) {
  const [detail, setDetail] = useState<ProxyEndpoint | null>(null);
  const [loading, setLoading] = useState(false);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<Target | null>(null);
  const [form, setForm] = useState<TargetForm>(EMPTY_TARGET_FORM);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!endpoint) {
      setDetail(null);
      return;
    }
    setLoading(true);
    api
      .getEndpoint(endpoint.id)
      .then(setDetail)
      .catch((err) => toast.error(err instanceof Error ? err.message : "Falha ao carregar targets"))
      .finally(() => setLoading(false));
  }, [endpoint]);

  useEffect(() => {
    if (editing) {
      setForm({
        url: editing.url,
        weight: editing.weight,
        active: editing.active,
        auth: { type: editing.auth_type },
      });
    } else {
      setForm(EMPTY_TARGET_FORM);
    }
  }, [editing]);

  async function refresh() {
    if (!endpoint) return;
    try {
      setDetail(await api.getEndpoint(endpoint.id));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Falha ao atualizar");
    }
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!endpoint) return;
    setSubmitting(true);
    try {
      if (editing) {
        await api.updateTarget(endpoint.id, editing.id, {
          url: form.url,
          weight: form.weight,
          active: form.active,
          auth: form.auth,
        });
        toast.success("Target atualizado");
      } else {
        await api.addTarget(endpoint.id, {
          url: form.url,
          weight: form.weight,
          auth: form.auth,
        });
        toast.success("Target adicionado");
      }
      setAdding(false);
      setEditing(null);
      await refresh();
      onUpdated();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Falha ao salvar");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Dialog
      open={endpoint !== null}
      onOpenChange={(o) => {
        if (!o) {
          setAdding(false);
          setEditing(null);
          onClose();
        }
      }}
    >
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle>Targets de {endpoint?.name}</DialogTitle>
          <DialogDescription>
            Upstreams reais para onde o proxy roteia as chamadas. Credenciais são cifradas em
            repouso com AES-256-GCM.
          </DialogDescription>
        </DialogHeader>

        {adding || editing ? (
          <form className="space-y-4" onSubmit={onSubmit}>
            <div className="space-y-2">
              <Label>URL upstream</Label>
              <Input
                value={form.url}
                onChange={(e) => setForm({ ...form, url: e.target.value })}
                placeholder="https://speech.eastus.azure.com"
                disabled={submitting}
                required
              />
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
                <Label>Tipo de autenticação</Label>
                <Select
                  value={form.auth.type}
                  onValueChange={(v) =>
                    setForm({ ...form, auth: { type: v as AuthType } })
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

            {editing && (
              <div className="flex items-center justify-between rounded-md border border-input bg-background/40 px-3 py-2">
                <div>
                  <p className="text-sm font-medium">Target ativo</p>
                </div>
                <Switch
                  checked={form.active}
                  onCheckedChange={(c) => setForm({ ...form, active: c })}
                  disabled={submitting}
                />
              </div>
            )}

            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  setAdding(false);
                  setEditing(null);
                }}
                disabled={submitting}
              >
                Cancelar
              </Button>
              <Button type="submit" disabled={submitting}>
                {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
                {editing ? "Salvar" : "Adicionar"}
              </Button>
            </DialogFooter>
          </form>
        ) : (
          <>
            <div className="rounded-md border border-border">
              {loading ? (
                <div className="space-y-2 p-4">
                  <Skeleton className="h-8 w-full" />
                  <Skeleton className="h-8 w-full" />
                </div>
              ) : !detail || detail.targets.length === 0 ? (
                <div className="px-4 py-10 text-center text-sm text-muted-foreground">
                  Nenhum target ainda. Adicione o primeiro.
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
                    {detail.targets.map((t) => (
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
                                onSelect={async () => {
                                  if (!endpoint) return;
                                  try {
                                    await api.removeTarget(endpoint.id, t.id);
                                    toast.success("Target removido");
                                    await refresh();
                                    onUpdated();
                                  } catch (err) {
                                    toast.error(
                                      err instanceof Error ? err.message : "Falha ao remover",
                                    );
                                  }
                                }}
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
            </div>

            <DialogFooter>
              <Button variant="outline" onClick={onClose}>
                Fechar
              </Button>
              <Button onClick={() => setAdding(true)}>
                <Plus className="h-4 w-4" />
                Adicionar target
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
