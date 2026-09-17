package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sars/store"
)

func newTestServer(t *testing.T) (*Server, http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })
	signer, err := newCursorSigner()
	require.NoError(t, err)
	s := &Server{store: st, cursor: signer}
	return s, newRouter(s), st
}

func getCards(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func issueSeqCard(t *testing.T, h http.Handler, seq int) {
	t.Helper()
	body := fmt.Sprintf(`{"x":%d,"y":%d}`, seq, (seq*3)%10000)
	rec := postJSON(t, h, "/api/cards", body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
}

func TestListCardsEmpty(t *testing.T) {
	_, h, _ := newTestServer(t)
	rec := getCards(t, h, "/api/cards")
	require.Equal(t, http.StatusOK, rec.Code)
	m := decodeBody(t, rec)
	assert.Equal(t, []any{}, m["cards"], "空库应返回 [] 而非 null")
	assert.Equal(t, false, m["has_more"])
	assert.Equal(t, "", m["next_cursor"])
}

func TestListCardsPaginatesInStableDescendingOrder(t *testing.T) {
	_, h, _ := newTestServer(t)
	const n = 7
	for i := 1; i <= n; i++ {
		issueSeqCard(t, h, i)
	}

	var ids []int64
	path := "/api/cards?limit=3"
	for page := 0; page < 5; page++ {
		rec := getCards(t, h, path)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		m := decodeBody(t, rec)
		for _, raw := range m["cards"].([]any) {
			ids = append(ids, int64(raw.(map[string]any)["id"].(float64)))
		}
		if m["has_more"] != true {
			break
		}
		path = "/api/cards?limit=3&cursor=" + m["next_cursor"].(string)
	}
	require.Len(t, ids, n)
	for i := 0; i < n-1; i++ {
		assert.Equal(t, int64(n-i), ids[i], "应严格按签发倒序（编号倒序）")
	}
}

// 同一游标重复请求应返回同一页：失败就地提示后用户重试同一页，结果不变。
func TestListCardsCursorIsReplayable(t *testing.T) {
	_, h, _ := newTestServer(t)
	for i := 1; i <= 5; i++ {
		issueSeqCard(t, h, i)
	}
	first := decodeBody(t, getCards(t, h, "/api/cards?limit=2"))
	require.Equal(t, true, first["has_more"])
	cursor := first["next_cursor"].(string)

	r1 := getCards(t, h, "/api/cards?limit=2&cursor="+cursor)
	r2 := getCards(t, h, "/api/cards?limit=2&cursor="+cursor)
	require.Equal(t, http.StatusOK, r1.Code)
	require.Equal(t, r1.Body.String(), r2.Body.String(), "重试同一游标必须返回同一页")
}

// 翻页期间新签发的卡不能插入当前快照；重新开始浏览才能看到新卡。
func TestListCardsSnapshotStableAgainstNewIssues(t *testing.T) {
	_, h, _ := newTestServer(t)
	for i := 1; i <= 5; i++ {
		issueSeqCard(t, h, i)
	}

	first := getCards(t, h, "/api/cards?limit=2")
	m := decodeBody(t, first)
	require.Equal(t, true, m["has_more"])
	cursor := m["next_cursor"].(string)
	snapshotID := int64(m["snapshot_id"].(float64))

	// 翻页途中签发两张新卡（编号大于快照边界）。
	issueSeqCard(t, h, 600)
	issueSeqCard(t, h, 601)

	var seen []int64
	for _, raw := range m["cards"].([]any) {
		seen = append(seen, int64(raw.(map[string]any)["id"].(float64)))
	}
	for cursor != "" {
		rec := getCards(t, h, "/api/cards?limit=2&cursor="+cursor)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		pm := decodeBody(t, rec)
		for _, raw := range pm["cards"].([]any) {
			id := int64(raw.(map[string]any)["id"].(float64))
			assert.LessOrEqual(t, id, snapshotID, "快照内不得出现新签发的卡")
			seen = append(seen, id)
		}
		cursor, _ = pm["next_cursor"].(string)
	}
	require.Len(t, seen, 5, "快照只含首批时已存在的 5 张卡，不重不漏")

	fresh := getCards(t, h, "/api/cards?limit=50")
	assert.Len(t, decodeBody(t, fresh)["cards"].([]any), 7, "重新浏览的新快照包含新卡")
}

func TestListCardsRejectsBadLimit(t *testing.T) {
	_, h, _ := newTestServer(t)
	for _, q := range []string{"limit=0", "limit=51", "limit=-1", "limit=abc"} {
		rec := getCards(t, h, "/api/cards?"+q)
		assert.Equal(t, http.StatusBadRequest, rec.Code, q)
		m := decodeBody(t, rec)
		assert.NotEmpty(t, m["error"])
		assert.NotContains(t, m, "cards", "参数非法不得泄露任何记录")
	}
}

func TestListCardsRejectsForgedCursors(t *testing.T) {
	_, h, _ := newTestServer(t)
	for i := 1; i <= 5; i++ {
		issueSeqCard(t, h, i)
	}
	real := decodeBody(t, getCards(t, h, "/api/cards?limit=2"))
	realCursor := real["next_cursor"].(string)
	dot := strings.IndexByte(realCursor, '.')
	body, sig := realCursor[:dot], realCursor[dot+1:]

	cases := map[string]string{
		"完全伪造":         "garbage",
		"只有 body 没有签名": "aGVsbG8",
		"签名长度不对":       "aGVsbG8.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"末尾追加字符":       realCursor + "x",
		"签名末位被改":       body + "." + flipBase64Char(sig),
		"body 被改但签名照旧": tamperCursorBody(t, realCursor),
		"把游标重复拼接":      realCursor + "." + sig,
	}
	for name, token := range cases {
		rec := getCards(t, h, "/api/cards?limit=2&cursor="+token)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "游标样例：%s", name)
		m := decodeBody(t, rec)
		assert.Equal(t, "cursor_invalid", m["code"], "游标样例：%s", name)
		assert.NotContains(t, m, "cards", "伪造游标不得泄露任何记录（%s）", name)
	}
}

func flipBase64Char(s string) string {
	b := []byte(s)
	if b[len(b)-1] == 'A' {
		b[len(b)-1] = 'B'
	} else {
		b[len(b)-1] = 'A'
	}
	return string(b)
}

// tamperCursorBody 解码真游标的 body，把锚点编号改掉后重新编码，签名保持不变。
func tamperCursorBody(t *testing.T, token string) string {
	t.Helper()
	dot := strings.IndexByte(token, '.')
	raw, err := base64.RawURLEncoding.DecodeString(token[:dot])
	require.NoError(t, err)
	var p cursorPayload
	require.NoError(t, json.Unmarshal(raw, &p))
	p.AfterID++
	raw, _ = json.Marshal(p)
	return base64.RawURLEncoding.EncodeToString(raw) + token[dot:]
}

// 过期游标必须被拒绝，并给出可识别的 code，前端可据此提示重新打开记录。
func TestListCardsRejectsExpiredCursor(t *testing.T) {
	s, h, _ := newTestServer(t)
	for i := 1; i <= 5; i++ {
		issueSeqCard(t, h, i)
	}
	cursor := s.cursor.sign(5, "2026-09-17T08:00:00Z", 4)
	s.cursor.now = func() time.Time { return time.Now().Add(cursorTTL + time.Hour) }

	rec := getCards(t, h, "/api/cards?limit=2&cursor="+cursor)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	m := decodeBody(t, rec)
	assert.Equal(t, "cursor_expired", m["code"])
	assert.NotContains(t, m, "cards")
}

// 即便两个实例共享签名密钥：游标指向的快照边界在当前库不存在，也必须以
// cursor_stale 拒绝，绝不退化成从头查询（那会把新卡混进旧序列）。
func TestListCardsRejectsCursorMismatchedWithSnapshot(t *testing.T) {
	t.Setenv("CURSOR_SECRET", base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef")))

	st1, err := store.Open(filepath.Join(t.TempDir(), "a.db"))
	require.NoError(t, err)
	t.Cleanup(func() { st1.Close() })
	signer1, err := newCursorSigner()
	require.NoError(t, err)
	h1 := newRouter(&Server{store: st1, cursor: signer1})
	for i := 1; i <= 5; i++ {
		issueSeqCard(t, h1, i)
	}
	token := decodeBody(t, getCards(t, h1, "/api/cards?limit=2"))["next_cursor"].(string)

	// 空库实例，同一密钥：游标里的快照边界与锚点都不存在。
	st2, err := store.Open(filepath.Join(t.TempDir(), "b.db"))
	require.NoError(t, err)
	t.Cleanup(func() { st2.Close() })
	signer2, err := newCursorSigner()
	require.NoError(t, err)
	h2 := newRouter(&Server{store: st2, cursor: signer2})

	rec := getCards(t, h2, "/api/cards?limit=2&cursor="+token)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	m := decodeBody(t, rec)
	assert.Equal(t, "cursor_stale", m["code"])
	assert.NotContains(t, m, "cards")
}

// 游标内容对客户端不透明：即便解码 base64，也只看到快照边界与锚点编号，
// 不含短码、坐标等任何记录数据。
func TestCursorLeaksNoCardData(t *testing.T) {
	cs, err := newCursorSigner()
	require.NoError(t, err)
	token := cs.sign(7, "2026-09-17T08:00:00Z", 5)
	dot := strings.IndexByte(token, '.')
	raw, err := base64.RawURLEncoding.DecodeString(token[:dot])
	require.NoError(t, err)
	var p map[string]any
	require.NoError(t, json.Unmarshal(raw, &p))
	assert.Equal(t, float64(7), p["s"])
	assert.Equal(t, float64(5), p["a"])
	assert.NotContains(t, p, "code")
	assert.NotContains(t, p, "x")
	assert.NotContains(t, p, "y")
}
