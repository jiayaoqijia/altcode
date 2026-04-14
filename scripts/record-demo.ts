/**
 * Playwright video demo recorder for AltFix web UI.
 *
 * Records a ~30-second walkthrough of the key dashboard features:
 *   1. Login via test-login bypass
 *   2. Dashboard with task list and KPI cards
 *   3. Task detail page with SSE activity feed
 *   4. New task creation form
 *   5. PR tracker page
 *   6. Settings page
 *
 * Prerequisites:
 *   - Daemon running on PORT (default 9100) with --test-mode
 *   - Sample tasks created via POST /tasks
 *   - Playwright chromium browser installed
 *
 * Usage (from repo root):
 *   E2E=internal/daemon/web/tests/e2e
 *   NODE_PATH=$E2E/node_modules npx tsx scripts/record-demo.ts
 *
 * Output:
 *   /tmp/altfix-demo.webm
 */

import { chromium } from 'playwright';
import { copyFileSync, mkdirSync } from 'fs';

const PORT = process.env.ALTFIX_PORT || '9100';
const BASE = `http://localhost:${PORT}`;
const VIDEO_DIR = '/tmp/demo-videos';
const OUTPUT = '/tmp/altfix-demo.webm';

async function main() {
  mkdirSync(VIDEO_DIR, { recursive: true });

  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({
    recordVideo: {
      dir: VIDEO_DIR,
      size: { width: 1280, height: 720 },
    },
    viewport: { width: 1280, height: 720 },
  });
  const page = await context.newPage();

  // --- 1. Login via test-login bypass ---
  console.log('1/6 Login...');
  await page.goto(`${BASE}/auth/test-login`);
  await page.waitForURL(/\/ui\/(?!login)/);
  // Pause to show the dashboard landing.
  await page.waitForTimeout(2000);

  // --- 2. Dashboard overview ---
  console.log('2/6 Dashboard...');
  // Wait for htmx to populate the task list (polls every 5s; the
  // initial render is server-side so it should already be there).
  await page.waitForSelector('#task-list', { timeout: 8000 });
  await page.waitForTimeout(3000);

  // Slowly scroll through the task list for visual effect.
  await page.mouse.wheel(0, 150);
  await page.waitForTimeout(800);
  await page.mouse.wheel(0, 150);
  await page.waitForTimeout(1500);

  // Click the "Active" status filter tab.
  const activeTab = page.locator('main a[href="/ui/?status=active"]');
  if (await activeTab.count() > 0) {
    await activeTab.click();
    await page.waitForTimeout(1500);
  }

  // Back to "All".
  const allTab = page.locator('main a[href="/ui/"]');
  if (await allTab.count() > 0) {
    await allTab.click();
    await page.waitForTimeout(1500);
  }

  // --- 3. Task detail ---
  console.log('3/6 Task detail...');
  const firstTask = page.locator('a[href*="/ui/tasks/"]').first();
  if (await firstTask.count() > 0) {
    await firstTask.click();
    // Let SSE connection indicator and activity feed render.
    await page.waitForTimeout(4000);
  }

  // --- 4. New task form ---
  console.log('4/6 New task form...');
  // Navigate back to dashboard via nav link.
  await page.locator('nav a[href="/ui/"]').first().click();
  await page.waitForTimeout(1000);

  // Click "New Task" button.
  await page.locator('a[href="/ui/tasks/new"]').click();
  await page.waitForTimeout(1000);

  // Type a task description slowly for demo effect.
  const textarea = page.locator('textarea[name="description"]');
  if (await textarea.count() > 0) {
    await textarea.click();
    await textarea.type(
      'Implement JWT authentication with refresh tokens',
      { delay: 40 },
    );
    await page.waitForTimeout(800);

    // Open "Advanced Options" section.
    const details = page.locator('details summary');
    if (await details.count() > 0) {
      await details.click();
      await page.waitForTimeout(1200);
    }
  }

  // --- 5. PR tracker ---
  console.log('5/6 PR tracker...');
  const prsNav = page.locator('nav a[href="/ui/prs"]');
  if (await prsNav.count() > 0) {
    await prsNav.click();
    await page.waitForTimeout(2000);
  }

  // --- 6. Settings ---
  console.log('6/6 Settings...');
  const settingsNav = page.locator('nav a[href="/ui/settings"]');
  if (await settingsNav.count() > 0) {
    await settingsNav.click();
    await page.waitForTimeout(2000);
  }

  // Final shot: back to dashboard.
  await page.locator('nav a[href="/ui/"]').first().click();
  await page.waitForTimeout(2000);

  // Close context to flush the video file.
  const videoPath = await page.video()?.path();
  await context.close();
  await browser.close();

  if (videoPath) {
    copyFileSync(videoPath, OUTPUT);
    console.log(`\nDemo video saved: ${OUTPUT}`);
  } else {
    console.error('ERROR: no video path returned by Playwright');
    process.exit(1);
  }
}

main().catch((err) => {
  console.error('Recording failed:', err);
  process.exit(1);
});
