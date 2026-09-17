import { expect, test } from '@playwright/test';

// 固定合法样例（与后端 testify、前端 Vitest、verify 验收服务相同）。
const VALID = { x: '1234', y: '5678', code: '152637483' };
const VALID_X_CHECK = { x: '3', y: '0', code: '00000030X' };
// 固定篡改样例：VALID.code 主体第 7 位 4 → 9。
const TAMPERED = '152637493';

async function issue(page, x, y) {
  await page.goto('/#/issue');
  await page.getByTestId('input-x').fill(x);
  await page.getByTestId('input-y').fill(y);
  await page.getByRole('button', { name: '签发' }).click();
}

test('签发坐标卡：展示原坐标、短码与签发时间', async ({ page }) => {
  await page.goto('/#/issue');
  await page.getByTestId('input-x').fill(VALID.x);
  await page.getByTestId('input-y').fill(VALID.y);

  // 本地预览与服务端使用同一套判据
  await expect(page.getByTestId('code-preview')).toHaveText(VALID.code);

  await page.getByRole('button', { name: '签发' }).click();
  await expect(page.getByTestId('issued-code')).toHaveText(VALID.code);
  await expect(page.getByTestId('issued-x')).toHaveText('1234');
  await expect(page.getByTestId('issued-y')).toHaveText('5678');
  await expect(page.getByTestId('issued-at')).not.toBeEmpty();
});

test('签发含 X 校验符的短码', async ({ page }) => {
  await issue(page, VALID_X_CHECK.x, VALID_X_CHECK.y);
  await expect(page.getByTestId('issued-code')).toHaveText(VALID_X_CHECK.code);
});

test('核验合法短码得到唯一坐标', async ({ page }) => {
  await issue(page, VALID.x, VALID.y);
  await expect(page.getByTestId('issued-code')).toHaveText(VALID.code);

  await page.goto('/#/verify');
  await page.getByTestId('input-code').fill(VALID.code);
  await page.getByRole('button', { name: '核验' }).click();

  await expect(page.getByTestId('verify-result')).toBeVisible();
  await expect(page.getByTestId('verify-x')).toHaveText('1234');
  await expect(page.getByTestId('verify-y')).toHaveText('5678');
  await expect(page.getByTestId('verify-code')).toHaveText(VALID.code);
});

test('单数字篡改被明确拒绝，且不还原坐标', async ({ page }) => {
  await page.goto('/#/verify');
  await page.getByTestId('input-code').fill(TAMPERED);
  await page.getByRole('button', { name: '核验' }).click();

  await expect(page.getByTestId('verify-error')).toBeVisible();
  await expect(page.getByTestId('verify-error')).toContainText('校验符');
  await expect(page.getByTestId('verify-result')).toHaveCount(0);
});

test('格式非法的短码被明确拒绝', async ({ page }) => {
  await page.goto('/#/verify');
  await page.getByTestId('input-code').fill('15263748'); // 只有八位
  await page.getByRole('button', { name: '核验' }).click();

  await expect(page.getByTestId('verify-error')).toBeVisible();
  await expect(page.getByTestId('verify-error')).toContainText('格式');
  await expect(page.getByTestId('verify-result')).toHaveCount(0);
});

test('越界坐标被拒绝', async ({ page }) => {
  await page.goto('/#/issue');
  await page.getByTestId('input-x').fill('10000');
  await page.getByTestId('input-y').fill('0');
  await page.getByRole('button', { name: '签发' }).click();

  await expect(page.getByTestId('issue-error')).toBeVisible();
  await expect(page.getByTestId('issued-card')).toHaveCount(0);
});
