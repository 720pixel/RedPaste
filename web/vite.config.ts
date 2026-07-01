import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Builds the RedPaste SPA into ./dist, embedded into the Go binary. Assets are
// hashed and served same-origin.
export default defineConfig({
  plugins: [react()],
  resolve: {
    // Avoids needing @types/node just for a path helper.
    alias: { "@": new URL("./src", import.meta.url).pathname },
  },
  build: {
    target: "es2022",
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: false,
    // No inlined preload-polyfill script, so CSP can stay script-src 'self'.
    modulePreload: { polyfill: false },
  },
  server: {
    proxy: {
      "/api": "http://localhost:8080",
      "/healthz": "http://localhost:8080",
    },
  },
});
