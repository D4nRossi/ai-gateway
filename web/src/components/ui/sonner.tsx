import { Toaster as SonnerToaster, toast } from "sonner";

/**
 * Toaster — wraps sonner with the dark-theme tokens.
 * Mount once near the root of the app (in App.tsx).
 */
export function Toaster() {
  return (
    <SonnerToaster
      theme="dark"
      position="top-right"
      richColors
      closeButton
      toastOptions={{
        style: {
          background: "hsl(var(--card))",
          border: "1px solid hsl(var(--border))",
          color: "hsl(var(--foreground))",
        },
      }}
    />
  );
}

export { toast };
