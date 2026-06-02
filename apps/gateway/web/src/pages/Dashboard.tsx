import { useEffect, useState } from "react";
import { AppWindow, DollarSign, Loader2, Network, RefreshCw, Users as UsersIcon } from "lucide-react";
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Legend,
  Line,
  LineChart,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip as ChartTooltip,
  XAxis,
  YAxis,
} from "recharts";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Badge } from "@/components/ui/badge";
import {
  api,
  type BudgetCounter,
  type DashboardBreakdownRow,
  type DashboardTimeseriesPoint,
} from "@/lib/api";
import { formatBRL, formatNumber } from "@/lib/utils";
import { useSession } from "@/lib/useAuth";

// Paleta única usada em todos os charts. Cores escolhidas pra funcionar em
// light e dark mode sem precisar de CSS vars (recharts não consome var(--))
// e pra serem distinguíveis pra daltonismo comum.
const CHART_COLORS = {
  primary: "#a78bfa",   // violet — uso geral
  success: "#10b981",   // emerald — métricas positivas
  warning: "#f59e0b",   // amber — atenção
  danger: "#ef4444",    // red — erros
  info: "#22d3ee",      // cyan — latência
  neutral: "#94a3b8",   // slate — secundário
} as const;
const PIE_PALETTE = [
  CHART_COLORS.primary,
  CHART_COLORS.info,
  CHART_COLORS.success,
  CHART_COLORS.warning,
  CHART_COLORS.danger,
  CHART_COLORS.neutral,
];

interface Summary {
  applications: { total: number; active: number };
  endpoints: { total: number; active: number };
  users: { total: number; active: number };
  budget: BudgetCounter[];
  timeseries: DashboardTimeseriesPoint[];
  appBreakdown: DashboardBreakdownRow[];
  tierBreakdown: DashboardBreakdownRow[];
}

/** Janela default dos charts: últimas 24h em buckets de 1h. */
function defaultRange() {
  const to = new Date();
  const from = new Date(to.getTime() - 24 * 60 * 60 * 1000);
  return { from: from.toISOString(), to: to.toISOString() };
}

export default function Dashboard() {
  const session = useSession();
  const [summary, setSummary] = useState<Summary | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshedAt, setRefreshedAt] = useState<Date | null>(null);

  async function load() {
    setLoading(true);
    try {
      const range = defaultRange();
      const [apps, eps, budget, timeseries, appBreakdown, tierBreakdown] = await Promise.all([
        api.listApplications().catch(() => []),
        api.listEndpoints().catch(() => []),
        api.listBudget().catch(() => [] as BudgetCounter[]),
        api
          .listDashboardTimeseries({ ...range, bucket: "hour" })
          .catch(() => [] as DashboardTimeseriesPoint[]),
        api
          .listDashboardBreakdown({ ...range, dimension: "application", limit: 10 })
          .catch(() => [] as DashboardBreakdownRow[]),
        api
          .listDashboardBreakdown({ ...range, dimension: "tier" })
          .catch(() => [] as DashboardBreakdownRow[]),
      ]);
      let users: Awaited<ReturnType<typeof api.listUsers>> = [];
      if (session?.role === "admin") {
        users = await api.listUsers().catch(() => []);
      }
      setSummary({
        applications: {
          total: apps.length,
          active: apps.filter((a) => a.active).length,
        },
        endpoints: { total: eps.length, active: eps.filter((e) => e.active).length },
        users: { total: users.length, active: users.filter((u) => u.active).length },
        budget,
        timeseries,
        appBreakdown,
        tierBreakdown,
      });
      setRefreshedAt(new Date());
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session?.role]);

  // Top spenders no topo: o backend devolve a ordem do agrupamento (alfabética
  // por application_name), o que enterra a informação mais importante quando
  // há muitas apps. Ordenamos no front por estimated_cost_brl desc.
  const sortedBudget = summary
    ? [...summary.budget].sort((a, b) => b.estimated_cost_brl - a.estimated_cost_brl)
    : [];
  const totalSpend = sortedBudget.reduce((acc, b) => acc + b.estimated_cost_brl, 0);
  const totalRequests = sortedBudget.reduce((acc, b) => acc + b.total_requests, 0);

  return (
    <div className="space-y-6">
      <div className="flex items-end justify-between gap-4">
        <div className="space-y-1">
          <h1 className="text-lg font-semibold">Painel</h1>
          <p className="text-xs text-muted-foreground">
            Resumo do mês corrente · aplicações, endpoints e gasto agregado.
            {refreshedAt && (
              <> Atualizado às <span className="font-mono">{refreshedAt.toLocaleTimeString("pt-BR")}</span>.</>
            )}
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={() => void load()} disabled={loading}>
          {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
          Atualizar
        </Button>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <SummaryCard
          icon={<AppWindow className="h-5 w-5" />}
          label="Aplicações"
          tooltip="Total de aplicações cadastradas. Cada uma é um consumidor com bearer token próprio."
          value={summary?.applications.total}
          subtitle={
            summary && (
              <Badge variant="success" className="font-mono text-[10px]">
                {summary.applications.active} ativas
              </Badge>
            )
          }
          loading={loading}
        />
        <SummaryCard
          icon={<Network className="h-5 w-5" />}
          label="Endpoints"
          tooltip="Proxy endpoints expostos em /v1/proxy/{slug}. Cada um aponta pra um upstream específico."
          value={summary?.endpoints.total}
          subtitle={
            summary && (
              <Badge variant="success" className="font-mono text-[10px]">
                {summary.endpoints.active} ativos
              </Badge>
            )
          }
          loading={loading}
        />
        {session?.role === "admin" && (
          <SummaryCard
            icon={<UsersIcon className="h-5 w-5" />}
            label="Operadores"
            tooltip="Admin users com acesso ao Console. Roles: admin / operator / viewer."
            value={summary?.users.total}
            subtitle={
              summary && (
                <Badge variant="outline" className="font-mono text-[10px]">
                  {summary.users.active} ativos
                </Badge>
              )
            }
            loading={loading}
          />
        )}
        <SummaryCard
          icon={<DollarSign className="h-5 w-5" />}
          label="Gasto no mês"
          tooltip="Estimativa em BRL baseada em estimated_cost_brl agregado por aplicação no mês corrente."
          value={loading ? undefined : formatBRL(totalSpend)}
          subtitle={
            summary && (
              <span className="text-[11px] text-muted-foreground">
                {formatNumber(totalRequests)} requisições
              </span>
            )
          }
          loading={loading}
        />
      </div>

      {/* ── Charts (últimas 24h) ─────────────────────────────────────────── */}
      <div className="space-y-6">
        <ChartCard
          title="Requests por hora — últimas 24h"
          subtitle="Buckets de 1h. Erros 4xx/5xx empilhados na mesma escala."
          loading={loading}
          empty={!summary || summary.timeseries.length === 0}
        >
          <ResponsiveContainer width="100%" height={240}>
            <LineChart data={summary?.timeseries ?? []}>
              <CartesianGrid strokeDasharray="3 3" stroke="rgba(148,163,184,0.2)" />
              <XAxis dataKey="bucket_start" tickFormatter={formatHour} stroke="rgba(148,163,184,0.6)" fontSize={11} />
              <YAxis stroke="rgba(148,163,184,0.6)" fontSize={11} allowDecimals={false} />
              <ChartTooltip
                contentStyle={tooltipStyle}
                labelFormatter={(v) => new Date(v).toLocaleString("pt-BR")}
                formatter={(value, name) => [formatNumber(Number(value)), labelOf(String(name))]}
              />
              <Legend wrapperStyle={{ fontSize: 11 }} />
              <Line type="monotone" dataKey="request_count" stroke={CHART_COLORS.primary} strokeWidth={2} dot={false} name="Total" />
              <Line type="monotone" dataKey="error_count_4xx" stroke={CHART_COLORS.warning} strokeWidth={1.5} dot={false} name="4xx" />
              <Line type="monotone" dataKey="error_count_5xx" stroke={CHART_COLORS.danger} strokeWidth={1.5} dot={false} name="5xx" />
            </LineChart>
          </ResponsiveContainer>
        </ChartCard>

        <ChartCard
          title="Latência por hora — média e máxima"
          subtitle="ms — chamadas chat legacy e proxy (ADR-0024). Percentis virão em V2."
          loading={loading}
          empty={!summary || summary.timeseries.length === 0}
        >
          <ResponsiveContainer width="100%" height={220}>
            <LineChart data={summary?.timeseries ?? []}>
              <CartesianGrid strokeDasharray="3 3" stroke="rgba(148,163,184,0.2)" />
              <XAxis dataKey="bucket_start" tickFormatter={formatHour} stroke="rgba(148,163,184,0.6)" fontSize={11} />
              <YAxis stroke="rgba(148,163,184,0.6)" fontSize={11} unit=" ms" />
              <ChartTooltip
                contentStyle={tooltipStyle}
                labelFormatter={(v) => new Date(v).toLocaleString("pt-BR")}
                formatter={(value, name) => [`${Math.round(Number(value))} ms`, labelOf(String(name))]}
              />
              <Legend wrapperStyle={{ fontSize: 11 }} />
              <Line type="monotone" dataKey="avg_latency_ms" stroke={CHART_COLORS.info} strokeWidth={2} dot={false} name="Média" />
              <Line type="monotone" dataKey="max_latency_ms" stroke={CHART_COLORS.warning} strokeWidth={1.5} dot={false} strokeDasharray="4 4" name="Máxima" />
            </LineChart>
          </ResponsiveContainer>
        </ChartCard>

        <ChartCard
          title="Custo BRL por hora — últimas 24h"
          subtitle="Estimativa baseada em cost_per_1k do catálogo (gateway.yaml)."
          loading={loading}
          empty={!summary || summary.timeseries.length === 0}
        >
          <ResponsiveContainer width="100%" height={200}>
            <AreaChart data={summary?.timeseries ?? []}>
              <CartesianGrid strokeDasharray="3 3" stroke="rgba(148,163,184,0.2)" />
              <XAxis dataKey="bucket_start" tickFormatter={formatHour} stroke="rgba(148,163,184,0.6)" fontSize={11} />
              <YAxis stroke="rgba(148,163,184,0.6)" fontSize={11} tickFormatter={(v) => formatBRL(v as number)} />
              <ChartTooltip
                contentStyle={tooltipStyle}
                labelFormatter={(v) => new Date(v).toLocaleString("pt-BR")}
                formatter={(value) => [formatBRL(Number(value)), "Custo"]}
              />
              <Area type="monotone" dataKey="total_cost_brl" stroke={CHART_COLORS.success} fill={CHART_COLORS.success} fillOpacity={0.15} />
            </AreaChart>
          </ResponsiveContainer>
        </ChartCard>

        <div className="grid gap-6 lg:grid-cols-2">
          <ChartCard
            title="Top apps por gasto — últimas 24h"
            subtitle="Top 10 ordenado por custo descendente."
            loading={loading}
            empty={!summary || summary.appBreakdown.length === 0}
          >
            <ResponsiveContainer width="100%" height={Math.max(200, (summary?.appBreakdown.length ?? 1) * 28)}>
              <BarChart data={summary?.appBreakdown ?? []} layout="vertical" margin={{ left: 80 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="rgba(148,163,184,0.2)" horizontal={false} />
                <XAxis type="number" stroke="rgba(148,163,184,0.6)" fontSize={11} tickFormatter={(v) => formatBRL(v as number)} />
                <YAxis type="category" dataKey="key" stroke="rgba(148,163,184,0.6)" fontSize={11} width={80} />
                <ChartTooltip
                  contentStyle={tooltipStyle}
                  formatter={(value, name) =>
                    name === "total_cost_brl"
                      ? [formatBRL(Number(value)), "Custo"]
                      : [formatNumber(Number(value)), labelOf(String(name))]
                  }
                />
                <Bar dataKey="total_cost_brl" fill={CHART_COLORS.primary} radius={[0, 4, 4, 0]} />
              </BarChart>
            </ResponsiveContainer>
          </ChartCard>

          <ChartCard
            title="Distribuição por tier — últimas 24h"
            subtitle="Requests por tier de segurança."
            loading={loading}
            empty={!summary || summary.tierBreakdown.length === 0}
          >
            <ResponsiveContainer width="100%" height={240}>
              <PieChart>
                <ChartTooltip
                  contentStyle={tooltipStyle}
                  formatter={(value, name) => [formatNumber(Number(value)), String(name)]}
                />
                <Legend wrapperStyle={{ fontSize: 11 }} />
                <Pie
                  data={summary?.tierBreakdown ?? []}
                  dataKey="request_count"
                  nameKey="key"
                  outerRadius={80}
                  label={(p) => String(p.key ?? "")}
                >
                  {(summary?.tierBreakdown ?? []).map((_, i) => (
                    <Cell key={i} fill={PIE_PALETTE[i % PIE_PALETTE.length]} />
                  ))}
                </Pie>
              </PieChart>
            </ResponsiveContainer>
          </ChartCard>
        </div>
      </div>

      <Card>
        <CardContent className="p-6">
          <div className="mb-4 flex items-end justify-between gap-4">
            <div>
              <h2 className="text-base font-semibold">Budget por aplicação · mês corrente</h2>
              <p className="text-xs text-muted-foreground">
                Ordenado por gasto descendente. Valores estimados — refletem requisições em
                {" "}<code>gogateway.usage_events</code>.
              </p>
            </div>
            {sortedBudget.length > 0 && (
              <Badge variant="outline" className="font-mono text-[10px]">
                {sortedBudget.length} app(s)
              </Badge>
            )}
          </div>
          {loading ? (
            <div className="space-y-2">
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
            </div>
          ) : sortedBudget.length === 0 ? (
            <div className="rounded-md border border-dashed border-border px-4 py-10 text-center text-sm text-muted-foreground">
              Sem dados de consumo no mês corrente.
            </div>
          ) : (
            <ul className="divide-y divide-border/60">
              {sortedBudget.map((b) => (
                <li
                  key={b.application_name}
                  className="flex items-center justify-between py-3 text-sm"
                >
                  <span className="font-medium">{b.application_name}</span>
                  <div className="flex items-center gap-4">
                    <span className="text-xs text-muted-foreground">
                      {formatNumber(b.total_requests)} req · {formatNumber(b.total_tokens)} tokens
                    </span>
                    <span className="font-mono">{formatBRL(b.estimated_cost_brl)}</span>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

// ── Chart helpers ────────────────────────────────────────────────────────────

const tooltipStyle = {
  backgroundColor: "rgba(15, 23, 42, 0.95)",
  border: "1px solid rgba(148, 163, 184, 0.3)",
  borderRadius: "6px",
  fontSize: "12px",
};

/**
 * Mapeia keys das séries (request_count, error_count_4xx etc) pra rótulos
 * humanos exibidos nos tooltips. Mantido inline pra evitar overhead de i18n
 * num projeto monolíngue.
 */
function labelOf(name: string): string {
  switch (name) {
    case "request_count": return "Requests";
    case "error_count_4xx": return "Erros 4xx";
    case "error_count_5xx": return "Erros 5xx";
    case "avg_latency_ms": return "Média";
    case "max_latency_ms": return "Máxima";
    case "total_tokens": return "Tokens";
    case "total_cost_brl": return "Custo";
    default: return name;
  }
}

function formatHour(value: string | number): string {
  const d = new Date(value);
  return `${String(d.getHours()).padStart(2, "0")}:00`;
}

function ChartCard({
  title,
  subtitle,
  loading,
  empty,
  children,
}: {
  title: string;
  subtitle?: string;
  loading: boolean;
  empty: boolean;
  children: React.ReactNode;
}) {
  return (
    <Card>
      <CardContent className="p-6">
        <div className="mb-4">
          <h2 className="text-base font-semibold">{title}</h2>
          {subtitle && <p className="text-xs text-muted-foreground">{subtitle}</p>}
        </div>
        {loading ? (
          <Skeleton className="h-[200px] w-full" />
        ) : empty ? (
          <div className="rounded-md border border-dashed border-border px-4 py-12 text-center text-sm text-muted-foreground">
            Sem dados no intervalo.
          </div>
        ) : (
          children
        )}
      </CardContent>
    </Card>
  );
}

function SummaryCard({
  icon,
  label,
  tooltip,
  value,
  subtitle,
  loading,
}: {
  icon: React.ReactNode;
  label: string;
  /** Optional hover hint to explain the metric without occupying screen space. */
  tooltip?: string;
  value: number | string | undefined;
  subtitle?: React.ReactNode;
  loading: boolean;
}) {
  return (
    <Card title={tooltip}>
      <CardContent className="p-6">
        <div className="mb-3 flex items-center gap-2 text-muted-foreground">
          {icon}
          <span className="text-xs font-medium uppercase tracking-wider">{label}</span>
        </div>
        {loading ? (
          <Skeleton className="h-9 w-24" />
        ) : (
          <p className="text-3xl font-semibold tracking-tight">
            {typeof value === "number" ? formatNumber(value) : (value ?? "—")}
          </p>
        )}
        {subtitle && <div className="mt-3 flex items-center gap-2">{subtitle}</div>}
      </CardContent>
    </Card>
  );
}
