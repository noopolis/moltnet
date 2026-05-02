import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  // Console is served from /console/ both in production and in dev so the
  // SPA's URL routes (/console/room/:id, /console/dm/:id, /console/events)
  // resolve relative asset paths consistently.
  base: "/console/",
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: false,
    target: "es2022",
  },
  server: {
    proxy: {
      "/v1": "http://127.0.0.1:8787",
    },
  },
});
