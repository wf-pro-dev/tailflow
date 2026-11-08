import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { TanStackRouterVite } from "@tanstack/router-plugin/vite";
import path from "node:path";

export default defineConfig({
  plugins: [
    TanStackRouterVite({ target: "react", autoCodeSplitting: true }),
    react(),
  ],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8080",
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: "dist",
    sourcemap: true,
    // No manualChunks: let Vite handle code splitting. Manually splitting
    // react and react-dom from @tanstack/* creates a circular chunk graph
    // (tanstack depends on react, react depends on nothing) that breaks
    // runtime loading — react-dom sees `undefined` for react's internals.
    rollupOptions: {
      output: {
        // Let Vite handle code splitting automatically.
      },
    },
  },
});
