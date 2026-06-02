import { Component, type ErrorInfo, type ReactNode } from "react";
import { AlertTriangle, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";

interface Props {
  children: ReactNode;
}

interface State {
  error: Error | null;
}

/**
 * ErrorBoundary — captura erros de render dos descendentes e renderiza um
 * fallback. Sem isso, qualquer throw no render destrói toda a árvore React e
 * o usuário vê apenas o body vazio (tela preta sem feedback).
 *
 * Estratégia:
 *   - Mostra o erro + stack + botão "Tentar novamente" (reseta state)
 *   - Loga no console com prefixo distintivo pra facilitar inspeção
 *   - Não captura erros assíncronos (fetch errors, setTimeout) — esses
 *     continuam fluindo pra o toast normal via api.ts
 *
 * References:
 *   - https://react.dev/reference/react/Component#catching-rendering-errors-with-an-error-boundary
 */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    // eslint-disable-next-line no-console
    console.error("[AI Gateway UI] render error caught by ErrorBoundary:", error, info.componentStack);
  }

  reset = (): void => {
    this.setState({ error: null });
  };

  reload = (): void => {
    window.location.reload();
  };

  render() {
    if (!this.state.error) return this.props.children;

    return (
      <div className="flex min-h-screen items-center justify-center px-4">
        <div className="max-w-lg space-y-4 rounded-lg border border-destructive/30 bg-card/80 p-6 shadow-xl backdrop-blur-sm">
          <div className="flex items-center gap-3">
            <AlertTriangle className="h-6 w-6 text-destructive" />
            <h1 className="text-lg font-semibold">Algo quebrou</h1>
          </div>
          <p className="text-sm text-muted-foreground">
            O console encontrou um erro inesperado. A causa está abaixo — copie
            pro relato de bug:
          </p>
          <pre className="max-h-40 overflow-auto rounded-md border border-border bg-background/60 p-3 font-mono text-[11px] text-foreground/90">
            {this.state.error.message}
            {"\n\n"}
            {this.state.error.stack}
          </pre>
          <div className="flex gap-2">
            <Button variant="outline" onClick={this.reset}>
              <RotateCcw className="h-4 w-4" />
              Tentar de novo
            </Button>
            <Button onClick={this.reload}>Recarregar a página</Button>
          </div>
        </div>
      </div>
    );
  }
}
