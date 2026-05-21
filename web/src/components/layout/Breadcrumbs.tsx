import { Link, useLocation, useMatches } from "react-router-dom";
import { ChevronRight } from "lucide-react";
import { cn } from "@/lib/utils";

interface Crumb {
  label: string;
  to?: string;
}

/**
 * Breadcrumbs — deriva a trilha a partir do pathname + dados expostos por
 * detail pages via React Router data ("handle" no Route).
 *
 * Páginas que precisam mostrar nome dinâmico (ex: /applications/:id → "AppDemo")
 * exportam um <BreadcrumbContext> com o label atualizado. As listagens não
 * precisam — o mapa estático cobre o nível raiz.
 */
const ROOT_LABELS: Record<string, string> = {
  dashboard: "Visão geral",
  applications: "Aplicações",
  endpoints: "Endpoints",
  users: "Usuários",
  observability: "Observabilidade",
};

export function Breadcrumbs() {
  const location = useLocation();
  const matches = useMatches();
  const segments = location.pathname.split("/").filter(Boolean);

  const crumbs: Crumb[] = [];
  let acc = "";
  for (let i = 0; i < segments.length; i++) {
    acc += "/" + segments[i];
    const seg = segments[i];

    // Static root label (Applications, Endpoints…)
    const rootLabel = ROOT_LABELS[seg];
    if (rootLabel) {
      crumbs.push({
        label: rootLabel,
        to: i < segments.length - 1 ? acc : undefined,
      });
      continue;
    }

    // Detail pages can advertise their label via Route `handle: { crumb: "..." }`.
    // useMatches surfaces handle data from every matched route in order.
    const handle = matches[matches.length - 1]?.handle as
      | { crumb?: string | ((params: Record<string, string | undefined>) => string) }
      | undefined;
    if (i === segments.length - 1 && handle?.crumb) {
      const params = (matches[matches.length - 1]?.params ?? {}) as Record<string, string | undefined>;
      const label = typeof handle.crumb === "function" ? handle.crumb(params) : handle.crumb;
      crumbs.push({ label });
      continue;
    }

    // Fallback: capitalise the raw segment.
    crumbs.push({
      label: seg.charAt(0).toUpperCase() + seg.slice(1),
      to: i < segments.length - 1 ? acc : undefined,
    });
  }

  if (crumbs.length === 0) {
    return null;
  }

  return (
    <nav aria-label="Breadcrumb" className="flex items-center gap-1 text-sm">
      {crumbs.map((c, i) => (
        <span key={i} className="flex items-center gap-1">
          {i > 0 && <ChevronRight className="h-3.5 w-3.5 text-muted-foreground/60" />}
          {c.to ? (
            <Link
              to={c.to}
              className="text-muted-foreground transition-colors hover:text-foreground"
            >
              {c.label}
            </Link>
          ) : (
            <span
              className={cn(
                "font-medium",
                i === crumbs.length - 1 ? "text-foreground" : "text-muted-foreground",
              )}
            >
              {c.label}
            </span>
          )}
        </span>
      ))}
    </nav>
  );
}
