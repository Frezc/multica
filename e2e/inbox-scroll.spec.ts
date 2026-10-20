import { expect, test } from "@playwright/test";
import { writeFile } from "node:fs/promises";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("inbox detail scrolling", () => {
  let api: TestApiClient;

  test.beforeEach(async ({ baseURL }) => {
    const local = (url: string) =>
      ["localhost", "127.0.0.1", "[::1]"].includes(new URL(url).hostname);
    test.skip(
      !local(baseURL!) || !local(process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080"),
      "Local fixtures only",
    );
    api = await createTestApi();
  });

  test.afterEach(async () => {
    await api?.cleanup();
  });

  for (const width of [1440, 1052, 851]) {
    test(`keeps the detail header fixed at ${width}px`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width: 1440, height: 720 });
      const workspaceSlug = await loginAsDefault(page);
      const height = width >= 1024 ? 360 : 720;
      await page.setViewportSize({ width, height });
      await page.evaluate(() => {
        localStorage.setItem("react-resizable-panels:multica_inbox_issue_detail_layout", JSON.stringify({ content: 65, sidebar: 35 }));
      });
      const issue = await api.createIssue("Inbox scroll regression", {
        description: Array.from({ length: 40 }, (_, i) =>
          `Paragraph ${i + 1}: Long task content must scroll within the detail pane.`,
        ).join("\n\n"),
      });
      const comment = await api.createComment(issue.id, "Inbox notification target comment");
      // The execution log's off-screen accessibility labels used to escape
      // the sidebar's clipping and make the outer detail panel scrollable.
      // An empty execution log does not reproduce that overflow.
      await page.route(`**/api/issues/${issue.id}/task-runs`, (route) => route.fulfill({
        json: Array.from({ length: 3 }, (_, i) => ({
          id: `00000000-0000-4000-8000-00000000001${i}`,
          issue_id: issue.id,
          status: "completed",
          trigger_summary: `Completed scroll regression run ${i + 1}`,
          created_at: "2026-10-19T10:00:00Z",
          started_at: "2026-10-19T10:00:00Z",
          completed_at: "2026-10-19T10:01:00Z",
          usage: [{
            provider: "test", model: "test-model", input_tokens: 1000,
            output_tokens: 100, cost_usd_ticks: 1000000,
          }],
        })),
      }));
      // Supply a read notification without depending on another user's actions.
      await page.route(/\/api\/inbox$/, (route) => route.fulfill({
        json: [{
          id: "00000000-0000-4000-8000-000000000001",
          workspace_id: issue.workspace_id,
          recipient_type: "member", recipient_id: issue.creator_id,
          actor_type: "member", actor_id: issue.creator_id,
          type: "issue_assigned", severity: "info", issue_id: issue.id,
          title: issue.title, body: null, issue_status: issue.status,
          read: true, archived: false, details: { comment_id: comment.id }, created_at: issue.created_at,
        }],
      }));
      await page.goto(`/${workspaceSlug}/inbox?issue=${issue.id}`, {
        waitUntil: "domcontentloaded",
      });
      const scroller = page.locator(`[data-tab-scroll-root="main:${issue.id}"]`);
      await expect(scroller.getByText("Paragraph 40:", { exact: false })).toBeAttached();
      if (width >= 1024) {
        await expect(page.locator("[data-right-sidebar-panel]")).toBeInViewport();
        await expect(page.getByText("Completed scroll regression run 3", { exact: true })).toBeAttached();
      }
      const header = page.getByRole("link", { name: `${issue.identifier} ${issue.title}`, exact: true });
      await expect(header).toBeInViewport();
      const before = await header.boundingBox();
      const geometry = await scroller.evaluate((element) => {
        const ancestors = [];
        for (let current: HTMLElement | null = element; current; current = current.parentElement) {
          ancestors.push({
            tag: current.tagName, class: current.className,
            height: current.clientHeight, contentHeight: current.scrollHeight,
            overflow: getComputedStyle(current).overflowY,
          });
        }
        return ancestors;
      });
      await testInfo.attach("scroll-geometry", {
        body: JSON.stringify(geometry, null, 2), contentType: "application/json",
      });
      expect(geometry[0]!.height).toBeLessThan(height);
      expect(geometry[0]!.contentHeight).toBeGreaterThan(geometry[0]!.height);
      await scroller.hover();
      await page.mouse.wheel(0, 900);
      await expect.poll(() => scroller.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
      await scroller.evaluate((element) => { element.scrollTop = element.scrollHeight; });
      await page.mouse.wheel(0, 900);
      await page.waitForTimeout(300);
      const afterWheel = testInfo.outputPath("after-body-end-wheel.png");
      await page.screenshot({ path: afterWheel });
      await testInfo.attach("after-body-end-wheel", {
        path: afterWheel, contentType: "image/png",
      });
      const scrollState = await scroller.evaluate((element) => {
        const chain = [];
        for (let current: HTMLElement | null = element; current; current = current.parentElement) {
          chain.push({
            id: current.id, class: current.className, top: current.scrollTop,
            height: current.clientHeight, content: current.scrollHeight,
            y: current.getBoundingClientRect().y, overflow: getComputedStyle(current).overflowY,
          });
        }
        return chain;
      });
      await writeFile(testInfo.outputPath("after-body-end-wheel.json"), JSON.stringify(scrollState, null, 2));
      await expect(header).toBeInViewport();
      expect((await header.boundingBox())!.y).toBe(before!.y);
      expect(await scroller.evaluate((element) => {
        for (let parent = element.parentElement; parent; parent = parent.parentElement) {
          if (parent.scrollTop !== 0) return false;
        }
        return true;
      })).toBe(true);
      if (width >= 1024) {
        const sidebar = page.locator("[data-right-sidebar-panel] .overflow-y-auto");
        expect(await sidebar.evaluate((element) => element.scrollHeight - element.clientHeight)).toBeGreaterThan(0);
        const bodyScrollTop = await scroller.evaluate((element) => element.scrollTop);
        await sidebar.hover();
        await page.mouse.wheel(0, 900);
        await expect.poll(() => sidebar.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
        expect(await scroller.evaluate((element) => element.scrollTop)).toBe(bodyScrollTop);
        await expect(header).toBeInViewport();
        expect((await header.boundingBox())!.y).toBe(before!.y);
      }
      const screenshot = testInfo.outputPath("inbox-scrolled.png");
      await page.screenshot({ path: screenshot });
      await testInfo.attach("inbox-scrolled", { path: screenshot, contentType: "image/png" });
    });
  }

  test("keeps a long notification without an issue scrollable", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 720 });
    const workspaceSlug = await loginAsDefault(page);
    const notificationId = "00000000-0000-4000-8000-000000000002";
    const body = Array.from({ length: 80 }, (_, i) => `Notification line ${i + 1}`).join("\n");
    await page.route(/\/api\/inbox$/, (route) => route.fulfill({
      json: [{
        id: notificationId, workspace_id: "00000000-0000-4000-8000-000000000003",
        recipient_type: "member", recipient_id: "00000000-0000-4000-8000-000000000004",
        actor_type: "system", actor_id: null, type: "quick_create_unconfirmed",
        severity: "info", issue_id: null, title: "Long notification", body,
        issue_status: null, read: true, archived: false, details: null,
        created_at: "2026-10-20T00:00:00Z",
      }],
    }));
    await page.goto(`/${workspaceSlug}/inbox`, { waitUntil: "domcontentloaded" });
    await page.getByText("Long notification", { exact: true }).click();
    const content = page.locator('[data-testid="detail"]').getByText(body, { exact: true });
    await expect(content).toBeAttached();
    const geometry = await content.evaluate((element) => {
      for (let parent = element.parentElement; parent; parent = parent.parentElement) {
        if (getComputedStyle(parent).overflowY !== "auto") continue;
        parent.scrollTop = parent.scrollHeight;
        return { height: parent.clientHeight, contentHeight: parent.scrollHeight, top: parent.scrollTop };
      }
      return null;
    });
    expect(geometry).not.toBeNull();
    expect(geometry!.height).toBeLessThan(720);
    expect(geometry!.top).toBeGreaterThan(0);
    await expect(page.getByRole("button", { name: "Edit as advanced form" })).toBeInViewport();
  });
});
