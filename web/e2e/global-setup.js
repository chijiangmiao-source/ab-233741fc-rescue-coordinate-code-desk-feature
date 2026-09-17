// 运行前确认 docker compose 栈已就绪，避免把“服务没启动”误报成测试失败。
export default async function globalSetup() {
  const base =
    process.env.E2E_BASE_URL || `http://localhost:${process.env.WEB_PORT || '8080'}`;
  try {
    const res = await fetch(`${base}/api/health`);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
  } catch (err) {
    throw new Error(
      `E2E 需要完整栈已启动（${base} 不可达：${err.message}）。` +
        '请先运行 docker compose up -d，再执行 npm run test:e2e。',
    );
  }
}
