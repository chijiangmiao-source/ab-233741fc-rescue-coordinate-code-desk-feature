// 与后端 server/shortcode/shortcode_test.go 使用同一组固定合法样例与篡改样例，
// 以此保证前后端调用的是同一套短码判据。
import { describe, expect, it } from 'vitest';
import {
  ChecksumError,
  FormatError,
  RangeError,
  checkChar,
  decode,
  encode,
  interleave,
  isValidCoordinate,
  parseCoordinate,
} from './shortcode';

const VALID_SAMPLES = [
  { x: 0, y: 0, code: '000000000' },
  { x: 1234, y: 5678, code: '152637483' },
  { x: 9999, y: 9999, code: '999999995' },
  { x: 3, y: 0, code: '00000030X' }, // 校验符余数为十，写作 X
  { x: 42, y: 7, code: '000040272' },
  { x: 500, y: 6000, code: '065000005' },
];

// 对合法短码 "152637483" 的固定单字符篡改样例。
const TAMPERED_SAMPLES = [
  '252637483', // 主体第 1 位被改
  '152637493', // 主体第 7 位被改
  '152637403', // 主体第 8 位被改
  '152637484', // 校验符被改成别的数字
  '15263748X', // 校验符被改成 X
];

describe('编码：固定合法样例', () => {
  for (const { x, y, code } of VALID_SAMPLES) {
    it(`(${x}, ${y}) → ${code}`, () => {
      const got = encode(x, y);
      expect(got).toBe(code);
      expect(got).toHaveLength(9);
    });
  }

  it('交错规则：x 千位、y 千位、x 百位、y 百位……直至个位', () => {
    expect(interleave(1234, 5678)).toBe('15263748');
    expect(interleave(3, 0)).toBe('00000030');
  });

  it('校验符：乘 1 至 8 求和模 11，余数十写作 X', () => {
    expect(checkChar('15263748')).toBe('3');
    expect(checkChar('00000030')).toBe('X');
    expect(checkChar('00000000')).toBe('0');
  });
});

describe('解码：固定合法样例还原唯一坐标', () => {
  for (const { x, y, code } of VALID_SAMPLES) {
    it(`${code} → (${x}, ${y})`, () => {
      expect(decode(code)).toEqual({ x, y });
    });
  }
});

describe('坐标范围', () => {
  it.each([-1, 10000, 10001, 1.5, NaN, Infinity])('encode 拒绝 %s', (v) => {
    expect(() => encode(v, 0)).toThrow(RangeError);
    expect(() => encode(0, v)).toThrow(RangeError);
  });

  it.each([0, 1, 9999])('isValidCoordinate 接受 %s', (v) => {
    expect(isValidCoordinate(v)).toBe(true);
  });

  it.each([
    ['1234', 1234],
    ['0003', 3],
    [' 42 ', 42],
  ])('parseCoordinate(%j) → %s', (raw, want) => {
    expect(parseCoordinate(raw)).toBe(want);
  });

  it.each(['', '10000', '-1', '1.5', 'abc', '1 2', '１２３'])(
    'parseCoordinate(%j) → null',
    (raw) => {
      expect(parseCoordinate(raw)).toBeNull();
    },
  );
});

describe('核验：固定篡改样例全部拒绝', () => {
  for (const code of TAMPERED_SAMPLES) {
    it(`${code} 被校验符拒绝`, () => {
      expect(() => decode(code)).toThrow(ChecksumError);
    });
  }

  it('穷举单字符篡改：任何一位换成任何其他合法字符都必须被拒绝', () => {
    const alphabet = '0123456789X';
    for (const { code } of VALID_SAMPLES) {
      for (let i = 0; i < code.length; i++) {
        for (const repl of alphabet) {
          if (repl === code[i]) continue;
          const mutated = code.slice(0, i) + repl + code.slice(i + 1);
          expect(() => decode(mutated), `${code} 第 ${i + 1} 位篡改为 ${repl}`).toThrow();
        }
      }
    }
  });
});

describe('核验：格式非法一律拒绝', () => {
  it.each([
    '',
    '15263748', // 少一位
    '1526374830', // 多一位
    '15263748x', // 小写 x 不接受
    '1526374 3', // 含空格
    'ABCDEFGHI', // 非数字主体
    '１５２６３７４８３', // 全角数字不接受
  ])('decode(%j) 抛出 FormatError', (code) => {
    expect(() => decode(code)).toThrow(FormatError);
  });
});
