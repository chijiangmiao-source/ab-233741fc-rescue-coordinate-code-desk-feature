// 签发记录分页游标：对客户端不透明、带 HMAC-SHA256 签名与过期时间的令牌。
// 客户端只能原样回传服务端签发的游标，无法伪造或窥探其中的快照边界与键集位置。
package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// cursorTTL 是游标有效期：交接班浏览通常在数分钟内完成，过期后需重新
// 打开签发记录确定新快照，避免拿着陈旧游标无限翻页。
const cursorTTL = 2 * time.Hour

// cursorVersion 标记游标签名格式；将来调整判据时可显式拒绝旧格式。
const cursorVersion = 1

var (
	errCursorMalformed = errors.New("游标无效")
	errCursorExpired   = errors.New("游标已过期，请重新打开签发记录")
)

// cursorPayload 是签名保护的游标内容，全部以服务端存储为准，客户端不可改写。
type cursorPayload struct {
	Version  int    `json:"v"`
	SnapID   int64  `json:"s"` // 快照边界编号（首批那一刻最新一张卡）
	SnapAt   string `json:"t"` // 快照边界签发时间（RFC3339，用于核对边界未漂移）
	AfterID  int64  `json:"a"` // 本页要接续的最后一张已返回卡编号
	ExpireAt int64  `json:"e"` // 过期时刻（Unix 秒）
}

// cursorSigner 负责签发与校验游标。secret 默认每进程随机生成（服务重启即
// 作废旧游标，客户端重新拉取首批即可）；也可用环境变量 CURSOR_SECRET
// 注入 base64 编码密钥（至少 16 字节），便于多实例共享。
type cursorSigner struct {
	secret []byte
	now    func() time.Time
}

func newCursorSigner() (*cursorSigner, error) {
	secret := make([]byte, 32)
	if raw := os.Getenv("CURSOR_SECRET"); raw != "" {
		b, err := decodeSecret(raw)
		if err != nil {
			return nil, fmt.Errorf("CURSOR_SECRET 无效: %w", err)
		}
		secret = b
	} else if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("生成游标密钥失败: %w", err)
	}
	return &cursorSigner{secret: secret, now: time.Now}, nil
}

func decodeSecret(raw string) ([]byte, error) {
	if b, err := base64.RawURLEncoding.DecodeString(raw); err == nil && len(b) >= 16 {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) >= 16 {
		return b, nil
	}
	return nil, errors.New("须为至少 16 字节的 base64 编码")
}

func (cs *cursorSigner) mac(payload []byte) []byte {
	mac := hmac.New(sha256.New, cs.secret)
	mac.Write(payload)
	return mac.Sum(nil)
}

// sign 把快照边界与键集锚点封装为不透明游标字符串。
func (cs *cursorSigner) sign(snapID int64, snapAt string, afterID int64) string {
	p := cursorPayload{
		Version:  cursorVersion,
		SnapID:   snapID,
		SnapAt:   snapAt,
		AfterID:  afterID,
		ExpireAt: cs.now().Add(cursorTTL).Unix(),
	}
	body, _ := json.Marshal(p)
	enc := base64.RawURLEncoding.EncodeToString(body)
	sig := base64.RawURLEncoding.EncodeToString(cs.mac([]byte(enc)))
	return enc + "." + sig
}

// parse 校验游标格式、签名与有效期，任一项不过都返回错误且不泄露任何记录。
func (cs *cursorSigner) parse(token string) (cursorPayload, error) {
	var p cursorPayload
	dot := strings.LastIndexByte(token, '.')
	if dot <= 0 || dot == len(token)-1 {
		return p, errCursorMalformed
	}
	enc, sig := token[:dot], token[dot+1:]
	gotSig, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || len(gotSig) != sha256.Size {
		return p, errCursorMalformed
	}
	if subtle.ConstantTimeCompare(cs.mac([]byte(enc)), gotSig) != 1 {
		return p, errCursorMalformed
	}
	body, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil || json.Unmarshal(body, &p) != nil {
		return p, errCursorMalformed
	}
	if p.Version != cursorVersion || p.SnapID <= 0 || p.AfterID <= 0 || p.SnapAt == "" {
		return p, errCursorMalformed
	}
	if cs.now().Unix() >= p.ExpireAt {
		return p, errCursorExpired
	}
	return p, nil
}

// parseLimit 解析每页数量：缺省 defaultLimit，超出 [1, maxPageSize] 一律拒绝。
func parseLimit(raw string) (int, error) {
	if raw == "" {
		return defaultPageSize, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > maxPageSize {
		return 0, fmt.Errorf("每页数量须为 1 至 %d 的整数", maxPageSize)
	}
	return n, nil
}
