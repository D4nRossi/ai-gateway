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
import { api, type LBStrategy, type ProviderKind, type ProxyEndpoint } from "@/lib/api";
import { PROVIDERS } from "@/lib/providers";
import { filterByText } from "@/lib/filter";
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
      toast.error(err instanceof Error ? err.message : "Falha ao carregar endpoints");
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
                  toast.error(err instanceof Error ? err.message : "Falha ao desativar");
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
        provider_kind: existing.provider_kind ?? "custom",
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
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{existing ? "Editar endpoint" : "Novo endpoint"}</DialogTitle>
          <DialogDescription>
            Endpoints são acessíveis em <code className="font-mono">/v1/proxy/{"{slug}"}</code>.
            Escolha um provider para que o gateway pré-preencha defaults; use{" "}
            <strong>Personalizado</strong> para qualquer outra API HTTP.
          </DialogDescription>
        </DialogHeader>

        <form className="space-y-4" onSubmit={onSubmit}>
          <div className="space-y-2">
            <Label>Provider</Label>
            <ProviderSelector
              value={form.provider_kind}
              onChange={(kind) => setForm(formForProvider(kind, form))}
              disabled={submitting}
            />
            {form.provider_kind !== "custom" && PROVIDERS[form.provider_kind].docs && (
              <p className="text-[11px] text-muted-foreground">
                URL sugerida:{" "}
                <code className="font-mono">{PROVIDERS[form.provider_kind].baseURL}</code> ·{" "}
                <a
                  href={PROVIDERS[form.provider_kind].docs}
                  target="_blank"
                  rel="noreferrer"
                  className="underline hover:text-foreground"
                >
                  Documentação
                </a>
              </p>
            )}
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label htmlFor="slug">Slug</Label>
              <Input
                id="slug"
                value={form.slug}
                onChange={(e) => setForm({ ...form, slug: e.target.value })}
                placeholder="chat-default"
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
                placeholder="Provedor de chat principal"
                disabled={submitting}
                required
              />
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
          </div>

          <div className="grid grid-cols-2 gap-4">
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
