import { useEffect, useMemo, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import {
  Copy,
  Eye,
  KeyRound,
  Loader2,
  MoreHorizontal,
  Pencil,
  Plus,
  ShieldAlert,
  Trash2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
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
  DropdownMenuTrigger,
  DropdownMenuSeparator,
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
import { api, type Application, type Tier } from "@/lib/api";
import { filterByText } from "@/lib/filter";
import { formatBRL, formatDateTime, formatNumber } from "@/lib/utils";

interface FormState {
  name: string;
  tier: Tier;
  allowed_models: string;
  streaming_allowed: boolean;
  max_rpm: number;
  max_tpm: number;
  monthly_budget_brl: number;
  active: boolean;
}

const EMPTY_FORM: FormState = {
  name: "",
  tier: "tier_2",
  allowed_models: "",
  streaming_allowed: false,
  max_rpm: 60,
  max_tpm: 60_000,
  monthly_budget_brl: 100,
  active: true,
};

function toForm(app: Application): FormState {
  return {
    name: app.name,
    tier: app.tier,
    allowed_models: app.allowed_models.join(", "),
    streaming_allowed: app.streaming_allowed,
    max_rpm: app.max_rpm,
    max_tpm: app.max_tpm,
    monthly_budget_brl: app.monthly_budget_brl,
    active: app.active,
  };
}

export default function Applications() {
  const [apps, setApps] = useState<Application[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState("");
  const [editing, setEditing] = useState<Application | null>(null);
  const [creating, setCreating] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState<Application | null>(null);
  const [confirmRotate, setConfirmRotate] = useState<Application | null>(null);
  const [tokenReveal, setTokenReveal] = useState<{ token: string; appName: string } | null>(null);

  async function refresh() {
    setLoading(true);
    try {
      setApps(await api.listApplications());
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Falha ao carregar aplicações");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  const filtered = useMemo(
    () => filterByText(apps, search, (a) => [a.name, a.tier]),
    [apps, search],
  );

  return (
    <div className="space-y-6">
      <p className="text-sm text-muted-foreground">
        Aplicações registradas no gateway, com seus limites e tokens de acesso.
      </p>

      <DataTableToolbar
        search={search}
        onSearchChange={setSearch}
        onRefresh={refresh}
        refreshing={loading}
        placeholder="Buscar por nome ou tier…"
        rightSlot={
          <Button onClick={() => setCreating(true)}>
            <Plus className="h-4 w-4" />
            Nova aplicação
          </Button>
        }
      />

      <Card>
        <CardContent className="p-0">
          {loading ? (
            <div className="space-y-3 p-6">
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
            </div>
          ) : apps.length === 0 ? (
            <div className="flex flex-col items-center justify-center px-6 py-16 text-center">
              <p className="text-sm text-muted-foreground">
                Nenhuma aplicação cadastrada ainda.
              </p>
              <Button className="mt-4" onClick={() => setCreating(true)}>
                <Plus className="h-4 w-4" />
                Criar primeira aplicação
              </Button>
            </div>
          ) : filtered.length === 0 ? (
            <div className="px-6 py-10 text-center text-sm text-muted-foreground">
              Nenhuma aplicação corresponde ao filtro.
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Nome</TableHead>
                  <TableHead>Tier</TableHead>
                  <TableHead>RPM</TableHead>
                  <TableHead>TPM</TableHead>
                  <TableHead>Budget</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Criada em</TableHead>
                  <TableHead className="w-[60px]" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {filtered.map((app) => (
                  <TableRow key={app.id}>
                    <TableCell className="font-medium">
                      <Link
                        to={`/applications/${app.id}`}
                        className="hover:text-primary hover:underline"
                      >
                        {app.name}
                      </Link>
                    </TableCell>
                    <TableCell>
                      <Badge variant="outline" className="font-mono text-[10px]">
                        {app.tier}
                      </Badge>
                    </TableCell>
                    <TableCell className="font-mono text-xs">{formatNumber(app.max_rpm)}</TableCell>
                    <TableCell className="font-mono text-xs">{formatNumber(app.max_tpm)}</TableCell>
                    <TableCell className="font-mono text-xs">
                      {formatBRL(app.monthly_budget_brl)}
                    </TableCell>
                    <TableCell>
                      {app.active ? (
                        <Badge variant="success">Ativa</Badge>
                      ) : (
                        <Badge variant="muted">Inativa</Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {formatDateTime(app.created_at)}
                    </TableCell>
                    <TableCell>
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button variant="ghost" size="icon" className="h-8 w-8">
                            <MoreHorizontal className="h-4 w-4" />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem asChild>
                            <Link to={`/applications/${app.id}`}>
                              <Eye className="h-4 w-4" />
                              Ver detalhes
                            </Link>
                          </DropdownMenuItem>
                          <DropdownMenuItem onSelect={() => setEditing(app)}>
                            <Pencil className="h-4 w-4" />
                            Editar
                          </DropdownMenuItem>
                          <DropdownMenuItem onSelect={() => setConfirmRotate(app)}>
                            <KeyRound className="h-4 w-4" />
                            Rotacionar chave
                          </DropdownMenuItem>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem
                            variant="destructive"
                            onSelect={() => setConfirmDelete(app)}
                          >
                            <Trash2 className="h-4 w-4" />
                            Desativar
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
      </Card>

      {/* Create / Edit dialog */}
      <ApplicationFormDialog
        open={creating || editing !== null}
        existing={editing}
        onClose={() => {
          setCreating(false);
          setEditing(null);
        }}
        onCreated={(app, token) => {
          setCreating(false);
          setTokenReveal({ token, appName: app.name });
          void refresh();
        }}
        onUpdated={() => {
          setEditing(null);
          void refresh();
        }}
      />

      {/* Delete confirmation */}
      <Dialog
        open={confirmDelete !== null}
        onOpenChange={(open) => {
          if (!open) setConfirmDelete(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Desativar aplicação</DialogTitle>
            <DialogDescription>
              A aplicação {confirmDelete?.name && <strong>{confirmDelete.name}</strong>} ficará
              indisponível. Os tokens existentes deixam de funcionar imediatamente.
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
                  await api.deleteApplication(confirmDelete.id);
                  toast.success(`Aplicação ${confirmDelete.name} desativada`);
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

      {/* Rotate-key confirmation */}
      <Dialog
        open={confirmRotate !== null}
        onOpenChange={(open) => {
          if (!open) setConfirmRotate(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Rotacionar chave de API</DialogTitle>
            <DialogDescription>
              A chave anterior de <strong>{confirmRotate?.name}</strong> deixa de funcionar
              imediatamente. O novo token aparece uma única vez.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmRotate(null)}>
              Cancelar
            </Button>
            <Button
              onClick={async () => {
                if (!confirmRotate) return;
                try {
                  const res = await api.rotateKey(confirmRotate.id);
                  toast.success(`Chave de ${confirmRotate.name} rotacionada`);
                  setTokenReveal({ token: res.token, appName: confirmRotate.name });
                  setConfirmRotate(null);
                } catch (err) {
                  toast.error(err instanceof Error ? err.message : "Falha ao rotacionar");
                }
              }}
            >
              <KeyRound className="h-4 w-4" />
              Gerar nova chave
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Token reveal — shown once */}
      <TokenRevealDialog
        reveal={tokenReveal}
        onClose={() => setTokenReveal(null)}
      />
    </div>
  );
}

function ApplicationFormDialog({
  open,
  existing,
  onClose,
  onCreated,
  onUpdated,
}: {
  open: boolean;
  existing: Application | null;
  onClose: () => void;
  onCreated: (app: Application, token: string) => void;
  onUpdated: () => void;
}) {
  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (existing) {
      setForm(toForm(existing));
    } else {
      setForm(EMPTY_FORM);
    }
  }, [existing, open]);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    try {
      const payload = {
        name: form.name.trim(),
        tier: form.tier,
        allowed_models: form.allowed_models
          .split(",")
          .map((s) => s.trim())
          .filter(Boolean),
        streaming_allowed: form.streaming_allowed,
        max_rpm: form.max_rpm,
        max_tpm: form.max_tpm,
        monthly_budget_brl: form.monthly_budget_brl,
      };
      if (existing) {
        await api.updateApplication(existing.id, { ...payload, active: form.active });
        toast.success("Aplicação atualizada");
        onUpdated();
      } else {
        const created = await api.createApplication(payload);
        onCreated(created, created.token);
      }
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
          <DialogTitle>{existing ? "Editar aplicação" : "Nova aplicação"}</DialogTitle>
          <DialogDescription>
            {existing
              ? "Atualize os limites e o tier da aplicação."
              : "Defina os limites iniciais. O token será gerado e exibido uma única vez."}
          </DialogDescription>
        </DialogHeader>

        <form className="grid grid-cols-2 gap-4" onSubmit={onSubmit}>
          <div className="col-span-2 space-y-2">
            <Label htmlFor="name">Nome</Label>
            <Input
              id="name"
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              required
              disabled={!!existing || submitting}
              placeholder="ex: SuporteIA"
            />
          </div>

          <div className="space-y-2">
            <Label>Tier</Label>
            <Select
              value={form.tier}
              onValueChange={(v) => setForm({ ...form, tier: v as Tier })}
              disabled={submitting}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="tier_1">tier_1 (mínimo)</SelectItem>
                <SelectItem value="tier_2">tier_2 (padrão)</SelectItem>
                <SelectItem value="tier_3">tier_3 (máximo)</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="flex items-center justify-between rounded-md border border-input bg-background/40 px-3 py-2">
            <div>
              <p className="text-sm font-medium">Streaming</p>
              <p className="text-xs text-muted-foreground">Permitir SSE</p>
            </div>
            <Switch
              checked={form.streaming_allowed}
              onCheckedChange={(c) => setForm({ ...form, streaming_allowed: c })}
              disabled={submitting}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="max_rpm">Max RPM</Label>
            <Input
              id="max_rpm"
              type="number"
              min={0}
              value={form.max_rpm}
              onChange={(e) => setForm({ ...form, max_rpm: Number(e.target.value) })}
              disabled={submitting}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="max_tpm">Max TPM</Label>
            <Input
              id="max_tpm"
              type="number"
              min={0}
              value={form.max_tpm}
              onChange={(e) => setForm({ ...form, max_tpm: Number(e.target.value) })}
              disabled={submitting}
            />
          </div>

          <div className="col-span-2 space-y-2">
            <Label htmlFor="budget">Budget mensal (BRL)</Label>
            <Input
              id="budget"
              type="number"
              min={0}
              step={0.01}
              value={form.monthly_budget_brl}
              onChange={(e) => setForm({ ...form, monthly_budget_brl: Number(e.target.value) })}
              disabled={submitting}
            />
          </div>

          <div className="col-span-2 space-y-2">
            <Label htmlFor="allowed_models">Modelos permitidos (separados por vírgula)</Label>
            <Input
              id="allowed_models"
              value={form.allowed_models}
              onChange={(e) => setForm({ ...form, allowed_models: e.target.value })}
              disabled={submitting}
              placeholder="gpt-4o-mini, gpt-4o"
            />
          </div>

          {existing && (
            <>
              <Separator className="col-span-2" />
              <div className="col-span-2 flex items-center justify-between rounded-md border border-input bg-background/40 px-3 py-2">
                <div>
                  <p className="text-sm font-medium">Aplicação ativa</p>
                  <p className="text-xs text-muted-foreground">
                    Desativar bloqueia toda autenticação imediatamente.
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
              {existing ? "Salvar" : "Criar aplicação"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function TokenRevealDialog({
  reveal,
  onClose,
}: {
  reveal: { token: string; appName: string } | null;
  onClose: () => void;
}) {
  const [copied, setCopied] = useState(false);

  async function copy() {
    if (!reveal) return;
    try {
      await navigator.clipboard.writeText(reveal.token);
      setCopied(true);
      toast.success("Token copiado");
      setTimeout(() => setCopied(false), 2500);
    } catch {
      toast.error("Não foi possível copiar — selecione manualmente.");
    }
  }

  return (
    <Dialog
      open={reveal !== null}
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
    >
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>Token gerado para {reveal?.appName}</DialogTitle>
          <DialogDescription>
            Esta é a única vez que o token completo aparece. Guarde-o agora — não há como
            recuperá-lo depois.
          </DialogDescription>
        </DialogHeader>

        <Alert variant="warning">
          <ShieldAlert className="h-4 w-4" />
          <AlertTitle>Mostrado uma única vez</AlertTitle>
          <AlertDescription>
            Após fechar este diálogo o token desaparece. Se perder, rotacione a chave para gerar
            uma nova.
          </AlertDescription>
        </Alert>

        <div className="space-y-2">
          <Label>Token</Label>
          <div className="flex gap-2">
            <Input
              readOnly
              value={reveal?.token ?? ""}
              className="font-mono text-xs"
              onFocus={(e) => e.currentTarget.select()}
            />
            <Button type="button" variant="outline" size="icon" onClick={copy}>
              <Copy className="h-4 w-4" />
            </Button>
          </div>
          <p className="text-[11px] text-muted-foreground">
            {copied ? "Copiado ✓" : "Clique no campo para selecionar."}
          </p>
        </div>

        <DialogFooter>
          <Button onClick={onClose}>Já guardei</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
