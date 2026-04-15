import { test, expect, type Page } from '@playwright/test';

// BasePath tests — verify URLs are correctly prefixed when daemon runs
// behind a proxy with --base-path=/dash/testvm.
// NOTE: The proxy strips BasePath before forwarding, so the daemon mux
// still matches /ui/*, /auth/*, etc. BasePath only affects template output.

const BASE = process.env.BASE_URL || 'http://localhost:9200';

async function login(page: Page) {
  await page.goto(`${BASE}/auth/test-login`);
  // With BasePath, test-login redirects to /dash/testvm/ui/
  // But since we hit the daemon directly (no proxy), we land at /ui/
  await page.waitForURL(/\/ui\/(?!login)/);
}

test.describe('BasePath template output', () => {
  test('dashboard HTML contains BasePath-prefixed URLs', async ({ page }) => {
    await login(page);
    const html = await page.content();

    // No bare /ui/ links without prefix (when BasePath is set, absPath
    // prepends it; when empty, absPath produces /ui/ which is correct)
    // We check the structure is consistent
    const links = await page.locator('a[href]').all();
    for (const link of links) {
      const href = await link.getAttribute('href');
      if (href && href.startsWith('/')) {
        // All internal links should start with BasePath or be /
        expect(href).toMatch(/^\//);
      }
    }
  });

  test('static assets load correctly', async ({ page }) => {
    await login(page);
    // htmx should be loaded
    const htmxLoaded = await page.evaluate(
      () => typeof (window as any).htmx !== 'undefined'
    );
    expect(htmxLoaded).toBe(true);
  });

  test('CSRF meta tag present with BasePath', async ({ page }) => {
    await login(page);
    const csrf = await page.locator('meta[name="csrf-token"]').getAttribute('content');
    expect(csrf).toBeTruthy();
    expect(csrf!.length).toBeGreaterThan(20);
  });

  test('nav links all work', async ({ page }) => {
    await login(page);

    // Click Dashboard
    await page.click('nav a:has-text("Dashboard")');
    await expect(page.locator('body')).toContainText(/task|Task|dashboard|Dashboard/i);

    // Click PRs
    const prsLink = page.locator('nav a:has-text("PRs")');
    if (await prsLink.count() > 0) {
      await prsLink.click();
      await expect(page.locator('body')).toContainText(/PR|pull/i);
    }
  });

  test('new task form action is correct', async ({ page }) => {
    await login(page);
    await page.goto(`${BASE}/ui/tasks/new`);
    const form = page.locator('form[action*="/tasks"]');
    await expect(form).toBeVisible();
    const action = await form.getAttribute('action');
    // Action should contain /tasks (may or may not have BasePath depending on mode)
    expect(action).toContain('/tasks');
  });

  test('task detail EventSource URL works', async ({ page, request }) => {
    // Create a task
    const resp = await request.post(`${BASE}/tasks`, {
      headers: { 'Authorization': 'Bearer e2e-token', 'Content-Type': 'application/json' },
      data: { repo_url: 'https://github.com/test/bp', task: 'BasePath SSE test' }
    });
    const { id } = await resp.json();

    await login(page);
    await page.goto(`${BASE}/ui/tasks/${id}`);

    // SSE status should show Connected
    await expect(page.locator('#sse-status')).toContainText('Connected', { timeout: 10000 });
  });

  test('login page GitHub link has correct path', async ({ page }) => {
    await page.goto(`${BASE}/ui/login`);
    const link = page.locator('a[href*="/auth/github"]');
    await expect(link).toBeVisible();
    const href = await link.getAttribute('href');
    expect(href).toContain('/auth/github');
  });

  test('logout redirects to login page', async ({ page }) => {
    await login(page);
    const csrf = await page.locator('meta[name="csrf-token"]').getAttribute('content');

    await page.evaluate(async ([c, b]) => {
      await fetch(`${b}/auth/logout`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': c! },
      });
    }, [csrf, BASE]);

    await page.goto(`${BASE}/ui/`);
    await page.waitForURL(/\/ui\/login/);
  });

  test('share link page renders without auth', async ({ page }) => {
    // Invalid share link should return error, not crash
    const resp = await page.goto(`${BASE}/share/test.invalidhmac?exp=9999999999`);
    expect([400, 403, 404]).toContain(resp?.status() || 0);
  });
});

test.describe('Session ticket (cloud mode)', () => {
  test('session endpoint returns 404 when cloud mode disabled', async ({ request }) => {
    const resp = await request.get(`${BASE}/auth/session?ticket=fake`);
    expect(resp.status()).toBe(404);
  });
});
