import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// 开发期：Vite :5173 代理后端 :8080（同源联调，规避 CORS）。
// 构建期：产物默认输出 web/dist，经 scripts/embed.mjs 同步至 ../internal/webui/static，
// 由 go:embed 打入单二进制。
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": { target: "http://127.0.0.1:8080", changeOrigin: true },
      "/healthz": { target: "http://127.0.0.1:8080", changeOrigin: true },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
