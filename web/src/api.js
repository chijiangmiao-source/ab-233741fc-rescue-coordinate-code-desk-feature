// 与 Gin API 通信的薄封装。所有错误反馈都来自服务端真实响应，
// 不打桩、不返回固定结果。
const BASE = '/api';

async function handle(res) {
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data.error || `服务返回异常（HTTP ${res.status}）`);
  }
  return data;
}

async function post(path, payload) {
  let res;
  try {
    res = await fetch(`${BASE}${path}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
  } catch {
    throw new Error('无法连接短码服务，请检查网络或稍后重试');
  }
  return handle(res);
}

async function get(path, params) {
  const qs = new URLSearchParams(params).toString();
  let res;
  try {
    res = await fetch(`${BASE}${path}${qs ? `?${qs}` : ''}`);
  } catch {
    throw new Error('无法连接短码服务，请检查网络或稍后重试');
  }
  return handle(res);
}

/** 签发坐标卡：成功时返回 { id, x, y, code, issued_at }。 */
export function issueCard(x, y) {
  return post('/cards', { x, y });
}

/** 核验短码：成功时返回 { valid, x, y, code, issued, issued_at? }。 */
export function verifyCode(code) {
  return post('/verify', { code });
}

/**
 * 查询签发记录（只读）：按签发时间倒序。
 * 首批查询只传 limit；翻页时回传上一页返回的 snapshot 与 cursor。
 * 成功时返回 { cards, snapshot, next_cursor, has_more }。
 */
export function listCards({ limit, snapshot, cursor } = {}) {
  const params = {};
  if (limit) params.limit = String(limit);
  if (snapshot) params.snapshot = snapshot;
  if (cursor) params.cursor = cursor;
  return get('/cards', params);
}
