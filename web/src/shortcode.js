// 山地搜救坐标短码判据 —— 与后端 server/shortcode/shortcode.go 为同一套判据，
// 双方使用相同的固定合法样例与篡改样例测试（见 shortcode.test.js）。
//
// 判据：
//   - 输入只接受 x、y 各为 0 至 9999 的十进制整数米，不做单位换算或坐标归一化；
//   - x、y 各补足四位后，按 x 千位、y 千位、x 百位、y 百位……直至个位交错，
//     得到八位主体；
//   - 主体从左到右八个数字分别乘 1 至 8，求和除以 11 的余数作为校验符，
//     余数十写作 X，其余写对应数字；
//   - 最终短码固定九字符：八位主体 + 一位校验符。

export const MAX_COORD = 9999;
export const CODE_LENGTH = 9;

const CODE_RE = /^[0-9]{8}[0-9X]$/;
const COORD_RE = /^\d{1,4}$/;

export class RangeError extends Error {
  constructor(message) {
    super(message);
    this.name = 'CoordRangeError';
  }
}

export class FormatError extends Error {
  constructor(message) {
    super(message);
    this.name = 'CodeFormatError';
  }
}

export class ChecksumError extends Error {
  constructor(message) {
    super(message);
    this.name = 'CodeChecksumError';
  }
}

/** 坐标是否为 0 至 9999 的整数。 */
export function isValidCoordinate(v) {
  return Number.isInteger(v) && v >= 0 && v <= MAX_COORD;
}

/**
 * 把用户输入解析为坐标；非法输入返回 null。
 * 只接受一至四位十进制数字（允许前导零），不接受小数、负号、空白以外的字符。
 */
export function parseCoordinate(raw) {
  const s = String(raw).trim();
  if (!COORD_RE.test(s)) return null;
  const n = Number(s);
  return isValidCoordinate(n) ? n : null;
}

/** v 补足四位后的十进制数字数组（千、百、十、个）。 */
function digits4(v) {
  return [
    Math.floor(v / 1000) % 10,
    Math.floor(v / 100) % 10,
    Math.floor(v / 10) % 10,
    v % 10,
  ];
}

/** x、y 各补足四位后按位交错：x 千位、y 千位、x 百位、y 百位……直至个位。 */
export function interleave(x, y) {
  const xd = digits4(x);
  const yd = digits4(y);
  let body = '';
  for (let i = 0; i < 4; i++) body += String(xd[i]) + String(yd[i]);
  return body;
}

/** 八位主体的校验符：从左到右乘 1 至 8 求和，模 11，余数十写作 X。 */
export function checkChar(body) {
  let sum = 0;
  for (let i = 0; i < 8; i++) sum += Number(body[i]) * (i + 1);
  const r = sum % 11;
  return r === 10 ? 'X' : String(r);
}

/** 将坐标 (x, y) 编码为九字符短码；坐标越界时抛出 RangeError。 */
export function encode(x, y) {
  if (!isValidCoordinate(x) || !isValidCoordinate(y)) {
    throw new RangeError('坐标超出范围：x、y 须为 0 至 9999 的整数米');
  }
  const body = interleave(x, y);
  return body + checkChar(body);
}

/**
 * 核验短码并还原唯一坐标。
 * 格式或校验失败时抛出 FormatError / ChecksumError，绝不还原坐标。
 */
export function decode(code) {
  if (typeof code !== 'string' || !CODE_RE.test(code)) {
    throw new FormatError('短码格式无效：须为八位数字加一位 0-9 或 X 的校验符');
  }
  const body = code.slice(0, 8);
  if (checkChar(body) !== code[8]) {
    throw new ChecksumError('校验符不匹配：短码可能被抄错或篡改，拒绝还原坐标');
  }
  const d = [...body].map(Number);
  return {
    x: d[0] * 1000 + d[2] * 100 + d[4] * 10 + d[6],
    y: d[1] * 1000 + d[3] * 100 + d[5] * 10 + d[7],
  };
}
