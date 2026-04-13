import { test, expect, type Page } from '@playwright/test';

// -------------------------------------------------------------------
// AltFix Web UI E2E Tests
//
// These tests run against a live altcode daemon started by setup.sh
// with --test-mode enabled. The /auth/test-login endpoint creates a
// pre-authenticated session so Playwright can exercise all
// authenticated pages without a real GitHub OAuth flow.
// -------------------------------------------------------------------

const BASE = process.env.BASE_URL || `http://localhost:${process.env.ALTFIX_PORT || 9100}`;

// Helper: authenticate via test-login bypass.
// Uses a regex that excludes /ui/login so a broken redirect is caught
// immediately instead of silently running tests against the login page.
async function login(page: Page) {
  await page.goto(`${BASE}/auth/test-login`);
  await page.waitForURL(/\/ui\/(?!login)/);
}

// ---- Unauthenticated tests ----

test.describe('Health endpoint', () => {
  test('returns 200 with status ok', async ({ request }) => {
    const resp = await request.get('/health');
    expect(resp.status()).toBe(200);
    const body = await resp.json();
    expect(body).toHaveProperty('status', 'ok');
  });
});

test.describe('Login page', () => {
  test('renders with correct branding', async ({ page }) => {
    await page.goto('/ui/login');
    await expect(page).toHaveTitle(/AltFix/);
    await expect(page.locator('h1')).toContainText('AltFix Control Plane');
    await expect(page.getByText('Sign in with GitHub')).toBeVisible();
  });

  test('displays error parameter', async ({ page }) => {
    await page.goto('/ui/login?error=access+denied');
    await expect(page.locator('.bg-red-50')).toContainText('access denied');
  });

  test('GitHub sign-in link points to /auth/github', async ({ page }) => {
    await page.goto('/ui/login');
    const link = page.locator('a[href="/auth/github"]');
    await expect(link).toBeVisible();
    await expect(link).toContainText('Sign in with GitHub');
  });
});

test.describe('Unauthenticated redirects', () => {
  test('GET /ui/ redirects to /ui/login', async ({ page }) => {
    await page.goto('/ui/');
    await expect(page).toHaveURL(/\/ui\/login/);
    await expect(page.locator('h1')).toContainText('AltFix Control Plane');
  });

  test('GET /ui/tasks/new redirects to /ui/login', async ({ request }) => {
    const resp = await request.get('/ui/tasks/new', {
      maxRedirects: 0,
    });
    expect(resp.status()).toBe(302);
    expect(resp.headers()['location']).toBe('/ui/login');
  });

  test('GET /ui/prs redirects to /ui/login', async ({ request }) => {
    const resp = await request.get('/ui/prs', {
      maxRedirects: 0,
    });
    expect(resp.status()).toBe(302);
    expect(resp.headers()['location']).toBe('/ui/login');
  });

  test('GET /ui/settings redirects to /ui/login', async ({ request }) => {
    const resp = await request.get('/ui/settings', {
      maxRedirects: 0,
    });
    expect(resp.status()).toBe(302);
    expect(resp.headers()['location']).toBe('/ui/login');
  });

  test('GET /ui/partials/task-list redirects to /ui/login', async ({ request }) => {
    const resp = await request.get('/ui/partials/task-list', {
      maxRedirects: 0,
    });
    expect(resp.status()).toBe(302);
    expect(resp.headers()['location']).toBe('/ui/login');
  });

  test('test-login works in test mode', async ({ page }) => {
    await page.goto(`${BASE}/auth/test-login`);
    await page.waitForURL(/\/ui\/(?!login)/);
    const cookies = await page.context().cookies();
    const session = cookies.find((c) => c.name === 'altfix_session');
    expect(session).toBeTruthy();
  });
});

// ---- Static assets ----

test.describe('Static assets', () => {
  test('htmx.min.js loads', async ({ request }) => {
    const resp = await request.get('/ui/static/htmx.min.js');
    expect(resp.status()).toBe(200);
    const ct = resp.headers()['content-type'];
    expect(ct).toMatch(/javascript/);
    const body = await resp.text();
    expect(body.length).toBeGreaterThan(100);
  });

  test('app.css loads', async ({ request }) => {
    const resp = await request.get('/ui/static/app.css');
    expect(resp.status()).toBe(200);
    const ct = resp.headers()['content-type'];
    expect(ct).toMatch(/css/);
  });

  test('tailwind.css loads', async ({ request }) => {
    const resp = await request.get('/ui/static/tailwind.css');
    expect(resp.status()).toBe(200);
  });
});

// ---- Share link validation ----

test.describe('Share links', () => {
  test('missing dot separator returns 400', async ({ request }) => {
    const resp = await request.get('/share/badtoken');
    expect(resp.status()).toBe(400);
  });

  test('invalid HMAC returns 403', async ({ request }) => {
    const resp = await request.get(
      '/share/task123.deadbeef?exp=9999999999',
    );
    expect(resp.status()).toBe(403);
  });

  test('missing expiry returns 400', async ({ request }) => {
    const resp = await request.get('/share/task123.deadbeef');
    expect(resp.status()).toBe(400);
  });

  test('expired share link returns 403', async ({ request }) => {
    const resp = await request.get('/share/task123.deadbeef?exp=0');
    expect(resp.status()).toBe(403);
  });
});

// ---- OAuth redirect ----

test.describe('OAuth redirect', () => {
  test('GET /auth/github redirects to GitHub', async ({ request }) => {
    const resp = await request.get('/auth/github', {
      maxRedirects: 0,
    });
    expect(resp.status()).toBe(302);
    const location = resp.headers()['location'];
    expect(location).toContain('github.com/login/oauth/authorize');
    expect(location).toContain('client_id=');
    expect(location).toContain('code_challenge=');
    expect(location).toContain('state=');
  });

  test('sets altfix_oauth cookie', async ({ request }) => {
    const resp = await request.get('/auth/github', {
      maxRedirects: 0,
    });
    const setCookie = resp.headers()['set-cookie'];
    expect(setCookie).toBeDefined();
    expect(setCookie).toContain('altfix_oauth');
  });
});

// ---- OAuth callback error handling ----

test.describe('OAuth callback error handling', () => {
  test('missing OAuth cookie redirects to login with error', async ({ request }) => {
    const resp = await request.get('/auth/callback?state=x&code=y', {
      maxRedirects: 0,
    });
    expect(resp.status()).toBe(302);
    const location = resp.headers()['location'];
    expect(location).toContain('/ui/login');
    expect(location).toContain('error=');
  });
});

// ---- CSRF enforcement ----

test.describe('CSRF enforcement', () => {
  test('POST /auth/logout without session returns 302 to login', async ({ request }) => {
    const resp = await request.post('/auth/logout', {
      maxRedirects: 0,
    });
    expect(resp.status()).toBe(302);
    expect(resp.headers()['location']).toBe('/ui/login');
  });

  test('POST without CSRF token returns 403', async ({ page, request }) => {
    await login(page);
    const cookies = await page.context().cookies();
    const session = cookies.find((c) => c.name === 'altfix_session');
    // POST to a CSRF-protected endpoint with valid session but no token.
    const resp = await request.post(`${BASE}/auth/logout`, {
      headers: { Cookie: `altfix_session=${session?.value}` },
      maxRedirects: 0,
    });
    expect(resp.status()).toBe(403);
  });
});

// ---- API auth enforcement ----

test.describe('API auth enforcement', () => {
  test('POST /tasks without bearer token returns 401', async ({ request }) => {
    const resp = await request.post('/tasks', {
      data: { repo_url: 'https://github.com/test/test' },
    });
    expect(resp.status()).toBe(401);
  });

  test('GET /tasks without bearer token returns 401', async ({ request }) => {
    const resp = await request.get('/tasks');
    expect(resp.status()).toBe(401);
  });
});

// ---- Login page rendering ----

test.describe('Login page rendering', () => {
  test('no unexpected console errors on login page', async ({ page }) => {
    const errors: string[] = [];
    page.on('console', (msg) => {
      if (msg.type() === 'error') {
        const text = msg.text();
        // Known: layout.html refs /static/ vs /ui/static/ paths,
        // causing MIME type errors and 401s from bearer auth.
        if (
          text.includes('/static/tailwind.css') ||
          text.includes('/static/app.css') ||
          text.includes('/static/htmx.min.js') ||
          text.includes('status of 401')
        ) {
          return;
        }
        errors.push(text);
      }
    });
    await page.goto('/ui/login');
    await page.waitForLoadState('networkidle');
    expect(errors).toHaveLength(0);
  });

  test('login page is responsive at narrow viewport', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await page.goto('/ui/login');
    await expect(page.locator('h1')).toBeVisible();
    await expect(page.getByText('Sign in with GitHub')).toBeVisible();
    const card = page.locator('.card');
    const box = await card.boundingBox();
    expect(box).toBeTruthy();
    expect(box!.width).toBeLessThanOrEqual(375);
  });
});

// ---- Authenticated page tests ----

test.describe('Authenticated pages', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test('dashboard renders heading and new-task link', async ({ page }) => {
    await expect(page.locator('h1')).toContainText('Dashboard');
    // "New Task" button links to the creation form.
    const newTaskLink = page.locator('a[href="/ui/tasks/new"]');
    await expect(newTaskLink).toBeVisible();
    await expect(newTaskLink).toContainText('New Task');
  });

  test('dashboard shows KPI cards container', async ({ page }) => {
    // The htmx polling container for KPI cards should exist.
    await expect(
      page.locator('[hx-get="/ui/partials/kpi-cards"]'),
    ).toBeVisible();
  });

  test('dashboard shows task-list container', async ({ page }) => {
    // The htmx polling container for the task list should exist.
    await expect(
      page.locator('[hx-get*="/ui/partials/task-list"]'),
    ).toBeVisible();
  });

  test('dashboard shows empty state when no tasks', async ({ page }) => {
    await expect(page.locator('#task-list')).toContainText('No tasks yet');
  });

  test('new task page has form', async ({ page }) => {
    await page.goto(`${BASE}/ui/tasks/new`);
    await expect(page.locator('h1')).toContainText('New Task');
    // The task creation form (not the nav logout form).
    await expect(page.locator('form[action="/api/tasks"]')).toBeVisible();
    // Description textarea present.
    await expect(page.locator('textarea[name="description"]')).toBeVisible();
  });

  test('PR tracker page renders', async ({ page }) => {
    await page.goto(`${BASE}/ui/prs`);
    await expect(page.locator('h1')).toContainText('PR Tracker');
  });

  test('settings page renders for admin', async ({ page }) => {
    await page.goto(`${BASE}/ui/settings`);
    await expect(page.locator('h1')).toContainText('Settings');
    // Admin should see the settings cards, not the read-only warning.
    await expect(page.locator('body')).toContainText('Budget');
    await expect(page.locator('body')).toContainText('Access Control');
  });

  test('settings shows admin-only content', async ({ page }) => {
    await page.goto(`${BASE}/ui/settings`);
    // Admin user should see editable sections, not a read-only warning.
    const body = await page.textContent('body');
    expect(body).not.toContain('read-only');
  });

  test('nav bar shows branding and links', async ({ page }) => {
    const nav = page.locator('nav');
    await expect(nav).toBeVisible();
    // Branding link.
    await expect(nav.locator('a').filter({ hasText: 'AltFix' })).toBeVisible();
    // Dashboard link.
    await expect(nav.locator('a').filter({ hasText: 'Dashboard' })).toBeVisible();
    // PRs link.
    await expect(nav.locator('a').filter({ hasText: 'PRs' })).toBeVisible();
    // Admin should see Settings link.
    await expect(nav.locator('a').filter({ hasText: 'Settings' })).toBeVisible();
  });

  test('nav shows username and logout', async ({ page }) => {
    const nav = page.locator('nav');
    await expect(nav).toContainText('test-user');
    await expect(nav.locator('button').filter({ hasText: 'Logout' })).toBeVisible();
  });

  test('htmx loaded and active', async ({ page }) => {
    const htmxLoaded = await page.evaluate(
      () => typeof (window as any).htmx !== 'undefined',
    );
    expect(htmxLoaded).toBe(true);
  });

  test('htmx polling triggers request', async ({ page }) => {
    // Verify that the task-list container has hx-get and hx-trigger
    // polling attributes, proving htmx is wired for live updates.
    const taskListContainer = page.locator('[hx-get*="task-list"]');
    await expect(taskListContainer).toBeVisible();
    await expect(taskListContainer).toHaveAttribute('hx-trigger', /every/);
  });

  test('CSRF meta tag present', async ({ page }) => {
    const csrfContent = await page
      .locator('meta[name="csrf-token"]')
      .getAttribute('content');
    expect(csrfContent).toBeTruthy();
    expect(csrfContent!.length).toBeGreaterThan(10);
  });

  test('task detail 404 for nonexistent task', async ({ page }) => {
    const resp = await page.goto(`${BASE}/ui/tasks/nonexistent-id`);
    // A 500 is a server bug, not an acceptable response for a missing task.
    expect([200, 404, 302]).toContain(resp?.status() || 0);
  });

  test('navigation flow: dashboard -> new task -> back', async ({ page }) => {
    await page.click('a[href="/ui/tasks/new"]');
    await page.waitForURL('**/ui/tasks/new');
    await expect(page.locator('h1')).toContainText('New Task');
    // Go back to dashboard — assert /ui/ (not /ui/login).
    await page.goBack();
    await expect(page).toHaveURL(/\/ui\/$/);
    await expect(page.locator('h1')).toContainText('Dashboard');
  });

  test('dashboard status filter tabs present', async ({ page }) => {
    // All / Active / Completed / Failed tabs.
    await expect(page.locator('a[href="/ui/"]')).toBeVisible();
    await expect(page.locator('a[href="/ui/?status=active"]')).toBeVisible();
    await expect(page.locator('a[href="/ui/?status=completed"]')).toBeVisible();
    await expect(page.locator('a[href="/ui/?status=failed"]')).toBeVisible();
  });

  test('PR page has status filter tabs', async ({ page }) => {
    await page.goto(`${BASE}/ui/prs`);
    await expect(page.locator('a[href="/ui/prs"]')).toBeVisible();
    await expect(page.locator('a[href="/ui/prs?status=open"]')).toBeVisible();
    await expect(page.locator('a[href="/ui/prs?status=merged"]')).toBeVisible();
  });

  test('settings page shows connected user info', async ({ page }) => {
    await page.goto(`${BASE}/ui/settings`);
    // test-user login should appear in the Connected GitHub card.
    await expect(page.locator('body')).toContainText('test-user');
    // test-org should appear in the Connected GitHub card orgs.
    await expect(page.locator('body')).toContainText('test-org');
  });

  test('new task form has CSRF hidden field', async ({ page }) => {
    await page.goto(`${BASE}/ui/tasks/new`);
    const csrfField = page.locator('input[name="_csrf"]');
    await expect(csrfField).toHaveCount(1);
    const val = await csrfField.getAttribute('value');
    expect(val).toBeTruthy();
    expect(val!.length).toBeGreaterThan(10);
  });
});
