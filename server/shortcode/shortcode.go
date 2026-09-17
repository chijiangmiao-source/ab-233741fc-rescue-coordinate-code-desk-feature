// Package shortcode 实现山地搜救坐标短码的编码与核验判据。
//
// 判据（与前端 web/src/shortcode.js 为同一套，双方用相同固定样例测试）：
//   - 输入只接受 x、y 各为 0 至 9999 的十进制整数米，不做单位换算或坐标归一化；
//   - x、y 各补足四位后，按 x 千位、y 千位、x 百位、y 百位……直至个位交错，
//     得到八位主体；
//   - 主体从左到右八个数字分别乘 1 至 8，求和除以 11 的余数作为校验符，
//     余数十写作 X，其余写对应数字；
//   - 最终短码固定九字符：八位主体 + 一位校验符。
package shortcode

import "errors"

// MaxCoord 是坐标允许的最大值（单位：米）。
const MaxCoord = 9999

// CodeLength 是短码的固定长度：八位主体 + 一位校验符。
const CodeLength = 9

var (
	// ErrRange 表示坐标超出 0 至 9999 的范围。
	ErrRange = errors.New("坐标超出范围：x、y 须为 0 至 9999 的整数米")
	// ErrFormat 表示短码格式非法（不是八位数字加一位 0-9 或 X）。
	ErrFormat = errors.New("短码格式无效：须为八位数字加一位 0-9 或 X 的校验符")
	// ErrChecksum 表示校验符与主体不匹配（抄录错误或遭篡改）。
	ErrChecksum = errors.New("校验符不匹配：短码可能被抄错或篡改，拒绝还原坐标")
)

// Encode 将坐标 (x, y) 编码为九字符短码。
func Encode(x, y int) (string, error) {
	if x < 0 || x > MaxCoord || y < 0 || y > MaxCoord {
		return "", ErrRange
	}
	body := interleave(x, y)
	return body + string(checkChar(body)), nil
}

// Decode 核验短码并还原唯一坐标。
// 格式或校验失败时返回错误，绝不还原坐标。
func Decode(code string) (x, y int, err error) {
	if len(code) != CodeLength {
		return 0, 0, ErrFormat
	}
	for i := 0; i < CodeLength-1; i++ {
		if code[i] < '0' || code[i] > '9' {
			return 0, 0, ErrFormat
		}
	}
	last := code[CodeLength-1]
	if (last < '0' || last > '9') && last != 'X' {
		return 0, 0, ErrFormat
	}
	body := code[:CodeLength-1]
	if checkChar(body) != last {
		return 0, 0, ErrChecksum
	}
	x = int(body[0]-'0')*1000 + int(body[2]-'0')*100 + int(body[4]-'0')*10 + int(body[6]-'0')
	y = int(body[1]-'0')*1000 + int(body[3]-'0')*100 + int(body[5]-'0')*10 + int(body[7]-'0')
	return x, y, nil
}

// interleave 将 x、y 各补足四位后按位交错：
// x 千位、y 千位、x 百位、y 百位、x 十位、y 十位、x 个位、y 个位。
func interleave(x, y int) string {
	xd, yd := digits4(x), digits4(y)
	body := make([]byte, 0, 8)
	for i := 0; i < 4; i++ {
		body = append(body, xd[i], yd[i])
	}
	return string(body)
}

// digits4 返回 v 补足四位后的十进制数字（千、百、十、个）。
func digits4(v int) []byte {
	return []byte{
		byte('0' + v/1000%10),
		byte('0' + v/100%10),
		byte('0' + v/10%10),
		byte('0' + v%10),
	}
}

// checkChar 计算八位主体的校验符：
// 从左到右八个数字分别乘 1 至 8 求和，除以 11 取余数；余数十写作 X。
func checkChar(body string) byte {
	sum := 0
	for i := 0; i < len(body); i++ {
		sum += int(body[i]-'0') * (i + 1)
	}
	if r := sum % 11; r == 10 {
		return 'X'
	} else {
		return byte('0' + r)
	}
}
