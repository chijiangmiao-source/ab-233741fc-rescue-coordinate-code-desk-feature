import { expect, test } from '@playwright/test';

// 签发记录页：真实栈（docker compose）上的端到端检查。
// 测试库由整个套件共享且可能留有历史数据，因此每轮都通过真实签发接口
// 生成一批随机坐标的新卡，并以其 id 严格递增来确认确实是新记录。

function coord() {
  return Math.floor(Math.random() * 10000);
}

// 连续签发 n 张新卡（过滤掉随机坐标撞上既有短码时的幂等返回）。
async function issueFreshCards(request, n) {
  const top = await request.get('/api/cards?limit=1');
  const topData = await top.json();
  let lastId = topData.cards.length ? topData.cards[0].id : 0;
  const cards = [];
  while (cards.length < n) {
    const res = await request.post('/api/cards', { data: { x: coord(), y: coord() } });
    expect(res.status()).toBe(201);
    const card = await res.json();
    if (card.id > lastId) {
      lastId = card.id;
      cards.push(card);
    }
  }
  return cards;
}

async function listedCodes(page) {
  return page.getByTestId('record-code').allTextContents();
}

test('签发记录按时间倒序展示坐标、短码与签发时间', async ({ page, request }) => {
  const cards = await issueFreshCards(request, 3);

  await page.goto('/#/records');
  const rows = page.getByTestId('record-row');
  await expect(rows.first()).toBeVisible();

  // 最新三张在最上方，按签发时间倒序（最新在前）。
  const expected = cards.map((c) => c.code).reverse();
  const codes = await listedCodes(page);
  expect(codes.slice(0, 3)).toEqual(expected);

  // 首行完整展示坐标与签发时间。
  const first = rows.first();
  await expect(first.getByTestId('record-coord')).toContainText(`X ${cards[2].x} 米`);
  await expect(first.getByTestId('record-coord')).toContainText(`Y ${cards[2].y} 米`);
  await expect(first.getByTestId('record-time')).not.toBeEmpty();
});

test('加载更早记录：多页顺序稳定，翻页期间新卡不进入序列', async ({ page, request }) => {
  // 超过两页（每页 20），保证翻页按钮出现且页间边界由自己的数据覆盖。
  const cards = await issueFreshCards(request, 42);
  const expected = cards.map((c) => c.code).reverse();

  await page.goto('/#/records');
  const rows = page.getByTestId('record-row');
  await expect(rows).toHaveCount(20);

  // 翻页期间签发一张新卡：不得插入当前浏览序列。
  const [intruder] = await issueFreshCards(request, 1);

  await page.getByTestId('load-more').click();
  await expect(rows).toHaveCount(40);

  let codes = await listedCodes(page);
  expect(codes).not.toContain(intruder.code);
  // 前 40 行仍是快照内最新的 40 张，倒序且无重复。
  expect(codes.slice(0, 40)).toEqual(expected.slice(0, 40));
  expect(new Set(codes).size).toBe(codes.length);

  // 继续翻到底（历史数据量不定，以响应里的 has_more 为准）：
  // 全程序列稳定、无重复，入侵卡始终不出现。
  const more = page.getByTestId('load-more');
  for (let i = 0; i < 50; i++) {
    if (!(await more.isVisible().catch(() => false))) break;
    const [resp] = await Promise.all([
      page.waitForResponse(
        (r) => r.url().includes('/api/cards') && r.request().method() === 'GET',
      ),
      more.click(),
    ]);
    if (!(await resp.json()).has_more) break;
  }
  await expect(page.getByTestId('records-end')).toBeVisible();
  codes = await listedCodes(page);
  expect(codes).not.toContain(intruder.code);
  expect(codes.slice(0, 42)).toEqual(expected);
  expect(new Set(codes).size).toBe(codes.length);
});

test('从记录带入短码到核验页：只预填不自动提交，手动核验还原唯一坐标', async ({
  page,
  request,
}) => {
  const [card] = await issueFreshCards(request, 1);

  await page.goto('/#/records');
  const first = page.getByTestId('record-row').first();
  await expect(first.getByTestId('record-code')).toHaveText(card.code);
  await first.getByTestId('carry-verify').click();

  // 跳转到核验页并预填短码，但不自动提交。
  await expect(page).toHaveURL(/#\/verify\?code=/);
  await expect(page.getByTestId('input-code')).toHaveValue(card.code);
  await expect(page.getByTestId('verify-result')).toHaveCount(0);
  await expect(page.getByTestId('verify-error')).toHaveCount(0);

  // 手动发起核验：真实链路还原唯一坐标。
  await page.getByRole('button', { name: '核验' }).click();
  await expect(page.getByTestId('verify-result')).toBeVisible();
  await expect(page.getByTestId('verify-x')).toHaveText(String(card.x));
  await expect(page.getByTestId('verify-y')).toHaveText(String(card.y));
  await expect(page.getByTestId('verify-code')).toHaveText(card.code);
  await expect(page.getByTestId('verify-issued')).toContainText('本台已签发');
});

test('翻页失败保留已加载内容并就地提示，可重试同一页', async ({ page, request }) => {
  const cards = await issueFreshCards(request, 25);

  await page.goto('/#/records');
  const rows = page.getByTestId('record-row');
  await expect(rows).toHaveCount(20);

  // 拦截下一次记录查询，模拟网络故障。
  const abortPattern = /\/api\/cards/;
  const abortHandler = (route) => route.abort();
  await page.route(abortPattern, abortHandler);
  await page.getByTestId('load-more').click();
  await expect(page.getByTestId('records-error')).toBeVisible();
  await expect(rows).toHaveCount(20, '失败时已加载内容必须保留');

  // 恢复网络后重试同一页：用原游标继续，不重不漏。
  await page.unroute(abortPattern, abortHandler);
  const [resp] = await Promise.all([
    page.waitForResponse(
      (r) => r.url().includes('/api/cards') && r.request().method() === 'GET',
    ),
    page.getByTestId('records-retry').click(),
  ]);
  expect(resp.ok()).toBeTruthy();
  await expect(rows.nth(20)).toBeVisible();
  await expect(page.getByTestId('records-error')).toHaveCount(0);
  const codes = await listedCodes(page);
  expect(codes.length).toBeGreaterThan(20);
  expect(codes.slice(0, 25)).toEqual(cards.map((c) => c.code).reverse());
  expect(new Set(codes).size).toBe(codes.length);
});
