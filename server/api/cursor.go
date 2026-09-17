// 记录查询的不透明游标：快照边界（首批查询确定的时间与编号）与键集位置
// 都封装在带 HMAC-SHA256 签名的令牌里，客户端无法伪造或篡改；
// 令牌自带过期时间，超时后必须重新加载。
package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// 令牌种类：快照令牌（首批查询返回，标记浏览序列的边界）与翻页游标
// （在快照内标记键集位置）。两者用同一套签名，但不可混用。
const (
	tokenKindSnapshot = "s"
	tokenKindCursor   = "c"
)

var (
	// errTokenInvalid 表示令牌格式或签名不符（伪造、篡改、损坏）。
	errTokenInvalid = errors.New("记录游标无效或已损坏，请重新加载签发记录")
	// errTokenExpired 表示令牌已过有效期。
	errTokenExpired = errors.New("记录游标已过期，请重新加载签发记录")
	// errTokenMismatch 表示翻页游标与当前快照不属于同一次浏览序列。
	errTokenMismatch = errors.New("记录游标与当前快照不匹配，请重新加载签发记录")
)

// pageToken 是快照令牌与翻页游标的共同载荷。
type pageToken struct {
	Kind     string `json:"k"`            // tokenKindSnapshot 或 tokenKindCursor
	SnapTime string `json:"st"`           // 快照边界：签发时间（RFC3339，与库中存储一致）
	SnapID   int64  `json:"sd"`           // 快照边界：编号
	LastTime string `json:"lt,omitempty"` // 键集位置：上一页最后一行的签发时间（仅游标）
	LastID   int64  `json:"ld,omitempty"` // 键集位置：上一页最后一行的编号（仅游标）
	Exp      int64  `json:"exp"`          // 过期时间（Unix 秒）
}

// newSecret 生成随机的游标签名密钥（未通过环境变量配置时使用）。
// 进程重启后旧游标自然失效，与游标本身的短有效期语义一致。
func newSecret() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

// signToken 把载荷序列化并签名，返回不透明令牌字符串。
func (s *Server) signToken(t pageToken) string {
	raw, _ := json.Marshal(t)
	body := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(body))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return body + "." + sig
}

// parseToken 校验令牌签名并还原载荷；签名不符视为伪造，过期单独报错。
func (s *Server) parseToken(token string) (pageToken, error) {
	var t pageToken
	body, sig, ok := strings.Cut(token, ".")
	if !ok {
		return t, errTokenInvalid
	}
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(body))
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, mac.Sum(nil)) {
		return t, errTokenInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return t, errTokenInvalid
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return t, errTokenInvalid
	}
	if t.Exp <= time.Now().Unix() {
		return t, errTokenExpired
	}
	return t, nil
}
