import { defineConfig } from '@playwright/test';

const webPort = process.env.WEB_PORT || '8080';

// E2E 针对 docker compose 启动的真实前后端运行，错误反馈全部来自真实 API，
// 不打桩、不用固定结果。启动方式见 README：docker compose up -d
export default defineConfig({
  testDir: './e2e',
  globalSetup: './e2e/global-setup.js',
  timeout: 30_000,
  retries: 0,
  workers: 1, // 用例共享同一个 SQLite 库，串行执行避免相互干扰
  use: {
    baseURL: process.env.E2E_BASE_URL || `http://localhost:${webPort}`,
  },
});
