// 与 Gin API 通信的薄封装。所有错误反馈都来自服务端真实响应，
// 不打桩、不返回固定结果。
const BASE = '/api';

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
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data.error || `服务返回异常（HTTP ${res.status}）`);
  }
  return data;
}

/** 签发坐标卡：成功时返回 { id, x, y, code, issued_at }。 */
export function issueCard(x, y) {
  return post('/cards', { x, y });
}

/** 核验短码：成功时返回 { valid, x, y, code, issued, issued_at? }。 */
export function verifyCode(code) {
  return post('/verify', { code });
}
