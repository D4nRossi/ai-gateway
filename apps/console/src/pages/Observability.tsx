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
  errMessage,
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
  // "info" maps to secondary (informativo positivo) em vez de "outline" (neutro
  // estilizado), pra dar contraste maior com "warn"/"error" — antes "info" e
  // severidades desconhecidas ficavam visualmente indistinguíveis.
  const variant =
    s === "error" ? "destructive" :
    s === "warn"  ? "warning"     :
    s === "info"  ? "secondary"   :
                    "muted";
  return (
    <Badge variant={variant} className="font-mono text-[10px]">
      {s}
    </Badge>
  );
}

// ── Usage tab ─────────────────────────────────────────────────────────────────

const USAGE_LIMIT = 200;

function UsageTab() {
  const [rows, setRows] = useState<UsageEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [from, setFrom] = useState(isoMinusDays(1));
  const [to, setTo] = useState(new Date().toISOString());
  const [app, setApp] = useState("");

  // Bug fix: o `to` default era capturado na primeira render e ficava
  // congelado. Quando o usuário reabria a aba 1h depois e clicava "Aplicar"
  // sem mexer nos filtros, o `to` continuava com a hora antiga e dados
  // recentes não apareciam. `refreshTo` é chamado antes de cada load
  // disparado pela UI — preserva o comportamento de filtragem por intervalo
  // explícito mas mantém o "agora" atualizado quando o usuário não tocou no
  // campo manualmente.
  async function load({ refreshTo }: { refreshTo: boolean } = { refreshTo: false }) {
    const effectiveTo = refreshTo ? new Date().toISOString() : to;
    if (refreshTo) setTo(effectiveTo);
    setLoading(true);
    try {
      setRows(
        await api.listUsage({
          from,
          to: effectiveTo,
          application: app || undefined,
          limit: USAGE_LIMIT,
        }),
      );
    } catch (err) {
      toast.error(errMessage(err, "Falha ao carregar uso"));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load({ refreshTo: true });
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
        onApply={() => load({ refreshTo: false })}
        loading={loading}
      />

      <Card>
        <CardContent className="p-0">
          {loading ? (
            <TableSkeleton columns={7} rows={6} />
          ) : rows.length === 0 ? (
            <EmptyState message="Nenhum evento no intervalo." />
          ) : (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead title="Timestamp UTC convertido pro fuso local do navegador.">Quando</TableHead>
                    <TableHead>App</TableHead>
                    <TableHead title="Nome público do modelo (public_name no catálogo). O deployment real fica oculto.">Modelo</TableHead>
                    <TableHead title="total_tokens = input_tokens + output_tokens reportados pelo provider.">Tokens</TableHead>
                    <TableHead title="latency_ms — tempo total da request no gateway, do auth ao envio do response.">Latência</TableHead>
                    <TableHead title="estimated_cost_brl — token count × cost_per_1k_brl do modelo (gateway.yaml).">Custo</TableHead>
                    <TableHead title="HTTP status code retornado ao consumidor.">Status</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rows.map((u) => (
                    <TableRow key={u.id}>
                      <TableCell className="text-xs text-muted-foreground" title={u.created_at}>
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
              <ResultsFooter count={rows.length} limit={USAGE_LIMIT} />
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

// ── Audit tab ─────────────────────────────────────────────────────────────────

const AUDIT_LIMIT = 200;

function AuditTab() {
  const [rows, setRows] = useState<AuditEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [from, setFrom] = useState(isoMinusDays(1));
  const [to, setTo] = useState(new Date().toISOString());
  const [app, setApp] = useState("");
  const [eventType, setEventType] = useState("");

  async function load({ refreshTo }: { refreshTo: boolean } = { refreshTo: false }) {
    const effectiveTo = refreshTo ? new Date().toISOString() : to;
    if (refreshTo) setTo(effectiveTo);
    setLoading(true);
    try {
      setRows(
        await api.listAudit({
          from,
          to: effectiveTo,
          application: app || undefined,
          event_type: eventType || undefined,
          limit: AUDIT_LIMIT,
        }),
      );
    } catch (err) {
      toast.error(errMessage(err, "Falha ao carregar auditoria"));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load({ refreshTo: true });
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
        onApply={() => load({ refreshTo: false })}
        loading={loading}
      />

      <Card>
        <CardContent className="p-0">
          {loading ? (
            <TableSkeleton columns={5} rows={6} />
          ) : rows.length === 0 ? (
            <EmptyState message="Nenhum evento no intervalo." />
          ) : (
            <>
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
                      <MetadataCell raw={a.metadata} />
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              <ResultsFooter count={rows.length} limit={AUDIT_LIMIT} />
            </>
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
  const periodError = validatePeriod(period);

  async function load() {
    if (validatePeriod(period) !== null) return;
    setLoading(true);
    try {
      setRows(await api.listBudget({ period }));
    } catch (err) {
      toast.error(errMessage(err, "Falha ao carregar budget"));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Top-down ordering: custo total descendente coloca os "top spenders" no
  // topo. Antes do fix, a ordem vinha do DB (alfabética por app_name), que
  // enterrava informação importante.
  const sortedRows = [...rows].sort((a, b) => b.estimated_cost_brl - a.estimated_cost_brl);
  const totalRequests = sortedRows.reduce((acc, b) => acc + b.total_requests, 0);
  const totalTokens = sortedRows.reduce((acc, b) => acc + b.total_tokens, 0);
  const totalCost = sortedRows.reduce((acc, b) => acc + b.estimated_cost_brl, 0);

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
              aria-invalid={periodError !== null}
            />
            {periodError && (
              <p className="text-[11px] text-destructive">{periodError}</p>
            )}
          </div>
          <Button
            onClick={load}
            disabled={loading || periodError !== null}
            className="ml-auto"
          >
            {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
            Aplicar
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="p-0">
          {loading ? (
            <TableSkeleton columns={5} rows={4} />
          ) : sortedRows.length === 0 ? (
            <EmptyState message={`Sem dados para o período ${period}.`} />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Aplicação</TableHead>
                  <TableHead className="text-right">Requisições</TableHead>
                  <TableHead className="text-right">Tokens</TableHead>
                  <TableHead className="text-right">Custo estimado</TableHead>
                  <TableHead>Atualizado em</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {sortedRows.map((b) => (
                  <TableRow key={b.application_name}>
                    <TableCell className="font-medium">{b.application_name}</TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {formatNumber(b.total_requests)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {formatNumber(b.total_tokens)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {formatBRL(b.estimated_cost_brl)}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {formatDateTime(b.updated_at)}
                    </TableCell>
                  </TableRow>
                ))}
                <TableRow className="bg-muted/30 font-semibold">
                  <TableCell>Total · {sortedRows.length} app(s)</TableCell>
                  <TableCell className="text-right font-mono text-xs">
                    {formatNumber(totalRequests)}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs">
                    {formatNumber(totalTokens)}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs">
                    {formatBRL(totalCost)}
                  </TableCell>
                  <TableCell />
                </TableRow>
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

/**
 * Valida YYYYMM. Aceita meses 01-12 de qualquer ano entre 2000 e 2099.
 * Retorna null se válido, mensagem de erro caso contrário.
 */
function validatePeriod(p: string): string | null {
  if (!/^\d{6}$/.test(p)) return "Use 6 dígitos (ex: 202605)";
  const year = parseInt(p.slice(0, 4), 10);
  const month = parseInt(p.slice(4, 6), 10);
  if (year < 2000 || year > 2099) return "Ano deve estar entre 2000 e 2099";
  if (month < 1 || month > 12) return "Mês deve estar entre 01 e 12";
  return null;
}

function currentPeriod(): string {
  const d = new Date();
  return `${d.getUTCFullYear()}${String(d.getUTCMonth() + 1).padStart(2, "0")}`;
}

// ── Shared visual primitives ─────────────────────────────────────────────────

/**
 * Esqueleto de linha de tabela alinhado ao número de colunas. Antes do fix,
 * cada tab tinha 2-3 linhas hard-coded — quando a tabela renderizada tinha
 * 7 colunas e 50 rows, o skeleton parecia desproporcional. Aqui o número
 * de linhas é controlado pela call site (`rows`) e o número de colunas
 * pelo prop `columns`.
 */
function TableSkeleton({ columns, rows }: { columns: number; rows: number }) {
  return (
    <div className="p-4">
      <Table>
        <TableHeader>
          <TableRow>
            {Array.from({ length: columns }).map((_, i) => (
              <TableHead key={i}>
                <Skeleton className="h-3 w-16" />
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {Array.from({ length: rows }).map((_, r) => (
            <TableRow key={r}>
              {Array.from({ length: columns }).map((_, c) => (
                <TableCell key={c}>
                  <Skeleton className="h-4 w-full max-w-[120px]" />
                </TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

/** Empty state padronizado pras 3 tabs. */
function EmptyState({ message }: { message: string }) {
  return (
    <div className="px-6 py-12 text-center text-sm text-muted-foreground">
      {message}
    </div>
  );
}

/**
 * Mostra contagem de resultados + sinalização explícita de truncagem quando
 * o backend devolveu exatamente o limite (`count === limit`), que provavelmente
 * significa que há mais dados na janela e o usuário precisa estreitar o filtro.
 */
function ResultsFooter({ count, limit }: { count: number; limit: number }) {
  const truncated = count >= limit;
  return (
    <div className="flex items-center justify-between border-t border-border/40 px-4 py-2 text-[11px] text-muted-foreground">
      <span>
        {count} {count === 1 ? "resultado" : "resultados"}
      </span>
      {truncated && (
        <span className="font-medium text-warning-foreground/95">
          Limite de {limit} atingido — refine o filtro para ver mais.
        </span>
      )}
    </div>
  );
}

/**
 * Renderiza metadata JSON com pretty-print no `title` (tooltip nativo) e o
 * texto cru truncado a 80 chars na célula. Quando a string é vazia, mostra
 * `—` em vez de espaço em branco.
 */
function MetadataCell({ raw }: { raw: string | null }) {
  if (raw == null || raw.trim() === "") {
    return (
      <TableCell className="font-mono text-[11px] text-muted-foreground">—</TableCell>
    );
  }
  let pretty = raw;
  try {
    pretty = JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    // Não é JSON válido — preserva o conteúdo original como tooltip
  }
  const truncated = raw.length > 80 ? raw.slice(0, 80) + "…" : raw;
  return (
    <TableCell
      className="max-w-[420px] truncate font-mono text-[11px] text-muted-foreground"
      title={pretty}
    >
      {truncated}
    </TableCell>
  );
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
