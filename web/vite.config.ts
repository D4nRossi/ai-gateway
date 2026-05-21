import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";

// Vite configuration for the AI Gateway admin web UI.
//
// `base: "/ui/"` makes every emitted asset URL relative to /ui/, matching the
// chi mount point in the Go router. The dev server proxies admin API calls
// straight through to the Go backend on :8080 so we never have CORS issues.
//
// References:
//   - ADR-0014 — frontend embedded in Go binary
//   - https://vitejs.dev/config/
export default defineConfig({
  base: "/ui/",
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/admin": {
        target: "http://localhost:8080",
        changeOrigin: false,
      },
      "/v1": {
        target: "http://localhost:8080",
        changeOrigin: false,
      },
      "/healthz": "http://localhost:8080",
      "/readyz": "http://localhost:8080",
    },
  },
  build: {
    outDir: "dist",
    // Source maps inline-pelo-bundler: stacks de erro no browser apontam para
    // arquivos fonte (.tsx) em vez de offsets minificados. Custo: ~1 MB extra
    // no dist (não vai pra produção do binário se você quiser remover —
    // basta trocar para false antes de buildar a imagem final).
    sourcemap: true,
    target: "es2022",
    chunkSizeWarningLimit: 1000,
  },
});
