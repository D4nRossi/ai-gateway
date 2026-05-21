import { Link, useLocation } from "react-router-dom";
import { ChevronRight } from "lucide-react";
import { cn } from "@/lib/utils";

interface Crumb {
  label: string;
  to?: string;
}

/**
 * Breadcrumbs — deriva a trilha do pathname.
 *
 * Para evitar dependência do `useMatches` (que exige createBrowserRouter, não
 * o BrowserRouter declarativo), o componente é totalmente baseado no caminho
 * URL. Detail pages exibem o nome dinâmico no próprio cabeçalho da página;
 * aqui mostramos apenas "Detalhes" para segmentos numéricos, o que já dá
 * navegação reversa correta.
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
  const segments = location.pathname.split("/").filter(Boolean);

  if (segments.length === 0) {
    return null;
  }

  const crumbs: Crumb[] = [];
  let acc = "";
  for (let i = 0; i < segments.length; i++) {
    acc += "/" + segments[i];
    const seg = segments[i];
    const last = i === segments.length - 1;

    let label: string;
    if (ROOT_LABELS[seg]) {
      label = ROOT_LABELS[seg];
    } else if (/^\d+$/.test(seg)) {
      // Detail page (id numérico) — a tela mostra o nome real grande no header.
      label = "Detalhes";
    } else {
      label = seg.charAt(0).toUpperCase() + seg.slice(1);
    }

    crumbs.push({ label, to: last ? undefined : acc });
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
