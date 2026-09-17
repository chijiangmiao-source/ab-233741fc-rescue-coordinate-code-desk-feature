// 与 Gin API 通信的薄封装。所有错误反馈都来自服务端真实响应，
// 不打桩、不返回固定结果。
const BASE = '/api';

async function request(method, path, payload) {
  let res;
  try {
    res = await fetch(`${BASE}${path}`, {
      method,
      headers: payload === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: payload === undefined ? undefined : JSON.stringify(payload),
    });
  } catch {
    throw new Error('无法连接短码服务，请检查网络或稍后重试');
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    const err = new Error(data.error || `服务返回异常（HTTP ${res.status}）`);
    // 游标类错误码供前端区分“重试本页”与“重新打开新快照”。
    err.code = data.code || '';
    throw err;
  }
  return data;
}

function post(path, payload) {
  return request('POST', path, payload);
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
 * 查询签发记录（只读键集分页）。
 * 不带 cursor 取首批并由服务端确定快照边界；之后回传上一页的 next_cursor。
 * 返回 { cards, has_more, next_cursor, snapshot_id? }。
 */
export function listCards({ limit, cursor } = {}) {
  const q = new URLSearchParams();
  if (limit !== undefined) q.set('limit', String(limit));
  if (cursor) q.set('cursor', cursor);
  const suffix = q.toString() ? `?${q.toString()}` : '';
  return request('GET', `/cards${suffix}`);
}
