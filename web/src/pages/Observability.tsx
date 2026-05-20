import { useEffect, useState } from "react";
import { Loader2, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { toast } from "@/components/ui/sonner";
import {
  api,
  type AuditEvent,
  type BudgetCounter,
  type UsageEvent,
} from "@/lib/api";
import { formatBRL, formatDateTime, formatNumber } from "@/lib/utils";

function isoMinusDays(days: number): string {
  const d = new Date();
  d.setDate(d.getDate() - days);
  return d.toISOString();
}

export default function Observability() {
  const [tab, setTab] = useState("usage");

  return (
    <div className="space-y-6">
      <p className="text-sm text-muted-foreground">
        Eventos registrados pelo gateway. Filtros aplicam-se à aba ativa.
      </p>

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value="usage">Uso</TabsTrigger>
          <TabsTrigger value="audit">Auditoria</TabsTrigger>
          <TabsTrigger value="budget">Budget</TabsTrigger>
        </TabsList>

        <TabsContent value="usage">
          <UsageTab />
        </TabsContent>
        <TabsContent value="audit">
          <AuditTab />
        </TabsContent>
        <TabsContent value="budget">
          <BudgetTab />
        </TabsContent>
      </Tabs>
    </div>
  );
}

function StatusBadge({ code }: { code: number }) {
  const variant =
    code >= 500 ? "destructive" : code >= 400 ? "warning" : code >= 300 ? "outline" : "success";
  return (
    <Badge variant={variant} className="font-mono text-[10px]">
      {code}
    </Badge>
  );
}

function SeverityBadge({ s }: { s: string }) {
  const variant =
    s === "error" ? "destructive" : s === "warn" ? "warning" : s === "info" ? "outline" : "muted";
  return (
    <Badge variant={variant} className="font-mono text-[10px]">
      {s}
    </Badge>
  );
}

// ── Usage tab ─────────────────────────────────────────────────────────────────

function UsageTab() {
  const [rows, setRows] = useState<UsageEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [from, setFrom] = useState(isoMinusDays(1));
  const [to, setTo] = useState(new Date().toISOString());
  const [app, setApp] = useState("");

  async function load() {
    setLoading(true);
    try {
      setRows(await api.listUsage({ from, to, application: app || undefined, limit: 200 }));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Falha ao carregar uso");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="space-y-4">
      <FilterBar
        from={from}
        to={to}
        appFilter={app}
        onChange={(f) => {
          setFrom(f.from);
          setTo(f.to);
          setApp(f.app);
        }}
        onApply={load}
        loading={loading}
      />

      <Card>
        <CardContent className="p-0">
          {loading ? (
            <div className="space-y-2 p-6">
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-full" />
            </div>
          ) : rows.length === 0 ? (
            <div className="px-6 py-10 text-center text-sm text-muted-foreground">
              Nenhum evento no intervalo.
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Quando</TableHead>
                  <TableHead>App</TableHead>
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
                    <TableCell className="font-medium">{u.application_name}</TableCell>
                    <TableCell className="font-mono text-xs">{u.model}</TableCell>
                    <TableCell className="font-mono text-xs">
                      {u.total_tokens != null ? formatNumber(u.total_tokens) : "—"}
                    </TableCell>
                    <TableCell className="font-mono text-xs">{u.latency_ms} ms</TableCell>
                    <TableCell className="font-mono text-xs">
                      {u.estimated_cost_brl != null ? formatBRL(u.estimated_cost_brl) : "—"}
                    </TableCell>
                    <TableCell>
                      <StatusBadge code={u.status_code} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

// ── Audit tab ─────────────────────────────────────────────────────────────────

function AuditTab() {
  const [rows, setRows] = useState<AuditEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [from, setFrom] = useState(isoMinusDays(1));
  const [to, setTo] = useState(new Date().toISOString());
  const [app, setApp] = useState("");
  const [eventType, setEventType] = useState("");

  async function load() {
    setLoading(true);
    try {
      setRows(
        await api.listAudit({
          from,
          to,
          application: app || undefined,
          event_type: eventType || undefined,
          limit: 200,
        }),
      );
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Falha ao carregar auditoria");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="space-y-4">
      <FilterBar
        from={from}
        to={to}
        appFilter={app}
        eventType={eventType}
        onChange={(f) => {
          setFrom(f.from);
          setTo(f.to);
          setApp(f.app);
          if (f.eventType !== undefined) setEventType(f.eventType);
        }}
        onApply={load}
        loading={loading}
      />

      <Card>
        <CardContent className="p-0">
          {loading ? (
            <div className="space-y-2 p-6">
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-full" />
            </div>
          ) : rows.length === 0 ? (
            <div className="px-6 py-10 text-center text-sm text-muted-foreground">
              Nenhum evento no intervalo.
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Quando</TableHead>
                  <TableHead>App</TableHead>
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
                    <TableCell className="font-medium">{a.application_name}</TableCell>
                    <TableCell className="font-mono text-xs">{a.event_type}</TableCell>
                    <TableCell>
                      <SeverityBadge s={a.severity} />
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
    </div>
  );
}

// ── Budget tab ────────────────────────────────────────────────────────────────

function BudgetTab() {
  const [rows, setRows] = useState<BudgetCounter[]>([]);
  const [loading, setLoading] = useState(true);
  const [period, setPeriod] = useState(currentPeriod());

  async function load() {
    setLoading(true);
    try {
      setRows(await api.listBudget({ period }));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Falha ao carregar budget");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="space-y-4">
      <Card>
        <CardContent className="flex flex-wrap items-end gap-4 p-4">
          <div className="space-y-1">
            <Label htmlFor="period">Período (YYYYMM)</Label>
            <Input
              id="period"
              value={period}
              onChange={(e) => setPeriod(e.target.value)}
              className="w-32 font-mono"
              maxLength={6}
            />
          </div>
          <Button onClick={load} disabled={loading} className="ml-auto">
            {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
            Aplicar
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="p-0">
          {loading ? (
            <div className="space-y-2 p-6">
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-full" />
            </div>
          ) : rows.length === 0 ? (
            <div className="px-6 py-10 text-center text-sm text-muted-foreground">
              Sem dados para o período {period}.
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Aplicação</TableHead>
                  <TableHead>Requisições</TableHead>
                  <TableHead>Tokens</TableHead>
                  <TableHead>Custo estimado</TableHead>
                  <TableHead>Atualizado em</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((b) => (
                  <TableRow key={b.application_name}>
                    <TableCell className="font-medium">{b.application_name}</TableCell>
                    <TableCell className="font-mono text-xs">
                      {formatNumber(b.total_requests)}
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {formatNumber(b.total_tokens)}
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {formatBRL(b.estimated_cost_brl)}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {formatDateTime(b.updated_at)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function currentPeriod(): string {
  const d = new Date();
  return `${d.getUTCFullYear()}${String(d.getUTCMonth() + 1).padStart(2, "0")}`;
}

// ── Shared filter bar ─────────────────────────────────────────────────────────

interface FilterChange {
  from: string;
  to: string;
  app: string;
  eventType?: string;
}

function FilterBar({
  from,
  to,
  appFilter,
  eventType,
  onChange,
  onApply,
  loading,
}: {
  from: string;
  to: string;
  appFilter: string;
  eventType?: string;
  onChange: (f: FilterChange) => void;
  onApply: () => void;
  loading: boolean;
}) {
  return (
    <Card>
      <CardContent className="grid grid-cols-1 items-end gap-3 p-4 md:grid-cols-5">
        <div className="space-y-1">
          <Label className="text-xs">De (UTC)</Label>
          <Input
            type="datetime-local"
            value={toLocalInput(from)}
            onChange={(e) =>
              onChange({
                from: fromLocalInput(e.target.value),
                to,
                app: appFilter,
                eventType,
              })
            }
          />
        </div>
        <div className="space-y-1">
          <Label className="text-xs">Até (UTC)</Label>
          <Input
            type="datetime-local"
            value={toLocalInput(to)}
            onChange={(e) =>
              onChange({
                from,
                to: fromLocalInput(e.target.value),
                app: appFilter,
                eventType,
              })
            }
          />
        </div>
        <div className="space-y-1">
          <Label className="text-xs">Aplicação</Label>
          <Input
            value={appFilter}
            onChange={(e) =>
              onChange({ from, to, app: e.target.value, eventType })
            }
            placeholder="(todas)"
          />
        </div>
        {eventType !== undefined && (
          <div className="space-y-1">
            <Label className="text-xs">Tipo de evento</Label>
            <Input
              value={eventType}
              onChange={(e) =>
                onChange({ from, to, app: appFilter, eventType: e.target.value })
              }
              placeholder="(todos)"
            />
          </div>
        )}
        <Button onClick={onApply} disabled={loading}>
          {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
          Aplicar
        </Button>
      </CardContent>
    </Card>
  );
}

function toLocalInput(iso: string): string {
  try {
    const d = new Date(iso);
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
  } catch {
    return "";
  }
}

function fromLocalInput(v: string): string {
  if (!v) return new Date().toISOString();
  return new Date(v).toISOString();
}
