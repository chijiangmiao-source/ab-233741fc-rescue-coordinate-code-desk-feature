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

async function get(url) {
  const res = await fetch(url);
  const data = await res.json().catch(() => ({}));
  return { status: res.status, data };
}

// 沿不透明游标翻完整个快照（每页 limit 条），返回页序列与快照编号。
// 全程不解析游标内容，只原样回传服务端签发的 next_cursor。
async function walkSnapshot(limit, cursor = '', pages = []) {
  const q = new URLSearchParams({ limit: String(limit) });
  if (cursor) q.set('cursor', cursor);
  const res = await get(`${API}/api/cards?${q}`);
  if (res.status !== 200) return { pages, snapshotId: null, failed: res };
  pages.push(res.data);
  if (!res.data.next_cursor) {
    return { pages, snapshotId: res.data.snapshot_id ?? pages[0]?.snapshot_id ?? null };
  }
  return walkSnapshot(limit, res.data.next_cursor, pages);
}

function flatten(pages) {
  return pages.flatMap((p) => p.cards);
}

// 翻转游标末位字符，得到一个必然过不了 HMAC 的伪造游标。
function flipCursorLast(token) {
  const last = token.slice(-1);
  return token.slice(0, -1) + (last === 'A' ? 'B' : 'A');
}

// 校验序列严格按 (issued_at 倒序, id 倒序) 且短码无重复。
function isStableDescending(rows) {
  const codes = new Set();
  for (let i = 0; i < rows.length; i++) {
    if (codes.has(rows[i].code)) return false;
    codes.add(rows[i].code);
    if (i === 0) continue;
    const a = rows[i - 1];
    const b = rows[i];
    if (a.issued_at < b.issued_at) return false;
    if (a.issued_at === b.issued_at && a.id <= b.id) return false;
  }
  return true;
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

  // ── 签发记录只读查询与快照键集分页 ─────────────────────────────────
  // 用与前面 6 个固定样例不同的坐标区间（8xxx）签发 7 张专用卡，
  // 确保跨多次验收运行也能产生新行（重复短码会去重为旧行）。
  const pagedCards = [];
  for (let i = 1; i <= 7; i++) {
    const res = await post(`${API}/api/cards`, { x: 8000 + i, y: 8500 + i });
    check(
      `记录验收：签发 (${8000 + i}, ${8500 + i})`,
      res.status === 201 && Boolean(res.data.id) && Boolean(res.data.issued_at),
      `HTTP ${res.status}`,
    );
    pagedCards.push(res.data);
  }
  const pagedSet = new Set(pagedCards.map((c) => c.code));

  // 1) 多页顺序稳定：每页 3 条翻完，严格 (issued_at DESC, id DESC)、无重复。
  const walk = await walkSnapshot(3);
  check(
    '记录查询：多页全部成功返回',
    !walk.failed && walk.pages.length >= 3,
    walk.failed ? `HTTP ${walk.failed.status}` : `页数=${walk.pages.length}`,
  );
  const allRows = flatten(walk.pages);
  check(
    '记录查询：序列严格倒序且无重复',
    isStableDescending(allRows),
    `行数=${allRows.length}`,
  );
  // 每个中间页之后继续加载都能接续上：页间无重叠、无遗漏（用 id 集合差集）。
  const allIds = allRows.map((c) => c.id);
  check(
    '记录查询：多页合计不重不漏',
    new Set(allIds).size === allIds.length &&
      pagedCards.every((c) => allIds.includes(c.id)),
    `唯一 id 数=${new Set(allIds).size}`,
  );
  // 页大小 3：除最后一页外每页都恰好 3 条。
  const fullPages = walk.pages.slice(0, -1);
  check(
    '记录查询：非尾页每页恰好 3 条',
    fullPages.every((p) => p.cards.length === 3) &&
      walk.pages[walk.pages.length - 1].cards.length >= 1,
  );

  // 2) 同一游标可重放（失败后重试同一页）：重复请求第一个 next_cursor。
  const cursor0 = walk.pages[0].next_cursor;
  const replayA = await get(`${API}/api/cards?limit=3&cursor=${encodeURIComponent(cursor0)}`);
  const replayB = await get(`${API}/api/cards?limit=3&cursor=${encodeURIComponent(cursor0)}`);
  check(
    '记录查询：同一游标重试返回同一页',
    replayA.status === 200 &&
      replayB.status === 200 &&
      JSON.stringify(replayA.data) === JSON.stringify(replayB.data),
  );

  // 3) 翻页期间新签发的卡不影响快照：沿首个 next_cursor 把后续页全部走完，
  // 再签发一张编号大于快照边界的新卡。原游标链各页都不出现它、后续页不重
  // 不漏；重新打开（无游标）的新快照第一位就是这张新卡。
  const snapshotId = walk.snapshotId;
  // 重复跑验收时固定坐标会因短码去重返回旧行；这里尝试多组坐标，直到拿到
  // 编号确实大于快照边界（即快照后新签发）的卡。
  let late = null;
  const seed = (Date.now() % 9000) + 50;
  for (let i = 0; i < 30 && !late; i++) {
    const x = (seed + i) % 9000;
    const y = (seed * 3 + i * 7) % 9000;
    const r = await post(`${API}/api/cards`, { x, y });
    if (r.status === 201 && r.data.id > snapshotId) late = r.data;
  }
  check('记录验收：取得快照后新签发的卡', late !== null);

  const firstPageIds = new Set(walk.pages[0].cards.map((c) => c.id));
  const expectedRestIds = new Set(allIds.filter((id) => !firstPageIds.has(id)));
  const walk2 = await walkSnapshot(3, cursor0);
  const rows2 = flatten(walk2.pages);
  const restIds = new Set(rows2.map((c) => c.id));
  check(
    '记录查询：翻页期间新卡不插入旧快照，后续页不重不漏',
    !walk2.failed &&
      late &&
      rows2.every((c) => c.id !== late.id) &&
      walk2.pages.every((p) => p.snapshot_id === snapshotId) &&
      restIds.size === rows2.length &&
      restIds.size === expectedRestIds.size &&
      [...expectedRestIds].every((id) => restIds.has(id)),
  );
  const freshFirst = await get(`${API}/api/cards?limit=3`);
  check(
    '记录查询：重新打开的新快照最新位是新卡、边界编号更新',
    freshFirst.status === 200 &&
      freshFirst.data.cards[0].id === late.id &&
      freshFirst.data.snapshot_id === late.id,
  );
  check(
    '记录查询：旧首批响应带快照边界编号且边界在序列首位',
    typeof snapshotId === 'number' && allRows[0].id === snapshotId,
  );

  // 4) 非法/过期/跨快照游标：一律 400，响应不含任何记录。
  const forged = ['forged', 'aGVsbG8', cursor0.slice(0, -1) + flipCursorLast(cursor0)];
  for (const cur of forged) {
    const res = await get(`${API}/api/cards?limit=3&cursor=${encodeURIComponent(cur)}`);
    check(
      `记录查询：非法游标 ${JSON.stringify(cur.slice(0, 12))} 被拒绝且不泄露记录`,
      res.status === 400 && !('cards' in res.data) && Boolean(res.data.error),
      `HTTP ${res.status}`,
    );
  }
  // 每页数量限制：0、负数、超上限、非数字均 400。
  for (const lim of ['0', '-1', '51', 'abc']) {
    const res = await get(`${API}/api/cards?limit=${lim}`);
    check(`记录查询：limit=${lim} 被拒绝`, res.status === 400 && !('cards' in res.data));
  }

  // 5) 从记录带入有效短码后，仍由真实核验链路还原唯一坐标。
  //    （前端“带入”只预填不提交；这里直接验证记录中的合法短码经 /verify
  //    能还原出与列表完全一致的坐标，且查到签发记录。）
  const sample = allRows.find((r) => pagedSet.has(r.code));
  const verified = await post(`${API}/api/verify`, { code: sample.code });
  check(
    `记录查询：列表短码 ${sample.code} 经真实核验还原唯一坐标`,
    verified.status === 200 &&
      verified.data.valid === true &&
      verified.data.x === sample.x &&
      verified.data.y === sample.y &&
      verified.data.issued === true,
    `HTTP ${verified.status} ${JSON.stringify(verified.data)}`,
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
