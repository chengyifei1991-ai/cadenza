// V1 GA UI 冒烟：登录 → 仪表盘 → Collectors → 配置编辑 → 审批闭环 → AI 助手。
import { expect, test } from "@playwright/test";

const ADMIN = { username: "admin", password: "Admin@12345" };

async function login(page: import("@playwright/test").Page) {
  await page.goto("/");
  await expect(page).toHaveURL(/\/login/); // 登录保护重定向
  await page.getByPlaceholder("用户名").fill(ADMIN.username);
  await page.getByPlaceholder("密码").fill(ADMIN.password);
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page.getByRole("heading", { name: "仪表盘" })).toBeVisible();
  // 关闭首次引导弹窗（避免遮挡后续点击；各用例独立浏览器上下文）
  const closeBtn = page.getByRole("button", { name: /关闭（不再显示）/ });
  if (await closeBtn.isVisible().catch(() => false)) {
    await closeBtn.click();
    await expect(page.getByRole("dialog")).toBeHidden();
  }
}

test("登录保护：未登录被重定向，错误口令报错，正确登录进入仪表盘", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/login/);
  await page.getByPlaceholder("用户名").fill(ADMIN.username);
  await page.getByPlaceholder("密码").fill("wrong-pass");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page.getByText("用户名或密码错误")).toBeVisible();
  await page.getByPlaceholder("密码").fill(ADMIN.password);
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page.getByRole("heading", { name: "仪表盘" })).toBeVisible();
});

test("仪表盘（演示模式）：统计与待审批提醒可见，引导可关闭", async ({ page }) => {
  await login(page);
  await expect(page.getByText("演示数据模式（DEMO_MODE=true）")).toBeVisible();
  await expect(page.getByText("Collector 总数")).toBeVisible();
  await expect(page.getByText(/个任务待审批/)).toBeVisible();
  // Onboarding 首次弹窗 → 关闭并标记完成
  await expect(page.getByRole("dialog").getByText("欢迎使用 Cadenza")).toBeVisible();
  await page.getByRole("button", { name: /关闭（不再显示）/ }).click();
  // 最近任务速览含演示任务
  await expect(page.getByText(/memory_limiter（演示，等待审批）|增加内存限制/).first()).toBeVisible();
});

test("Collectors → 详情 → 版本历史（回滚按钮/当前禁用）", async ({ page }) => {
  await login(page);
  await page.getByRole("menuitem", { name: /Collectors/ }).click();
  await page.getByRole("link", { name: "demo-gateway-1" }).first().click();
  await expect(page.getByRole("heading", { name: "demo-gateway-1" })).toBeVisible();
  await page.getByRole("tab", { name: /版本历史/ }).click();
  await expect(page.getByRole("button", { name: /当前/ }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: /回滚到本版本/ }).first()).toBeVisible();
});

test("配置编辑器 → 保存并下发 → 审批闭环（done）", async ({ page }) => {
  await login(page);
  await page.getByRole("menuitem", { name: /Collectors/ }).click();
  await page.getByRole("link", { name: "demo-gateway-1" }).first().click();
  await page.getByRole("button", { name: "编辑配置" }).click();
  const editor = page.getByRole("textbox", { name: "Collector YAML 配置" });
  await expect(editor).toHaveValue(/exporters:/);
  // 语法预检（无需修改即可通过）
  await page.getByRole("button", { name: "语法预检" }).click();
  await expect(page.getByText(/YAML 语法正确/)).toBeVisible();
  // 保存并下发 → 任务详情（待审批）
  await page.getByRole("button", { name: "保存并下发" }).click();
  await expect(page.getByText("任务已创建")).toBeVisible();
  await expect(page.getByRole("button", { name: /审批通过并下发/ })).toBeVisible();
  // 审批 → done
  await page.getByRole("button", { name: /审批通过并下发/ }).click();
  await expect(page.getByText("已完成").first()).toBeVisible({ timeout: 20_000 });
  await expect(page.getByRole("heading", { name: /^任务 / })).toBeVisible();
});

test("AI 助手：演示会话历史可回读", async ({ page }) => {
  await login(page);
  await page.getByRole("menuitem", { name: /AI 助手/ }).click();
  await expect(page.getByText(/帮我给 demo-gateway-1 增加内存限制/)).toBeVisible();
  await page.getByText(/帮我给 demo-gateway-1 增加内存限制/).click();
  await expect(page.getByText(/已生成带 memory_limiter 的配置/)).toBeVisible();
});
