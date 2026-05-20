import { useEffect, useState } from "react";
import { AppWindow, DollarSign, Network, Users as UsersIcon } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { api, type BudgetCounter } from "@/lib/api";
import { formatBRL, formatNumber } from "@/lib/utils";
import { useSession } from "@/lib/useAuth";

interface Summary {
  applications: { total: number; active: number };
  endpoints: { total: number; active: number };
  users: { total: number; active: number };
  budget: BudgetCounter[];
}

export default function Dashboard() {
  const session = useSession();
  const [summary, setSummary] = useState<Summary | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function load() {
      try {
        const [apps, eps, budget] = await Promise.all([
          api.listApplications().catch(() => []),
          api.listEndpoints().catch(() => []),
          api.listBudget().catch(() => [] as BudgetCounter[]),
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
        });
      } finally {
        setLoading(false);
      }
    }
    void load();
  }, [session?.role]);

  const totalSpend = summary?.budget.reduce((acc, b) => acc + b.estimated_cost_brl, 0) ?? 0;
  const totalRequests = summary?.budget.reduce((acc, b) => acc + b.total_requests, 0) ?? 0;

  return (
    <div className="space-y-6">
      <Alert>
        <AlertTitle>
          Olá{session ? `, ${session.role}` : ""} 👋
        </AlertTitle>
        <AlertDescription>
          Painel do AI Gateway. Resumo do mês corrente, aplicações ativas e endpoints proxied.
        </AlertDescription>
      </Alert>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <SummaryCard
          icon={<AppWindow className="h-5 w-5" />}
          label="Aplicações"
          value={summary?.applications.total}
          subtitle={
            summary && (
              <>
                <Badge variant="success" className="font-mono text-[10px]">
                  {summary.applications.active} ativas
                </Badge>
              </>
            )
          }
          loading={loading}
        />
        <SummaryCard
          icon={<Network className="h-5 w-5" />}
          label="Endpoints"
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

      <Card>
        <CardContent className="p-6">
          <div className="mb-4 flex items-end justify-between">
            <div>
              <h2 className="text-base font-semibold">Budget por aplicação · mês corrente</h2>
              <p className="text-xs text-muted-foreground">
                Valores estimados — refletem requisições registradas em <code>usage_events</code>.
              </p>
            </div>
          </div>
          {loading ? (
            <div className="space-y-2">
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
            </div>
          ) : !summary || summary.budget.length === 0 ? (
            <div className="rounded-md border border-dashed border-border px-4 py-10 text-center text-sm text-muted-foreground">
              Sem dados de consumo no mês corrente.
            </div>
          ) : (
            <ul className="divide-y divide-border/60">
              {summary.budget.map((b) => (
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

function SummaryCard({
  icon,
  label,
  value,
  subtitle,
  loading,
}: {
  icon: React.ReactNode;
  label: string;
  value: number | string | undefined;
  subtitle?: React.ReactNode;
  loading: boolean;
}) {
  return (
    <Card>
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
