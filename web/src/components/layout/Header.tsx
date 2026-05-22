import { useNavigate } from "react-router-dom";
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
import { Breadcrumbs } from "./Breadcrumbs";

interface HeaderProps {
  /** Slot opcional renderizado à esquerda do breadcrumb (ex: botão de menu mobile). */
  leftSlot?: React.ReactNode;
}

export function Header({ leftSlot }: HeaderProps) {
  const navigate = useNavigate();
  const session = useSession();

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
    <header className="sticky top-0 z-10 flex h-16 items-center justify-between gap-3 border-b border-border bg-background/60 px-4 backdrop-blur-md sm:px-6 lg:px-8">
      <div className="flex min-w-0 items-center gap-2">
        {leftSlot}
        <div className="min-w-0 truncate">
          <Breadcrumbs />
        </div>
      </div>

      <div className="flex shrink-0 items-center gap-2 sm:gap-3">
        {session && (
          <Badge
            variant={session.role === "admin" ? "default" : "outline"}
            className="hidden sm:inline-flex"
          >
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
