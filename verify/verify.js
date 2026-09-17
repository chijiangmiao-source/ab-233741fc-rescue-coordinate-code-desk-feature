// 一次性验收服务：对运行中的 API 与前端做端到端检查，全部通过则以 0 退出。
// 所有断言都针对真实服务的真实响应，固定合法/篡改样例与
// server/shortcode、web/src 的测试保持一致。
const API = process.env.API_URL || 'http://api:8080';
const WEB = process.env.WEB_URL || 'http://web';

let failures = 0;

function check(name, cond, detail = '') {
  console.log(`${cond ? 'PASS' : 'FAIL'}  ${name}${detail ? ` — ${detail}` : ''}`);
  if (!cond) failures += 1;
}

async function post(url, body) {
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  const data = await res.json().catch(() => ({}));
  return { status: res.status, data };
}

async function waitFor(url, tries = 30) {
  for (let i = 0; i < tries; i++) {
    try {
      const res = await fetch(url);
      if (res.ok) return true;
    } catch {
      // 服务尚未就绪，继续等待
    }
    await new Promise((r) => setTimeout(r, 1000));
  }
  return false;
}

async function main() {
  check('等待 API 就绪', await waitFor(`${API}/api/health`));
  check('等待前端就绪', await waitFor(`${WEB}/`));
  if (failures) return;

  // 固定合法样例（与 testify / Vitest 相同）。
  const samples = [
    { x: 0, y: 0, code: '000000000' },
    { x: 1234, y: 5678, code: '152637483' },
    { x: 9999, y: 9999, code: '999999995' },
    { x: 3, y: 0, code: '00000030X' },
    { x: 42, y: 7, code: '000040272' },
    { x: 500, y: 6000, code: '065000005' },
  ];

  for (const s of samples) {
    const res = await post(`${API}/api/cards`, { x: s.x, y: s.y });
    check(
      `签发 (${s.x}, ${s.y}) → ${s.code}`,
      res.status === 201 &&
        res.data.code === s.code &&
        res.data.x === s.x &&
        res.data.y === s.y &&
        Boolean(res.data.issued_at),
      `HTTP ${res.status} ${JSON.stringify(res.data)}`,
    );
  }

  // 同一坐标重复签发：短码唯一，记录唯一。
  const again = await post(`${API}/api/cards`, { x: 1234, y: 5678 });
  check(
    '重复签发同一坐标返回同一短码',
    again.status === 201 && again.data.code === '152637483',
  );

  // 核验：同码还原唯一坐标，且能查到签发记录。
  const ok = await post(`${API}/api/verify`, { code: '152637483' });
  check(
    '核验 152637483 还原唯一坐标 (1234, 5678)',
    ok.status === 200 &&
      ok.data.valid === true &&
      ok.data.x === 1234 &&
      ok.data.y === 5678 &&
      ok.data.issued === true,
    `HTTP ${ok.status} ${JSON.stringify(ok.data)}`,
  );

  // 固定篡改样例：全部必须 422 拒绝，且响应不含坐标。
  const tampered = ['252637483', '152637493', '152637403', '152637484', '15263748X'];
  for (const code of tampered) {
    const res = await post(`${API}/api/verify`, { code });
    check(
      `篡改样例 ${code} 被拒绝`,
      res.status === 422 &&
        res.data.valid === false &&
        Boolean(res.data.error) &&
        !('x' in res.data) &&
        !('y' in res.data),
      `HTTP ${res.status}`,
    );
  }

  // 格式非法：一律 422。
  for (const code of ['', '15263748', '1526374830', '15263748x', 'ABCDEFGHI']) {
    const res = await post(`${API}/api/verify`, { code });
    check(`格式非法 ${JSON.stringify(code)} 被拒绝`, res.status === 422, `HTTP ${res.status}`);
  }

  // 越界 / 非整数坐标：一律 400。
  for (const body of [
    { x: 10000, y: 0 },
    { x: 0, y: 10000 },
    { x: -1, y: 0 },
    { x: 1.5, y: 0 },
    { x: 12 },
  ]) {
    const res = await post(`${API}/api/cards`, body);
    check(`非法坐标 ${JSON.stringify(body)} 被拒绝`, res.status === 400, `HTTP ${res.status}`);
  }

  // 前端静态页与 /api 反代（经 nginx 走完整链路）。
  const home = await fetch(`${WEB}/`);
  check('前端首页可访问', home.status === 200 && (await home.text()).includes('id="app"'));
  const proxied = await post(`${WEB}/api/verify`, { code: '00000030X' });
  check(
    '经前端反代核验 00000030X → (3, 0)',
    proxied.status === 200 && proxied.data.x === 3 && proxied.data.y === 0,
    `HTTP ${proxied.status}`,
  );
}

main()
  .catch((err) => {
    console.error(`验收服务异常: ${err.message}`);
    failures += 1;
  })
  .finally(() => {
    console.log(failures === 0 ? '验收通过' : `验收失败：${failures} 项未通过`);
    process.exit(failures === 0 ? 0 : 1);
  });
