import { useEffect } from "react";
import { activeToolbarRef } from "@/components/DataTableToolbar";

/**
 * useKeyboardShortcuts — listener global montado uma vez no AppShell.
 *
 * Atalhos:
 *   - Cmd/Ctrl + K  → foca a search da tela atual (DataTableToolbar.focus)
 *   - Esc           → blur do input ativo (cancela busca em foco)
 *
 * Não interfere quando o usuário está digitando em outro input/textarea, exceto
 * para Cmd+K que sobrescreve.
 */
export function useKeyboardShortcuts(): void {
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const meta = e.metaKey || e.ctrlKey;
      if (meta && (e.key === "k" || e.key === "K")) {
        e.preventDefault();
        activeToolbarRef.current?.focus();
        return;
      }
      if (e.key === "Escape") {
        const el = document.activeElement;
        if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) {
          el.blur();
        }
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
}
