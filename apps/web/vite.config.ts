import { defineConfig } from "vitest/config";
import { localPdfAssets } from "./pdf-assets.js";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath } from "node:url";

// In development the API runs on :8080; Vite proxies the same paths Caddy routes in production.
const api = process.env.VITE_API_PROXY ?? "http://localhost:8080";

export default defineConfig({
  plugins: [react(), tailwindcss(), localPdfAssets()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  server: {
    port: 5173,
    proxy: {
      "/api": api,
      "/health": api,
      "/openapi.json": api,
      "/docs": api,
      "/ws": { target: api, ws: true },
      "/media": { target: api, ws: true },
    },
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes("node_modules/hls.js")) return "hls";
          if (id.includes("node_modules/@dnd-kit")) return "dnd-kit";
        },
      },
    },
  },
  worker: {
    // MapLibre's worker config URL drives `new Worker(url, { type: "module" })`, so the
    // bundled worker must stay ESM.
    format: "es",
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test-setup.ts"],
  },
});
