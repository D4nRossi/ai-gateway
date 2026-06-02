import { useEffect, useState } from "react";
import { Outlet, useLocation } from "react-router-dom";
import { Menu, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Sidebar } from "./Sidebar";
import { Header } from "./Header";
import { useKeyboardShortcuts } from "@/lib/useKeyboardShortcuts";
import { cn } from "@/lib/utils";

/**
 * AppShell — layout responsivo.
 *
 * Desktop (≥ lg): sidebar fixa à esquerda + main à direita.
 * Mobile/tablet (< lg): sidebar oculta; botão hambúrguer no header abre um
 * drawer absoluto sobre o conteúdo. Fecha ao navegar (mudança de pathname)
 * ou clicar no overlay.
 */
export function AppShell() {
  const [drawerOpen, setDrawerOpen] = useState(false);
  const location = useLocation();
  useKeyboardShortcuts();

  // Fecha automaticamente o drawer ao trocar de rota — UX padrão de mobile
  // nav: a navegação confirma a ação e o overlay some.
  useEffect(() => {
    setDrawerOpen(false);
  }, [location.pathname]);

  return (
    <div className="flex h-screen w-full overflow-hidden">
      {/* Sidebar desktop: sempre visível em ≥ lg */}
      <div className="hidden lg:flex">
        <Sidebar />
      </div>

      {/* Sidebar mobile: drawer absoluto + overlay quando aberto */}
      {drawerOpen && (
        <>
          <button
            type="button"
            aria-label="Fechar menu"
            className="fixed inset-0 z-40 bg-black/60 backdrop-blur-sm lg:hidden"
            onClick={() => setDrawerOpen(false)}
          />
          <div
            className={cn(
              "fixed left-0 top-0 z-50 h-full animate-fade-in lg:hidden",
            )}
          >
            <Sidebar />
            <Button
              variant="ghost"
              size="icon"
              className="absolute right-2 top-3 lg:hidden"
              onClick={() => setDrawerOpen(false)}
              aria-label="Fechar menu"
            >
              <X className="h-5 w-5" />
            </Button>
          </div>
        </>
      )}

      <div className="flex flex-1 flex-col overflow-hidden">
        <Header
          leftSlot={
            <Button
              variant="ghost"
              size="icon"
              className="lg:hidden"
              onClick={() => setDrawerOpen(true)}
              aria-label="Abrir menu"
            >
              <Menu className="h-5 w-5" />
            </Button>
          }
        />
        <main className="flex-1 overflow-y-auto px-4 py-6 sm:px-6 lg:px-8 lg:py-8 animate-fade-in">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
