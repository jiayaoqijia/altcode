import { test, expect, type Page } from '@playwright/test';

// -------------------------------------------------------------------
// Agent orchestration E2E tests.
//
// Tests the full task lifecycle via the daemon REST API (bearer auth)
// and web UI pages (session auth). The web handler's store is wired
// to the daemon store, so dashboard and task detail pages display
// API-created tasks.
// -------------------------------------------------------------------

const BASE = process.env.BASE_URL || `http://localhost:${process.env.ALTFIX_PORT || 9200}`;
const API_TOKEN = 'e2e-token';

const apiHeaders = {
  'Authorization': `Bearer ${API_TOKEN}`,
  'Content-Type': 'application/json',
};

async function login(page: Page) {
  await page.goto(`${BASE}/auth/test-login`);
  await page.waitForURL(/\/ui\/(?!login)/);
}

// Helper: create a task via the REST API and return its ID.
async function createTask(
  request: any, desc: string, extra?: Record<string, unknown>,
): Promise<{ id: string; status: number }> {
  const resp = await request.post(`${BASE}/tasks`, {
    headers: apiHeaders,
    data: {
      repo_url: 'https://github.com/test/e2e',
      task: desc,
      ...extra,
    },
  });
  const body = await resp.json();
  return { id: body.id, status: resp.status() };
}

// ---- API lifecycle tests ----

test.describe('Task lifecycle via API', () => {
  test('create task returns 201 with pending status', async ({ request }) => {
    const resp = await request.post(`${BASE}/tasks`, {
      headers: apiHeaders,
      data: {
        repo_url: 'https://github.com/jiayaoqijia/altcode',
        task: 'Write a Go function that adds two numbers',
      },
    });
    expect(resp.status()).toBe(201);
    const body = await resp.json();
    expect(body).toHaveProperty('id');
    expect(body.id.length).toBeGreaterThan(0);
    expect(body).toHaveProperty('status', 'pending');
  });

  test('retrieve created task via GET', async ({ request }) => {
    const { id, status } = await createTask(request, 'Retrieval test task');
    expect(status).toBe(201);

    const getResp = await request.get(`${BASE}/tasks/${id}`, {
      headers: apiHeaders,
    });
    expect(getResp.status()).toBe(200);
    const body = await getResp.json();
    expect(body.task.task_description).toBe('Retrieval test task');
    expect(body.task.status).toBe('pending');
  });

  test('list tasks includes created task', async ({ request }) => {
    const { id } = await createTask(request, 'List inclusion test');

    const listResp = await request.get(`${BASE}/tasks`, {
      headers: apiHeaders,
    });
    expect(listResp.status()).toBe(200);
    const tasks = await listResp.json();
    expect(Array.isArray(tasks)).toBe(true);
    const found = tasks.find((t: any) => t.id === id);
    expect(found).toBeTruthy();
    expect(found.task_description).toBe('List inclusion test');
  });

  test('steer a task via API', async ({ request }) => {
    const { id } = await createTask(request, 'Steer target task');

    const steerResp = await request.post(`${BASE}/tasks/${id}/steer`, {
      headers: apiHeaders,
      data: { message: 'Focus on error handling' },
    });
    expect(steerResp.status()).toBe(202);
  });

  test('stop a task via API', async ({ request }) => {
    const { id } = await createTask(request, 'Stop target task');

    const stopResp = await request.post(`${BASE}/tasks/${id}/stop`, {
      headers: apiHeaders,
    });
    expect(stopResp.status()).toBe(202);
    const body = await stopResp.json();
    expect(body.status).toBe('stopping');
  });

  test('steer nonexistent task returns 404', async ({ request }) => {
    const resp = await request.post(`${BASE}/tasks/nonexistent-id/steer`, {
      headers: apiHeaders,
      data: { message: 'hello' },
    });
    expect(resp.status()).toBe(404);
  });

  test('stop nonexistent task returns 404', async ({ request }) => {
    const resp = await request.post(`${BASE}/tasks/nonexistent-id/stop`, {
      headers: apiHeaders,
    });
    expect(resp.status()).toBe(404);
  });

  test('create task with repo metadata', async ({ request }) => {
    const resp = await request.post(`${BASE}/tasks`, {
      headers: apiHeaders,
      data: {
        repo_url: 'https://github.com/jiayaoqijia/altcode',
        task: 'Fix flaky test in auth_test.go',
        repo_owner: 'jiayaoqijia',
        repo_name: 'altcode',
        issue_number: 42,
      },
    });
    expect(resp.status()).toBe(201);
    const body = await resp.json();
    expect(body.id).toBeTruthy();
  });
});

// ---- SSE streaming tests ----

test.describe('SSE streaming via API', () => {
  test('SSE endpoint returns event-stream content type', async ({ page, request }) => {
    const { id } = await createTask(request, 'SSE content-type test');

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

  test('SSE streams heartbeat within 5 seconds', async ({ request }) => {
    const { id } = await createTask(request, 'SSE heartbeat test');

    const ctrl = new AbortController();
    const resp = await fetch(`${BASE}/tasks/${id}/sse`, {
      headers: { 'Authorization': `Bearer ${API_TOKEN}` },
      signal: ctrl.signal,
    });
    expect(resp.status).toBe(200);
    expect(resp.headers.get('content-type')).toContain('text/event-stream');

    const reader = resp.body!.getReader();
    const decoder = new TextDecoder();
    let buf = '';
    let hasHeartbeat = false;
    const deadline = Date.now() + 5000;
    try {
      while (Date.now() < deadline) {
        const { value, done } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        if (/^:\s*heartbeat/m.test(buf)) {
          hasHeartbeat = true;
          break;
        }
      }
    } finally {
      ctrl.abort();
    }
    expect(hasHeartbeat).toBe(true);
  });

  test('SSE reconnects after disconnect', async ({ page, request }) => {
    const { id } = await createTask(request, 'SSE reconnect test');

    // Navigate to a page so we have a valid page context.
    await page.goto(`${BASE}/ui/login`);

    // First connection: verify heartbeat arrives.
    const firstHeartbeat = await page.evaluate(
      async ([url, token]) => {
        const ctrl = new AbortController();
        const resp = await fetch(url, {
          headers: { 'Authorization': `Bearer ${token}` },
          signal: ctrl.signal,
        });
        const reader = resp.body!.getReader();
        const decoder = new TextDecoder();
        let buf = '';
        const deadline = Date.now() + 5000;
        let found = false;
        while (Date.now() < deadline) {
          const { value, done } = await reader.read();
          if (done) break;
          buf += decoder.decode(value, { stream: true });
          if (/^:\s*heartbeat/m.test(buf)) {
            found = true;
            break;
          }
        }
        ctrl.abort();
        return found;
      },
      [`${BASE}/tasks/${id}/sse`, API_TOKEN] as [string, string],
    );
    expect(firstHeartbeat).toBe(true);

    // Second connection (simulates reconnect): verify heartbeat again.
    const secondHeartbeat = await page.evaluate(
      async ([url, token]) => {
        const ctrl = new AbortController();
        const resp = await fetch(url, {
          headers: { 'Authorization': `Bearer ${token}` },
          signal: ctrl.signal,
        });
        const reader = resp.body!.getReader();
        const decoder = new TextDecoder();
        let buf = '';
        const deadline = Date.now() + 5000;
        let found = false;
        while (Date.now() < deadline) {
          const { value, done } = await reader.read();
          if (done) break;
          buf += decoder.decode(value, { stream: true });
          if (/^:\s*heartbeat/m.test(buf)) {
            found = true;
            break;
          }
        }
        ctrl.abort();
        return found;
      },
      [`${BASE}/tasks/${id}/sse`, API_TOKEN] as [string, string],
    );
    expect(secondHeartbeat).toBe(true);
  });

  test('SSE for nonexistent task returns 404', async ({ request }) => {
    const resp = await request.get(`${BASE}/tasks/nonexistent-id/sse`, {
      headers: apiHeaders,
    });
    expect(resp.status()).toBe(404);
  });
});

// ---- Web UI orchestration flow tests ----

test.describe('Web UI orchestration pages', () => {
  test('dashboard renders and links to new task form', async ({ page }) => {
    await login(page);

    await expect(page.locator('h1')).toContainText('Dashboard');
    const newTaskLink = page.locator('a[href="/ui/tasks/new"]');
    await expect(newTaskLink).toBeVisible();
    await expect(newTaskLink).toContainText('New Task');
  });

  test('new task form accepts description input', async ({ page }) => {
    await login(page);
    await page.goto(`${BASE}/ui/tasks/new`);

    const textarea = page.locator('textarea[name="description"]');
    await expect(textarea).toBeVisible();

    await textarea.fill('Write a Go function that adds two numbers');
    const value = await textarea.inputValue();
    expect(value).toBe('Write a Go function that adds two numbers');

    // Submit button is present
    const submitBtn = page.locator(
      'form[action="/api/tasks"] button[type="submit"]',
    );
    await expect(submitBtn).toBeVisible();
    await expect(submitBtn).toContainText('Create Task');
  });

  test('dashboard shows API-created tasks', async ({ page, request }) => {
    const { id } = await createTask(request, 'Dashboard visibility test');
    await login(page);
    // Store is wired, so API-created tasks appear on the dashboard.
    await expect(page.locator('#task-list')).toContainText('Dashboard visibility test');
  });

  test('task detail renders for API-created task', async ({ page, request }) => {
    const { id } = await createTask(request, 'Detail page render test');
    await login(page);

    const resp = await page.goto(`${BASE}/ui/tasks/${id}`);
    // Store is wired, so detail page renders with task content.
    expect(resp?.status()).toBe(200);
    await expect(page.locator('body')).toContainText('Detail page render test');
  });

  test('navigation flow: dashboard -> new task -> back', async ({ page }) => {
    await login(page);

    await page.click('a[href="/ui/tasks/new"]');
    await page.waitForURL('**/ui/tasks/new');
    await expect(page.locator('h1')).toContainText('New Task');

    await page.goBack();
    await expect(page).toHaveURL(/\/ui\/$/);
    await expect(page.locator('h1')).toContainText('Dashboard');
  });
});

// ---- Full journey: API + UI combined ----

test.describe('Full orchestration journey', () => {
  test('create via API, verify via API, then check dashboard', async ({ page, request }) => {
    await login(page);

    // 1. Create task via API
    const { id, status } = await createTask(
      request, 'Full journey test task',
    );
    expect(status).toBe(201);
    expect(id).toBeTruthy();

    // 2. Verify task exists via API
    const getResp = await request.get(`${BASE}/tasks/${id}`, {
      headers: apiHeaders,
    });
    expect(getResp.status()).toBe(200);
    const taskBody = await getResp.json();
    expect(taskBody.task.task_description).toBe('Full journey test task');

    // 3. Steer the task
    const steerResp = await request.post(`${BASE}/tasks/${id}/steer`, {
      headers: apiHeaders,
      data: { message: 'Prioritize test coverage' },
    });
    expect(steerResp.status()).toBe(202);

    // 4. Stop the task
    const stopResp = await request.post(`${BASE}/tasks/${id}/stop`, {
      headers: apiHeaders,
    });
    expect(stopResp.status()).toBe(202);

    // 5. Dashboard still renders (doesn't crash after API mutations)
    await page.goto(`${BASE}/ui/`);
    await expect(page.locator('h1')).toContainText('Dashboard');
  });

  test('multiple tasks created and listed', async ({ request }) => {
    const ids: string[] = [];
    for (let i = 0; i < 3; i++) {
      const { id } = await createTask(request, `Batch task ${i}`);
      ids.push(id);
    }

    const listResp = await request.get(`${BASE}/tasks`, {
      headers: apiHeaders,
    });
    expect(listResp.status()).toBe(200);
    const tasks = await listResp.json();

    for (const id of ids) {
      const found = tasks.find((t: any) => t.id === id);
      expect(found).toBeTruthy();
    }
  });

  test('steer then stop sequence', async ({ request }) => {
    const { id } = await createTask(request, 'Steer-then-stop test');

    // Steer
    const steerResp = await request.post(`${BASE}/tasks/${id}/steer`, {
      headers: apiHeaders,
      data: { message: 'Focus on error handling, skip UI' },
    });
    expect(steerResp.status()).toBe(202);

    // Stop
    const stopResp = await request.post(`${BASE}/tasks/${id}/stop`, {
      headers: apiHeaders,
    });
    expect(stopResp.status()).toBe(202);

    // Task should exist with updated status
    const getResp = await request.get(`${BASE}/tasks/${id}`, {
      headers: apiHeaders,
    });
    expect(getResp.status()).toBe(200);
  });
});
