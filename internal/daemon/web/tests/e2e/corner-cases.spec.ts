import { test, expect, type Page } from '@playwright/test';

const BASE = process.env.BASE_URL || 'http://localhost:9100';
const API_TOKEN = 'e2e-token';
const apiHeaders = {
  Authorization: `Bearer ${API_TOKEN}`,
  'Content-Type': 'application/json',
};

async function login(page: Page) {
  await page.goto(`${BASE}/auth/test-login`);
  await page.waitForURL(/\/ui\/(?!login)/);
}

// ---- Group 1: Real user journeys (operate UI like a user) ----

test.describe('Real user journeys', () => {
  test('user logs in, sees dashboard, clicks New Task, fills form, goes back', async ({
    page,
  }) => {
    // 1. Login
    await page.goto(`${BASE}/auth/test-login`);
    await page.waitForURL(/\/ui\/(?!login)/);

    // 2. Verify dashboard loaded
    await expect(page.locator('nav')).toBeVisible();
    await expect(page.locator('nav')).toContainText('AltFix');

    // 3. Click "New Task" button/link
    await page.click('a[href="/ui/tasks/new"]');
    await page.waitForURL(/tasks\/new/);

    // 4. Fill the form
    await page.fill(
      'textarea[name="description"]',
      'User journey: fix the login bug',
    );

    // 5. Verify form has submit button (scoped to task form, not nav logout)
    const taskForm = page.locator('form[action="/api/tasks"]');
    const submitBtn = taskForm.locator('button[type="submit"]');
    await expect(submitBtn).toBeVisible();

    // 6. Click Dashboard in nav to go back
    await page.click('nav a[href="/ui/"]');
    await page.waitForURL(/\/ui\/$/);
  });

  test('user navigates through all pages via nav links', async ({ page }) => {
    await login(page);

    // Dashboard
    await expect(page).toHaveURL(/\/ui\//);

    // Click PRs link in nav
    const prsLink = page.locator('nav a').filter({ hasText: 'PRs' });
    await prsLink.click();
    await expect(page.locator('body')).toContainText(/PR|pull/i);

    // Click Settings (admin)
    const settingsLink = page.locator('nav a').filter({ hasText: 'Settings' });
    if ((await settingsLink.count()) > 0) {
      await settingsLink.click();
      await expect(page.locator('body')).toContainText(/settings|config/i);
    }

    // Click Dashboard to return
    const dashLink = page.locator('nav a').filter({ hasText: 'Dashboard' });
    await dashLink.click();
    await expect(page).toHaveURL(/\/ui\/$/);
  });

  test('user creates task via API then clicks it in dashboard', async ({
    page,
    request,
  }) => {
    // Create task via API
    const { id } = await (
      await request.post(`${BASE}/tasks`, {
        headers: apiHeaders,
        data: {
          repo_url: 'https://github.com/test/click',
          task: 'Click test task',
          repo_owner: 'test',
          repo_name: 'click',
        },
      })
    ).json();

    await login(page);
    // Wait for htmx refresh to load task list
    await page.waitForTimeout(6000);

    // Click on the task card in dashboard
    const taskLink = page.locator(`a[href="/ui/tasks/${id}"]`);
    if ((await taskLink.count()) > 0) {
      await taskLink.click();
      // Should navigate to detail page
      await expect(page.locator('#activity-feed')).toBeVisible();
    }
  });
});

// ---- Group 2: Store error handling ----

test.describe('Store error scenarios', () => {
  test('dashboard renders even with no tasks', async ({ page }) => {
    await login(page);
    // Dashboard should show either tasks or empty state -- never a 500
    const status = await page.evaluate(async (base) => {
      const r = await fetch(`${base}/ui/`);
      return r.status;
    }, BASE);
    expect(status).toBe(200);
  });

  test('task detail for nonexistent ID returns 404 not 500', async ({
    request,
  }) => {
    const resp = await request.get(`${BASE}/tasks/does-not-exist-abc`, {
      headers: { Authorization: `Bearer ${API_TOKEN}` },
    });
    expect(resp.status()).toBe(404);
  });
});

// ---- Group 3: Zero-value and edge display ----

test.describe('Display edge cases', () => {
  test('task with no repo info displays gracefully', async ({
    page,
    request,
  }) => {
    await request.post(`${BASE}/tasks`, {
      headers: apiHeaders,
      data: { repo_url: 'https://example.com', task: 'No repo metadata' },
    });

    await login(page);
    await page.waitForTimeout(6000);

    // Should not show "undefined" or "#0"
    const body = await page.textContent('body');
    expect(body).not.toContain('undefined');
    expect(body).not.toContain('#0');
  });

  test('task description with newlines renders without breaking layout', async ({
    page,
    request,
  }) => {
    await request.post(`${BASE}/tasks`, {
      headers: apiHeaders,
      data: {
        repo_url: 'https://example.com',
        task: 'Line 1\nLine 2\nLine 3',
      },
    });

    await login(page);
    await page.waitForTimeout(6000);
    const body = await page.textContent('body');
    expect(body).toContain('Line 1');
  });
});

// ---- Group 4: Route resolution ----

test.describe('Route resolution', () => {
  test('/ui/tasks/new resolves to new-task page not task detail', async ({
    page,
  }) => {
    await login(page);
    await page.goto(`${BASE}/ui/tasks/new`);
    // Should show the task form, not a 404
    await expect(page.locator('form[action="/api/tasks"]')).toBeVisible();
    await expect(page.locator('textarea[name="description"]')).toBeVisible();
  });
});

// ---- Group 5: Request body limits ----

test.describe('Request limits', () => {
  test('request over 1MB is rejected', async ({ request }) => {
    const hugeBody = JSON.stringify({
      repo_url: 'https://example.com',
      task: 'x'.repeat(2 * 1024 * 1024),
    });
    const resp = await request.post(`${BASE}/tasks`, {
      headers: apiHeaders,
      data: hugeBody,
    });
    // Should be 413 (body too large) or 400
    expect(resp.status()).toBeGreaterThanOrEqual(400);
    expect(resp.status()).toBeLessThan(500);
  });
});

// ---- Group 6: SSE rendering edge cases ----

test.describe('SSE rendering edge cases', () => {
  test('steer event with empty message returns 400', async ({ request }) => {
    const { id } = await (
      await request.post(`${BASE}/tasks`, {
        headers: apiHeaders,
        data: { repo_url: 'https://example.com', task: 'steer empty test' },
      })
    ).json();

    const resp = await request.post(`${BASE}/tasks/${id}/steer`, {
      headers: apiHeaders,
      data: { message: '' },
    });
    expect(resp.status()).toBe(400);
  });

  test('steer event with whitespace-only message returns 400', async ({
    request,
  }) => {
    const { id } = await (
      await request.post(`${BASE}/tasks`, {
        headers: apiHeaders,
        data: {
          repo_url: 'https://example.com',
          task: 'steer whitespace test',
        },
      })
    ).json();

    const resp = await request.post(`${BASE}/tasks/${id}/steer`, {
      headers: apiHeaders,
      data: { message: '   ' },
    });
    expect(resp.status()).toBe(400);
  });
});
