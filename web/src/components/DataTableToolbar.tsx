import { forwardRef, useEffect, useImperativeHandle, useRef } from "react";
import { Loader2, RefreshCw, Search } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

/**
 * DataTableToolbar — barra superior padrão para qualquer listagem.
 *
 * Padrões:
 *   - Search inline (filter client-side; backend pagination fica para Lote C)
 *   - Botão Atualizar com indicador de loading
 *   - Slot para ações extras (botão "Novo X", filtros adicionais)
 *   - Hint Cmd+K — listener global em useKeyboardShortcuts foca o input
 *
 * O foco é controlado externamente via ref imperativa (`focus()`) para que o
 * atalho global de Cmd+K funcione independente de qual toolbar está montada.
 */
export interface DataTableToolbarHandle {
  focus: () => void;
}

interface Props {
  search: string;
  onSearchChange: (v: string) => void;
  onRefresh: () => void;
  refreshing?: boolean;
  placeholder?: string;
  rightSlot?: React.ReactNode;
  className?: string;
}

export const DataTableToolbar = forwardRef<DataTableToolbarHandle, Props>(
  function DataTableToolbar(
    { search, onSearchChange, onRefresh, refreshing, placeholder, rightSlot, className },
    ref,
  ) {
    const inputRef = useRef<HTMLInputElement>(null);

    useImperativeHandle(ref, () => ({
      focus: () => inputRef.current?.focus(),
    }));

    // Register this toolbar as the "active search" on mount; the global
    // Cmd+K listener focuses whichever toolbar is currently mounted.
    useEffect(() => {
      activeToolbarRef.current = inputRef.current;
      return () => {
        if (activeToolbarRef.current === inputRef.current) {
          activeToolbarRef.current = null;
        }
      };
    }, []);

    return (
      <div className={cn("flex flex-wrap items-center gap-3", className)}>
        <div className="relative flex-1 min-w-[240px] max-w-md">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            ref={inputRef}
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder={placeholder ?? "Buscar…"}
            className="pl-9 pr-12"
            aria-label="Buscar"
          />
          <kbd className="pointer-events-none absolute right-3 top-1/2 hidden -translate-y-1/2 select-none items-center gap-0.5 rounded border border-border bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground sm:inline-flex">
            ⌘K
          </kbd>
        </div>
        <Button
          variant="outline"
          size="icon"
          onClick={onRefresh}
          disabled={refreshing}
          title="Atualizar"
          aria-label="Atualizar"
        >
          {refreshing ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <RefreshCw className="h-4 w-4" />
          )}
        </Button>
        {rightSlot && <div className="ml-auto flex items-center gap-2">{rightSlot}</div>}
      </div>
    );
  },
);

// Singleton ref used by the global Cmd+K shortcut to know which input to focus.
// Components that should respond to Cmd+K just need to mount a DataTableToolbar
// (or call activeToolbarRef.current?.focus() directly if they have a custom input).
export const activeToolbarRef = { current: null as HTMLInputElement | null };
