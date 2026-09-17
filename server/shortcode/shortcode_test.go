package shortcode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 固定合法样例：与前端 web/src/shortcode.test.js、验收服务 verify/verify.js
// 使用同一组样例，保证前后端判据一致。
var validSamples = []struct {
	x, y int
	code string
}{
	{0, 0, "000000000"},
	{1234, 5678, "152637483"},
	{9999, 9999, "999999995"},
	{3, 0, "00000030X"}, // 校验符余数为十，写作 X
	{42, 7, "000040272"},
	{500, 6000, "065000005"},
}

// 固定篡改样例：对合法短码 "152637483" 的单字符篡改，必须全部被拒绝。
var tamperedSamples = []string{
	"252637483", // 主体第 1 位被改
	"152637493", // 主体第 7 位被改
	"152637403", // 主体第 8 位被改
	"152637484", // 校验符被改成别的数字
	"15263748X", // 校验符被改成 X
}

func TestEncodeValidSamples(t *testing.T) {
	for _, s := range validSamples {
		code, err := Encode(s.x, s.y)
		require.NoError(t, err)
		assert.Equal(t, s.code, code, "Encode(%d, %d)", s.x, s.y)
		assert.Len(t, code, CodeLength)
	}
}

func TestDecodeValidSamples(t *testing.T) {
	for _, s := range validSamples {
		x, y, err := Decode(s.code)
		require.NoError(t, err, "Decode(%q)", s.code)
		assert.Equal(t, s.x, x, "Decode(%q).x", s.code)
		assert.Equal(t, s.y, y, "Decode(%q).y", s.code)
	}
}

// 同一坐标编码结果唯一，同一短码还原坐标唯一。
func TestRoundTripIsUnique(t *testing.T) {
	for _, s := range validSamples {
		code, err := Encode(s.x, s.y)
		require.NoError(t, err)
		x, y, err := Decode(code)
		require.NoError(t, err)
		assert.Equal(t, s.x, x)
		assert.Equal(t, s.y, y)
	}
}

func TestEncodeRejectsOutOfRange(t *testing.T) {
	for _, c := range [][2]int{{-1, 0}, {0, -1}, {10000, 0}, {0, 10000}, {-1, 9999}, {10000, 10000}} {
		_, err := Encode(c[0], c[1])
		assert.ErrorIs(t, err, ErrRange, "Encode(%d, %d)", c[0], c[1])
	}
}

func TestDecodeRejectsTamperedSamples(t *testing.T) {
	for _, code := range tamperedSamples {
		_, _, err := Decode(code)
		assert.ErrorIs(t, err, ErrChecksum, "Decode(%q)", code)
	}
}

// 穷举单字符篡改：对全部固定合法样例，任意一个字符被替换成任何其他
// 合法字符（0-9 或 X）都必须被判据发现，绝不还原坐标。
func TestDecodeRejectsEverySingleCharTamper(t *testing.T) {
	for _, s := range validSamples {
		for i := 0; i < CodeLength; i++ {
			for _, repl := range "0123456789X" {
				if byte(repl) == s.code[i] {
					continue
				}
				mut := s.code[:i] + string(repl) + s.code[i+1:]
				_, _, err := Decode(mut)
				assert.Error(t, err, "Decode(%q)（由 %s 篡改第 %d 位）", mut, s.code, i+1)
			}
		}
	}
}

func TestDecodeRejectsMalformed(t *testing.T) {
	malformed := []string{
		"",
		"15263748",   // 少一位
		"1526374830", // 多一位
		"15263748x",  // 小写 x 不接受
		"1526374 3",  // 含空格
		"ABCDEFGHI",  // 非数字主体
		"１５２６３７４８３",  // 全角数字不接受
	}
	for _, code := range malformed {
		_, _, err := Decode(code)
		assert.ErrorIs(t, err, ErrFormat, "Decode(%q)", code)
	}
}
