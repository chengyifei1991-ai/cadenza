/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// 开发期：Vite :5173 代理后端 :8080（同源联调，规避 CORS）。
// 构建期：产物默认输出 web/dist，经 scripts/embed.mjs 同步至 ../internal/webui/static，
// 由 go:embed 打入单二进制。
// manualChunks：路由级 React.lazy 之外，把 react/antd/query 拆为独立缓存友好包。
// test：默认 node 环境跑纯逻辑单测；DOM 用例文件头标注 @vitest-environment jsdom。
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
    // V1 GA：回归单包构建（自定义 manualChunks 曾致浏览器端 React 互操作异常：
    // antd 读取 React 版本时得到 undefined）。后续再以手动验证过的方式安全拆包。
  },
  test: {
    setupFiles: ["./src/setupTests.ts"],
    environment: "node",
    globals: true,
    css: false,
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
