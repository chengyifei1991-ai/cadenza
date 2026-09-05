import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// 开发期：Vite :5173 代理后端 :8080（同源联调，规避 CORS）。
// 构建期：产物默认输出 web/dist，经 scripts/embed.mjs 同步至 ../internal/webui/static，
// 由 go:embed 打入单二进制。
// manualChunks：路由级 React.lazy 之外，把 react/antd/query 拆为独立缓存友好包
// （评审 F-M1-5：首包由 ~721KB 拆分为可并行缓存多 chunk）。
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
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes("node_modules")) return undefined;
          if (id.includes("@tanstack")) return "query";
          if (id.includes("antd") || id.includes("@ant-design") || id.includes("/rc-")) return "antd";
          if (id.includes("react") || id.includes("scheduler")) return "react";
          return "vendor";
        },
      },
    },
  },
});
