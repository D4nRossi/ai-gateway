import { NavLink, useLocation } from "react-router-dom";
import {
  LayoutDashboard,
  AppWindow,
  Network,
  Users as UsersIcon,
  Activity,
  Sparkles,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { useSession } from "@/lib/useAuth";

interface NavItem {
  to: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  /** Hide for users below this role rank. */
  requireRole?: "admin" | "operator" | "viewer";
}

const items: NavItem[] = [
  { to: "/dashboard", label: "Visão geral", icon: LayoutDashboard },
  { to: "/applications", label: "Aplicações", icon: AppWindow, requireRole: "operator" },
  { to: "/endpoints", label: "Endpoints", icon: Network, requireRole: "operator" },
  { to: "/users", label: "Usuários", icon: UsersIcon, requireRole: "admin" },
  { to: "/observability", label: "Observabilidade", icon: Activity, requireRole: "viewer" },
];

const RANK = { viewer: 0, operator: 1, admin: 2 } as const;

export function Sidebar() {
  const location = useLocation();
  const session = useSession();
  const userRank = session ? RANK[session.role] : -1;

  return (
    <aside className="flex h-screen w-64 shrink-0 flex-col border-r border-border bg-card/40 backdrop-blur-md">
      {/* Brand */}
      <div className="flex h-16 items-center gap-2 border-b border-border px-6">
        <div className="flex h-8 w-8 items-center justify-center rounded-md bg-gradient-to-br from-primary to-violet-700 shadow-lg shadow-primary/30">
          <Sparkles className="h-4 w-4 text-white" />
        </div>
        <div className="flex flex-col leading-tight">
          <span className="text-sm font-semibold">AI Gateway</span>
          <span className="text-[10px] uppercase tracking-wider text-muted-foreground">
            Console v2
          </span>
        </div>
      </div>

      {/* Nav */}
      <nav className="flex-1 space-y-1 px-3 py-4">
        {items.map((item) => {
          if (item.requireRole && userRank < RANK[item.requireRole]) {
            return null;
          }
          const isActive =
            location.pathname === item.to ||
            location.pathname.startsWith(item.to + "/");
          return (
            <NavLink
              key={item.to}
              to={item.to}
              className={cn(
                "group flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-all",
                isActive
                  ? "bg-primary/10 text-primary shadow-inner"
                  : "text-muted-foreground hover:bg-muted/50 hover:text-foreground",
              )}
            >
              <item.icon
                className={cn(
                  "h-4 w-4 transition-transform group-hover:scale-110",
                  isActive && "text-primary",
                )}
              />
              {item.label}
            </NavLink>
          );
        })}
      </nav>

      {/* Footer */}
      <div className="border-t border-border p-4 text-[11px] text-muted-foreground">
        <p className="font-mono">v0.1.0 · phase 1</p>
      </div>
    </aside>
  );
}
