// 将 web/dist 构建产物同步至 ../internal/webui/static（go:embed 源）。
// 用法：pnpm build && pnpm embed（或 node scripts/embed.mjs）。
import { cpSync, existsSync, mkdirSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const dist = join(root, "dist");
const target = join(root, "..", "internal", "webui", "static");

if (!existsSync(dist)) {
  console.error("web/dist 不存在，请先执行 pnpm build");
  process.exit(1);
}
rmSync(target, { recursive: true, force: true });
mkdirSync(target, { recursive: true });
cpSync(dist, target, { recursive: true });
console.log(`已同步 web/dist → internal/webui/static（${target}）`);
