// V1 GA UI 冒烟（Playwright）：真实浏览器跑通核心闭环。
// 依赖：先构建 bin/cadenza 与 bin/cadenza-passwd，并 npm --prefix web ci。
import { defineConfig } from "@playwright/test";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const REPO = resolve(HERE, "..", "..");
const BIN = `${REPO}/bin`;
const PORT = 18770;
const BASE = `http://127.0.0.1:${PORT}`;

export default defineConfig({
  testDir: "./specs",
  timeout: 90_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  reporter: [["list"]],
  use: {
    baseURL: BASE,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    locale: "zh-CN",
  },
  webServer: {
    // 以登录保护 + 演示库模式启动被测单二进制（复用 E2E 的口令生成工具）。
    command: `bash -lc 'PW=$(printf %s Admin@12345 | ${BIN}/cadenza-passwd); rm -f /tmp/cadenza-ui-e2e.db; DB_DRIVER=sqlite DB_SQLITE_PATH=/tmp/cadenza-ui-e2e.db HTTP_ADDR=127.0.0.1:${PORT} DEMO_MODE=true WEB_AUTH_MODE=simple WEB_ADMIN_USER=admin WEB_ADMIN_PASSWORD_HASH=$PW LLM_API_KEY=not-used REQUIRE_APPROVAL=true ${BIN}/cadenza > /tmp/cadenza-ui-e2e.log 2>&1'`,
    url: `${BASE}/healthz`,
    reuseExistingServer: false,
    timeout: 60_000,
  },
});
