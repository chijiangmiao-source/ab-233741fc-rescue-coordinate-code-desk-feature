// 一次性验收服务：对运行中的 API 与前端做端到端检查，全部通过则以 0 退出。
// 所有断言都针对真实服务的真实响应，固定合法/篡改样例与
// server/shortcode、web/src 的测试保持一致。
const { randomInt } = require('node:crypto');

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

async function get(url) {
  const res = await fetch(url);
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

// 签发一张确定是“新”的卡：随机坐标撞上既有短码时会被幂等返回旧记录，
// 以返回 id 严格大于当前最大 id 来识别并重试。
async function issueFreshCard(minId) {
  for (;;) {
    const x = randomInt(10000);
    const y = randomInt(10000);
    const res = await post(`${API}/api/cards`, { x, y });
    if (res.status === 201 && res.data.id > minId) return res.data;
    await new Promise((r) => setTimeout(r, 50));
  }
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

  // —— 签发记录：快照边界 + 不透明游标键集分页 ——

  // 记录当前最大 id，随后连续签发 5 张确定是新记录的卡。
  const top = await get(`${API}/api/cards?limit=1`);
  check(
    '记录首页可访问',
    top.status === 200 && Array.isArray(top.data.cards) && typeof top.data.snapshot === 'string',
    `HTTP ${top.status}`,
  );
  let maxId = top.data.cards.length ? top.data.cards[0].id : 0;
  const mine = [];
  for (let i = 0; i < 5; i++) {
    const card = await issueFreshCard(maxId);
    maxId = card.id;
    mine.push(card);
  }

  // 首批查询确定快照边界：最新的卡在最上方，按签发时间倒序。
  const p1 = await get(`${API}/api/cards?limit=2`);
  check(
    '记录首页倒序展示最新签发',
    p1.status === 200 &&
      p1.data.cards.length === 2 &&
      p1.data.cards[0].id === mine[4].id &&
      p1.data.cards[1].id === mine[3].id &&
      p1.data.has_more === true &&
      Boolean(p1.data.snapshot) &&
      Boolean(p1.data.next_cursor),
    `HTTP ${p1.status} ${JSON.stringify(p1.data).slice(0, 200)}`,
  );
  const snap = encodeURIComponent(p1.data.snapshot);
  const cur1 = encodeURIComponent(p1.data.next_cursor);

  // 翻页期间签发新卡：不得插入当前浏览序列。
  const intruder = await issueFreshCard(maxId);
  const p2 = await get(`${API}/api/cards?limit=2&snapshot=${snap}&cursor=${cur1}`);
  check(
    '翻页期间新卡不进入序列，键集分页不重不漏',
    p2.status === 200 &&
      p2.data.cards.length === 2 &&
      p2.data.cards[0].id === mine[2].id &&
      p2.data.cards[1].id === mine[1].id &&
      !p2.data.cards.some((c) => c.code === intruder.code),
    `HTTP ${p2.status} ${JSON.stringify(p2.data).slice(0, 200)}`,
  );
  const p3 = await get(
    `${API}/api/cards?limit=2&snapshot=${snap}&cursor=${encodeURIComponent(p2.data.next_cursor)}`,
  );
  check(
    '第三页接续快照序列',
    p3.status === 200 && p3.data.cards.length >= 1 && p3.data.cards[0].id === mine[0].id,
    `HTTP ${p3.status} ${JSON.stringify(p3.data).slice(0, 200)}`,
  );

  // 沿游标继续翻页（页数设上限，历史数据可能很多）：全程 id 严格递减、
  // 无重复、入侵卡不出现，且所有记录都在快照边界内。
  let cursor = p3.data.next_cursor;
  let lastId = p3.data.cards.length ? p3.data.cards[p3.data.cards.length - 1].id : 0;
  const seen = new Set([...p1.data.cards, ...p2.data.cards, ...p3.data.cards].map((c) => c.id));
  let walkOk = p3.status === 200 && !p3.data.cards.some((c) => c.code === intruder.code);
  for (let i = 0; i < 20 && cursor; i++) {
    const page = await get(
      `${API}/api/cards?limit=7&snapshot=${snap}&cursor=${encodeURIComponent(cursor)}`,
    );
    if (page.status !== 200) {
      walkOk = false;
      break;
    }
    for (const c of page.data.cards) {
      if (c.id >= lastId || seen.has(c.id) || c.id > maxId || c.code === intruder.code) {
        walkOk = false;
      }
      seen.add(c.id);
      lastId = c.id;
    }
    cursor = page.data.has_more ? page.data.next_cursor : null;
  }
  check('多页遍历：id 严格递减、无重复、无越界记录', walkOk);

  // 控制组：重新取首批（新快照）应能看到翻页期间签发的新卡。
  const fresh = await get(`${API}/api/cards?limit=1`);
  check(
    '新快照能看到翻页期间签发的卡',
    fresh.status === 200 && fresh.data.cards.length === 1 && fresh.data.cards[0].code === intruder.code,
    `HTTP ${fresh.status}`,
  );

  // 非法游标：垃圾、篡改签名、伪造载荷、缺快照、跨快照混用——
  // 一律 400 且响应不含任何记录。
  const tamperedCursor = `${p1.data.next_cursor.slice(0, -1)}${
    p1.data.next_cursor.endsWith('A') ? 'B' : 'A'
  }`;
  const forgedCursor = `${Buffer.from(
    JSON.stringify({ k: 'c', st: '2026-09-17T00:00:00Z', sd: 1, lt: '2026-09-17T00:00:00Z', ld: 1, exp: 9999999999 }),
  ).toString('base64url')}.AAAA`;
  const badCursorCases = [
    ['垃圾游标', `${API}/api/cards?snapshot=${snap}&cursor=not-a-token`],
    ['篡改签名的游标', `${API}/api/cards?snapshot=${snap}&cursor=${encodeURIComponent(tamperedCursor)}`],
    ['伪造载荷的游标', `${API}/api/cards?snapshot=${snap}&cursor=${forgedCursor}`],
    ['缺少快照标识', `${API}/api/cards?cursor=${cur1}`],
    ['跨快照混用的游标', `${API}/api/cards?snapshot=${encodeURIComponent(fresh.data.snapshot)}&cursor=${cur1}`],
  ];
  for (const [name, url] of badCursorCases) {
    const res = await get(url);
    check(
      `非法游标被拒绝：${name}`,
      res.status === 400 && Boolean(res.data.error) && !('cards' in res.data),
      `HTTP ${res.status}`,
    );
  }

  // 非法每页数量被拒绝；超过上限按上限截断。
  const badLimit = await get(`${API}/api/cards?limit=0`);
  check('非法每页数量被拒绝', badLimit.status === 400 && Boolean(badLimit.data.error));
  const clamped = await get(`${API}/api/cards?limit=999`);
  check(
    '每页数量超过上限被截断',
    clamped.status === 200 && clamped.data.cards.length <= 50,
    `HTTP ${clamped.status}`,
  );

  // 从记录中取出的短码，经真实核验链路还原唯一坐标。
  const listed = p1.data.cards[0];
  const carried = await post(`${API}/api/verify`, { code: listed.code });
  check(
    `记录短码 ${listed.code} 经核验链路还原唯一坐标`,
    carried.status === 200 &&
      carried.data.valid === true &&
      carried.data.x === listed.x &&
      carried.data.y === listed.y &&
      carried.data.issued === true,
    `HTTP ${carried.status} ${JSON.stringify(carried.data)}`,
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
