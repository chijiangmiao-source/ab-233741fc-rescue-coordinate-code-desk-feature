import { expect, test } from '@playwright/test';

// 签发记录页：倒序浏览、键集分页、快照稳定、游标失败就地重试，
// 以及把短码带入核验页（只预填、不自动提交，再走真实核验链路）。
// 全部打真实服务；各用例串行共享同一个 SQLite 库，因此总数按“开测前已有
// 数量 + 本用例新增数量”计算，不假设库是空的。

async function issueCardAPI(page, x, y) {
  const res = await page.request.post('/api/cards', {
    data: { x, y },
    headers: { 'Content-Type': 'application/json' },
  });
  expect(res.status()).toBe(201);
  return res.json();
}

// 每次跑选用随机基数，避免对同一持久库重复跑时短码去重导致数量断言失效。
const runBase = Math.floor(Math.random() * 8000) + 100;

// 签发 n 张互不相同的卡，返回按签发顺序的记录。
async function seedCards(page, n, offset) {
  const cards = [];
  for (let i = 1; i <= n; i++) {
    const k = (runBase + offset + i) % 10000;
    cards.push(await issueCardAPI(page, k, (k + 5000) % 10000));
  }
  return cards;
}

// 直接走只读接口统计库中卡片总数（每页 50 是服务端允许的上限）。
async function totalCards(page) {
  let cursor = '';
  let total = 0;
  do {
    const q = new URLSearchParams({ limit: '50' });
    if (cursor) q.set('cursor', cursor);
    const res = await page.request.get(`/api/cards?${q}`);
    expect(res.status()).toBe(200);
    const body = await res.json();
    total += body.cards.length;
    cursor = body.next_cursor;
  } while (cursor);
  return total;
}

async function rowIds(page) {
  return page.getByTestId('record-row').evaluateAll((nodes) =>
    nodes.map((li) => Number(li.dataset.id)),
  );
}

async function rowCodes(page) {
  return page.getByTestId('record-code').allTextContents();
}

async function loadAll(page) {
  while (await page.getByTestId('load-more').isVisible()) {
    const [resp] = await Promise.all([
      page.waitForResponse(
        (r) => r.url().includes('/api/cards') && r.request().method() === 'GET',
      ),
      page.getByTestId('load-more').click(),
    ]);
    expect(resp.status()).toBe(200);
  }
}

test('签发记录倒序分页，顺序稳定且不重不漏', async ({ page }) => {
  const before = await totalCards(page);
  const cards = await seedCards(page, 25, 0);

  await page.goto('/#/records');
  await expect(page.getByTestId('records-list')).toBeVisible();

  // 首批固定 20 条：最新签发的排在第一。
  await expect(page.getByTestId('record-row')).toHaveCount(20);
  await expect(page.getByTestId('record-code').first()).toHaveText(cards[24].code);
  await expect(page.getByTestId('load-more')).toBeVisible();

  // 继续加载直到没有更早记录：总数与只读接口一致，严格倒序、无重复。
  await loadAll(page);
  const total = before + 25;
  await expect(page.getByTestId('record-row')).toHaveCount(total);
  const ids = await rowIds(page);
  expect(ids).toEqual([...ids].sort((a, b) => b - a));
  expect(new Set(ids).size).toBe(total);
  const codes = await rowCodes(page);
  for (const c of cards) expect(codes).toContain(c.code);
});

test('翻页期间新签发的卡不进入当前快照，重新打开后才出现', async ({ page }) => {
  const cards = await seedCards(page, 25, 100);

  await page.goto('/#/records');
  await expect(page.getByTestId('record-row')).toHaveCount(20);
  const snapshot = await page.getByTestId('snapshot-boundary').textContent();

  // 浏览途中另一席位新签发一张卡。取与本文件各 seed 区间（runBase 附近
  // 连续 25 个点）必然不重合的坐标，避免短码去重返回旧卡。
  const lateX = (runBase + 5000) % 10000;
  const late = await issueCardAPI(page, lateX, (lateX + 5000) % 10000);

  // 翻完当前快照：本用例已签发的 25 张都在，新卡不插入、快照编号不变。
  await loadAll(page);
  const codes = await rowCodes(page);
  expect(codes).not.toContain(late.code);
  for (const c of cards) expect(codes).toContain(c.code);
  await expect(page.getByTestId('snapshot-boundary')).toHaveText(snapshot);

  // 重新打开记录（新快照）：经另一页再进入，触发组件重新挂载。
  await page.goto('/#/issue');
  await page.goto('/#/records');
  await expect(page.getByTestId('record-code').first()).toHaveText(late.code);
});

test('非法游标就地提示失败并保留已加载内容，恢复后可重试同一页', async ({ page }) => {
  await seedCards(page, 25, 200);
  await page.goto('/#/records');
  await expect(page.getByTestId('record-row')).toHaveCount(20);

  // 让“下一页”请求携带伪造游标：服务端必须 400 拒绝且不返回任何记录。
  await page.route('**/api/cards*', async (route) => {
    const url = new URL(route.request().url());
    if (url.searchParams.get('cursor')) {
      url.searchParams.set('cursor', 'forged-cursor');
      await route.continue({ url: url.toString() });
    } else {
      await route.continue();
    }
  });

  await page.getByTestId('load-more').click();
  await expect(page.getByTestId('records-page-error')).toBeVisible();
  await expect(page.getByTestId('records-page-error')).toContainText('游标无效');
  // 已加载的 20 行原样保留，错误响应没有泄露任何记录。
  await expect(page.getByTestId('record-row')).toHaveCount(20);

  // 恢复正常链路后重试同一页：成功追加第二页，错误提示消失。
  await page.unroute('**/api/cards*');
  await page.getByTestId('retry-page').click();
  await expect(page.getByTestId('records-page-error')).toHaveCount(0);
  await expect(page.getByTestId('record-row')).toHaveCount(40);
});

test('从记录带入短码只预填不提交，经真实核验还原唯一坐标', async ({ page }) => {
  const card = await issueCardAPI(page, 2222, 4444);

  await page.goto('/#/records');
  await page.getByTestId('record-code').first().waitFor();
  // 找到该卡所在行并点击“带入核验”。
  const row = page.locator('[data-testid="record-row"]', {
    has: page.locator('[data-testid="record-code"]', { hasText: card.code }),
  });
  await row.getByTestId('bring-verify').click();

  // 跳到核验页：短码已带入，但没有自动核验。
  await expect(page).toHaveURL(/#\/verify\?code=/);
  await expect(page.getByTestId('input-code')).toHaveValue(card.code);
  await expect(page.getByTestId('verify-prefilled')).toBeVisible();
  await expect(page.getByTestId('verify-result')).toHaveCount(0);

  // 由人工点击核验，走真实核验链路还原唯一坐标。
  await page.getByRole('button', { name: '核验' }).click();
  await expect(page.getByTestId('verify-result')).toBeVisible();
  await expect(page.getByTestId('verify-code')).toHaveText(card.code);
  await expect(page.getByTestId('verify-x')).toHaveText('2222');
  await expect(page.getByTestId('verify-y')).toHaveText('4444');
  await expect(page.getByTestId('verify-issued')).toContainText('本台已签发');
});
