import { useLocation, useNavigate } from "react-router-dom";
import { LogOut, ShieldCheck, User as UserIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu";
import { Badge } from "@/components/ui/badge";
import { useSession } from "@/lib/useAuth";
import { clearSession } from "@/lib/auth";
import { api } from "@/lib/api";
import { toast } from "@/components/ui/sonner";

const TITLES: Record<string, string> = {
  "/dashboard": "Visão geral",
  "/applications": "Aplicações",
  "/endpoints": "Endpoints",
  "/users": "Usuários",
  "/observability": "Observabilidade",
};

export function Header() {
  const location = useLocation();
  const navigate = useNavigate();
  const session = useSession();

  const title =
    TITLES[Object.keys(TITLES).find((p) => location.pathname.startsWith(p)) ?? ""] ??
    "Console";

  async function handleLogout() {
    try {
      await api.logout();
    } catch {
      // Even on error, clear locally so the user isn't stuck.
    }
    clearSession();
    toast.success("Sessão encerrada");
    navigate("/login", { replace: true });
  }

  return (
    <header className="sticky top-0 z-10 flex h-16 items-center justify-between border-b border-border bg-background/60 px-8 backdrop-blur-md">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
      </div>

      <div className="flex items-center gap-3">
        {session && (
          <Badge variant={session.role === "admin" ? "default" : "outline"}>
            <ShieldCheck className="mr-1 h-3 w-3" />
            {session.role}
          </Badge>
        )}

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" className="rounded-full">
              <UserIcon className="h-5 w-5" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            <DropdownMenuLabel>Minha conta</DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onSelect={handleLogout}>
              <LogOut className="mr-2 h-4 w-4" />
              Sair
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </header>
  );
}
