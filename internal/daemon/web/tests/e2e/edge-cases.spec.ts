import { test, expect, type Page } from '@playwright/test';

const BASE = process.env.BASE_URL || 'http://localhost:9100';
const API_TOKEN = 'e2e-token';
const apiHeaders = { 'Authorization': `Bearer ${API_TOKEN}`, 'Content-Type': 'application/json' };

async function login(page: Page) {
  await page.goto(`${BASE}/auth/test-login`);
  await page.waitForURL(/\/ui\/(?!login)/);
}

async function createTask(request: any, desc: string, extra: any = {}) {
  const resp = await request.post(`${BASE}/tasks`, {
    headers: apiHeaders,
    data: { repo_url: 'https://github.com/test/edge', task: desc, ...extra }
  });
  return resp.json();
}

// --- Input edge cases ---

test.describe('Input edge cases', () => {
  test('empty task description returns 400', async ({ request }) => {
    const resp = await request.post(`${BASE}/tasks`, {
      headers: apiHeaders,
      data: { repo_url: 'https://github.com/test/edge', task: '' }
    });
    expect(resp.status()).toBe(400);
  });

  test('whitespace-only task description returns 400', async ({ request }) => {
    const resp = await request.post(`${BASE}/tasks`, {
      headers: apiHeaders,
      data: { repo_url: 'https://github.com/test/edge', task: '   \t\n  ' }
    });
    expect(resp.status()).toBe(400);
  });

  test('10KB task description is accepted', async ({ request }) => {
    const longDesc = 'A'.repeat(10240);
    const resp = await request.post(`${BASE}/tasks`, {
      headers: apiHeaders,
      data: { repo_url: 'https://github.com/test/edge', task: longDesc }
    });
    expect(resp.status()).toBe(201);
    const body = await resp.json();
    expect(body.id).toBeTruthy();
  });

  test('special chars in description are preserved', async ({ request }) => {
    const desc = 'Fix "quotes" & <angle> brackets\nnewline\ttab';
    const create = await request.post(`${BASE}/tasks`, {
      headers: apiHeaders,
      data: { repo_url: 'https://github.com/test/edge', task: desc }
    });
    expect(create.status()).toBe(201);
    const { id } = await create.json();

    const get = await request.get(`${BASE}/tasks/${id}`, { headers: apiHeaders });
    const body = await get.json();
    expect(body.task.task_description).toBe(desc);
  });

  test('unicode/emoji in task description', async ({ request }) => {
    const desc = '\u4fee\u590d\u8ba4\u8bc1\u6a21\u5757 a\u00f1adir pruebas';
    const resp = await request.post(`${BASE}/tasks`, {
      headers: apiHeaders,
      data: { repo_url: 'https://github.com/test/edge', task: desc }
    });
    expect(resp.status()).toBe(201);
    const { id } = await resp.json();

    const get = await request.get(`${BASE}/tasks/${id}`, { headers: apiHeaders });
    const body = await get.json();
    expect(body.task.task_description).toBe(desc);
  });
});

// --- SSE edge cases ---

test.describe('SSE edge cases', () => {
  test('negative Last-Event-ID is handled gracefully', async ({ page, request }) => {
    const { id } = await (await request.post(`${BASE}/tasks`, {
      headers: apiHeaders,
      data: { repo_url: 'https://github.com/test/sse-edge', task: 'SSE negative ID' }
    })).json();

    // SSE with negative Last-Event-ID should return events from beginning.
    // Use page.evaluate with AbortController since Playwright request API
    // waits for body close on SSE streams.
    await page.goto(`${BASE}/ui/login`);
    const contentType = await page.evaluate(
      async ([url, token]) => {
        const ctrl = new AbortController();
        const resp = await fetch(url, {
          headers: { 'Authorization': `Bearer ${token}`, 'Last-Event-ID': '-1' },
          signal: ctrl.signal,
        });
        const ct = resp.headers.get('content-type');
        ctrl.abort();
        return ct;
      },
      [`${BASE}/tasks/${id}/sse`, API_TOKEN] as [string, string],
    );
    expect(contentType).toContain('text/event-stream');
  });

  test('SSE on stopped task connects and closes', async ({ page, request }) => {
    const { id } = await (await request.post(`${BASE}/tasks`, {
      headers: apiHeaders,
      data: { repo_url: 'https://github.com/test/sse-edge', task: 'SSE completed' }
    })).json();

    // Stop the task to make it terminal
    await request.post(`${BASE}/tasks/${id}/stop`, { headers: apiHeaders });

    // SSE should still connect (task exists) - verify content-type
    await page.goto(`${BASE}/ui/login`);
    const contentType = await page.evaluate(
      async ([url, token]) => {
        const ctrl = new AbortController();
        const resp = await fetch(url, {
          headers: { 'Authorization': `Bearer ${token}` },
          signal: ctrl.signal,
        });
        const ct = resp.headers.get('content-type');
        ctrl.abort();
        return ct;
      },
      [`${BASE}/tasks/${id}/sse`, API_TOKEN] as [string, string],
    );
    expect(contentType).toContain('text/event-stream');
  });

  test('SSE reconnect with Last-Event-ID replays missed events', async ({ request }) => {
    const { id } = await (await request.post(`${BASE}/tasks`, {
      headers: apiHeaders,
      data: { repo_url: 'https://github.com/test/sse-replay', task: 'SSE replay test' }
    })).json();

    // Create steer events
    await request.post(`${BASE}/tasks/${id}/steer`, {
      headers: apiHeaders,
      data: { message: 'Event 1' }
    });
    await request.post(`${BASE}/tasks/${id}/steer`, {
      headers: apiHeaders,
      data: { message: 'Event 2' }
    });

    // Connect SSE with Last-Event-ID=0 to get all events. Use Node
    // fetch + AbortController to read the raw stream.
    const ctrl = new AbortController();
    const resp = await fetch(`${BASE}/tasks/${id}/sse`, {
      headers: { 'Authorization': `Bearer ${API_TOKEN}`, 'Last-Event-ID': '0' },
      signal: ctrl.signal,
    });
    expect(resp.status).toBe(200);

    const reader = resp.body!.getReader();
    const decoder = new TextDecoder();
    let buf = '';
    const deadline = Date.now() + 6000;
    try {
      while (Date.now() < deadline) {
        const { value, done } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        if (buf.includes('user_steer')) break;
      }
    } finally {
      ctrl.abort();
    }
    expect(buf).toContain('user_steer');
  });
});

// --- Share link edge cases ---

test.describe('Share link edge cases', () => {
  test('share link with URL-encoded task ID', async ({ request }) => {
    const resp = await request.get(`${BASE}/share/abc%20def.invalidhmac?exp=9999999999`);
    // Should handle gracefully: 403 (invalid HMAC) not 500
    expect([400, 403]).toContain(resp.status());
  });

  test('share link with very old expiry', async ({ request }) => {
    const resp = await request.get(`${BASE}/share/abc.hmac?exp=0`);
    expect(resp.status()).toBe(403); // expired
  });

  test('share link with future expiry year 3000', async ({ request }) => {
    const resp = await request.get(`${BASE}/share/abc.hmac?exp=32503680000`);
    expect(resp.status()).toBe(403); // invalid HMAC
  });
});

// --- UI display edge cases ---

test.describe('UI display edge cases', () => {
  test('dashboard at mobile viewport 375px', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 812 });
    await login(page);
    // Page should render without excessive horizontal scroll
    const bodyWidth = await page.evaluate(() => document.body.scrollWidth);
    const viewWidth = await page.evaluate(() => window.innerWidth);
    expect(bodyWidth).toBeLessThanOrEqual(viewWidth + 20); // 20px tolerance
  });

  test('task detail at mobile viewport', async ({ page, request }) => {
    const { id } = await createTask(request, 'Mobile detail test');
    await page.setViewportSize({ width: 375, height: 812 });
    await login(page);
    await page.goto(`${BASE}/ui/tasks/${id}`);
    await expect(page.locator('#activity-feed')).toBeVisible();
  });

  test('long task description does not overflow card', async ({ page, request }) => {
    await createTask(request, 'A'.repeat(500));
    await login(page);
    // Wait for htmx polling to load task list
    await page.waitForTimeout(6000);
    // Check no horizontal overflow on any card
    const overflow = await page.evaluate(() => {
      const cards = document.querySelectorAll('.card');
      for (const card of cards) {
        if (card.scrollWidth > card.clientWidth + 10) return true;
      }
      return false;
    });
    expect(overflow).toBe(false);
  });

  test('zero cost task displays $0.00', async ({ page, request }) => {
    await createTask(request, 'Zero cost test');
    await login(page);
    // Wait for htmx polling to load KPI cards
    await page.waitForTimeout(6000);
    const body = await page.textContent('body');
    expect(body).toContain('$0.00');
  });

  test('back button after logout shows login page', async ({ page }) => {
    await login(page);
    // Navigate to a page
    await page.goto(`${BASE}/ui/prs`);
    // Logout via fetch so we stay in the same page context
    const csrfToken = await page.locator('meta[name="csrf-token"]').getAttribute('content');
    await page.evaluate(async ([csrf, base]) => {
      await fetch(`${base}/auth/logout`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrf! },
        redirect: 'manual',
      });
    }, [csrfToken, BASE]);
    // After logout, navigating should redirect to login
    await page.goto(`${BASE}/ui/`);
    await page.waitForURL(/\/ui\/login/);
  });
});

// --- Auth edge cases ---

test.describe('Auth edge cases', () => {
  test('expired cookie value is rejected', async ({ page }) => {
    await page.context().addCookies([{
      name: 'altfix_session',
      value: 'totally-fake-session-id-that-does-not-exist',
      domain: 'localhost',
      path: '/'
    }]);
    await page.goto(`${BASE}/ui/`);
    await page.waitForURL(/\/ui\/login/);
  });

  test('empty cookie value is rejected', async ({ page }) => {
    await page.context().addCookies([{
      name: 'altfix_session',
      value: '',
      domain: 'localhost',
      path: '/'
    }]);
    await page.goto(`${BASE}/ui/`);
    await page.waitForURL(/\/ui\/login/);
  });

  test('concurrent requests with same session', async ({ browser }) => {
    const ctx = await browser.newContext();
    const page = await ctx.newPage();
    await page.goto(`${BASE}/auth/test-login`);
    await page.waitForURL(/\/ui\/(?!login)/);

    // Fire 5 concurrent requests
    const results = await page.evaluate(async (base) => {
      const promises = Array.from({ length: 5 }, () =>
        fetch(`${base}/ui/`).then(r => r.status)
      );
      return Promise.all(promises);
    }, BASE);

    // All should succeed (no race condition in session store)
    results.forEach(status => expect(status).toBe(200));
    await ctx.close();
  });
});
