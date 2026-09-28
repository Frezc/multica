import "./env";
import { test, expect } from "@playwright/test";
import pg from "pg";
import { createTestApi } from "./helpers";

test("switch workspace from an open chat", async ({ page }) => {
  test.setTimeout(180_000);
  const api = await createTestApi();
  const db = new pg.Client(process.env.DATABASE_URL);
  await db.connect();
  const source = (await api.getWorkspaces())[0]!;
  await db.query("UPDATE workspace SET name = $1 WHERE id = $2", ["Workspace switch source", source.id]);
  source.name = "Workspace switch source";
  const { rows: users } = await db.query('SELECT id FROM "user" WHERE email = $1', [api.getEmail()]);
  const userId = users[0].id;
  const targetSlug = `switch-target-${Date.now()}`;
  let targetId: string | null = null;
  let runtimeId: string | null = null;
  let agentId: string | null = null;
  let sessionId: string | null = null;
  try {
    const target = await db.query(
      "INSERT INTO workspace (name, slug) VALUES ($1, $2) RETURNING id",
      ["Workspace switch target", targetSlug],
    );
    targetId = target.rows[0].id;
    await db.query("INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')", [targetId, userId]);
    const runtime = await db.query(
      `INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
       VALUES ($1, 'Switch regression', 'cloud', 'e2e_switch', 'online', 'Switch regression', '{}'::jsonb, now()) RETURNING id`,
      [source.id],
    );
    runtimeId = runtime.rows[0].id;
    const agent = await db.query(
      `INSERT INTO agent (workspace_id, name, description, instructions, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
       VALUES ($1, 'Switch regression agent', '', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3) RETURNING id`,
      [source.id, runtimeId, userId],
    );
    agentId = agent.rows[0].id;
    const session = await db.query(
      `INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status, explicitly_created_at)
       VALUES ($1, $2, $3, 'Workspace switch conversation', 'active', now()) RETURNING id`,
      [source.id, agentId, userId],
    );
    sessionId = session.rows[0].id;
    await page.addInitScript((token) => {
      localStorage.setItem("multica_token", token!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, api.getToken());

    for (let i = 0; i < Number(process.env.SWITCH_REPRO_COUNT ?? 1); i++) {
      await page.goto(`/${source.slug}/chat?session=${sessionId}`, { waitUntil: "domcontentloaded" });
      await expect(page.getByText("Workspace switch conversation").first()).toBeVisible({ timeout: 45_000 });
      await page.getByRole("button", { name: source.name }).first().click();
      await page.getByRole("menuitem", { name: "Workspace switch target" }).click();
      try {
        await expect(page).toHaveURL(new RegExp(`/${targetSlug}/issues$`), { timeout: 20_000 });
        await expect(page.getByRole("heading", { name: "Issues", exact: true })).toBeVisible();
      } finally {
        console.log(`Workspace switch attempt ${i + 1}: ${page.url()}`);
        if (process.env.SWITCH_REPRO_SCREENSHOT) {
          await page.screenshot({ path: process.env.SWITCH_REPRO_SCREENSHOT, fullPage: true });
        }
      }
    }
  } finally {
    if (sessionId) await db.query("DELETE FROM chat_session WHERE id = $1", [sessionId]);
    if (agentId) await db.query("DELETE FROM agent WHERE id = $1", [agentId]);
    if (runtimeId) await db.query("DELETE FROM agent_runtime WHERE id = $1", [runtimeId]);
    if (targetId) {
      await db.query("DELETE FROM member WHERE workspace_id = $1", [targetId]);
      await db.query("DELETE FROM workspace WHERE id = $1", [targetId]);
    }
    await db.end();
    await api.cleanup();
  }
});
