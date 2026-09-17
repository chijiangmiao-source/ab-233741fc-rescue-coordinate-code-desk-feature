package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sars/store"
)

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })
	return NewRouter(st)
}

func postJSON(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &m))
	return m
}

// 固定合法样例（与 shortcode 包、前端 Vitest 相同）。
func TestIssueCardValidSamples(t *testing.T) {
	h := newTestRouter(t)
	samples := []struct {
		x, y int
		code string
	}{
		{0, 0, "000000000"},
		{1234, 5678, "152637483"},
		{9999, 9999, "999999995"},
		{3, 0, "00000030X"},
		{42, 7, "000040272"},
		{500, 6000, "065000005"},
	}
	for _, s := range samples {
		body, err := json.Marshal(map[string]int{"x": s.x, "y": s.y})
		require.NoError(t, err)
		rec := postJSON(t, h, "/api/cards", string(body))
		require.Equal(t, http.StatusCreated, rec.Code, "body=%s", body)
		m := decodeBody(t, rec)
		assert.Equal(t, s.code, m["code"])
		assert.EqualValues(t, s.x, m["x"])
		assert.EqualValues(t, s.y, m["y"])
		assert.NotEmpty(t, m["issued_at"], "签发成功后必须返回签发时间")
	}
}

func TestIssueCardRejectsBadInput(t *testing.T) {
	h := newTestRouter(t)
	bodies := []string{
		`{"x":10000,"y":0}`, // 超出上限
		`{"x":0,"y":10000}`, // 超出上限
		`{"x":-1,"y":0}`,    // 负数
		`{"x":1.5,"y":0}`,   // 非整数
		`{"x":"12","y":0}`,  // 字符串
		`{"x":12}`,          // 缺字段
		`not-json`,          // 非 JSON
	}
	for _, b := range bodies {
		rec := postJSON(t, h, "/api/cards", b)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "body=%s", b)
		assert.NotEmpty(t, decodeBody(t, rec)["error"])
	}
}

func TestVerifyIssuedCode(t *testing.T) {
	h := newTestRouter(t)

	rec := postJSON(t, h, "/api/cards", `{"x":1234,"y":5678}`)
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = postJSON(t, h, "/api/verify", `{"code":"152637483"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	m := decodeBody(t, rec)
	assert.Equal(t, true, m["valid"])
	assert.EqualValues(t, 1234, m["x"])
	assert.EqualValues(t, 5678, m["y"])
	assert.Equal(t, true, m["issued"], "已签发的短码应带签发记录")
	assert.NotEmpty(t, m["issued_at"])
}

func TestVerifyValidButNotIssued(t *testing.T) {
	h := newTestRouter(t)

	rec := postJSON(t, h, "/api/verify", `{"code":"00000030X"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	m := decodeBody(t, rec)
	assert.Equal(t, true, m["valid"])
	assert.EqualValues(t, 3, m["x"])
	assert.EqualValues(t, 0, m["y"])
	assert.Equal(t, false, m["issued"])
}

// 固定篡改样例：任何单字符篡改都必须得到 422 明确拒绝，且响应不含坐标。
func TestVerifyRejectsTamperedSamples(t *testing.T) {
	h := newTestRouter(t)
	tampered := []string{
		"252637483",
		"152637493",
		"152637403",
		"152637484",
		"15263748X",
	}
	for _, code := range tampered {
		body, err := json.Marshal(map[string]string{"code": code})
		require.NoError(t, err)
		rec := postJSON(t, h, "/api/verify", string(body))
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "code=%s", code)
		m := decodeBody(t, rec)
		assert.Equal(t, false, m["valid"])
		assert.NotEmpty(t, m["error"])
		assert.NotContains(t, m, "x", "校验失败不得还原坐标")
		assert.NotContains(t, m, "y", "校验失败不得还原坐标")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	h := newTestRouter(t)
	for _, code := range []string{"", "15263748", "1526374830", "15263748x", "ABCDEFGHI"} {
		body, err := json.Marshal(map[string]string{"code": code})
		require.NoError(t, err)
		rec := postJSON(t, h, "/api/verify", string(body))
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "code=%q", code)
		assert.NotEmpty(t, decodeBody(t, rec)["error"])
	}
}

// 核验失败不得落库：篡改短码被拒绝后，库中不应出现任何记录。
func TestFailedVerifyNeverPersists(t *testing.T) {
	h := newTestRouter(t)

	rec := postJSON(t, h, "/api/verify", `{"code":"152637493"}`)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	// 用合法短码 "152637483" 核验：若之前的篡改请求落库，这里会误报已签发。
	rec = postJSON(t, h, "/api/verify", `{"code":"152637483"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, false, decodeBody(t, rec)["issued"])
}

// —— 签发记录查询（快照边界 + 不透明游标键集分页）——

type listResponse struct {
	Cards      []store.Card `json:"cards"`
	Snapshot   string       `json:"snapshot"`
	NextCursor *string      `json:"next_cursor"`
	HasMore    bool         `json:"has_more"`
	Error      string       `json:"error"`
}

func getList(t *testing.T, h http.Handler, path string) (int, listResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var lr listResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &lr))
	return rec.Code, lr
}

func getMap(t *testing.T, h http.Handler, path string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, decodeBody(t, rec)
}

// issueCards 经真实签发接口连续签发 n 张卡（坐标从 start 起递增，保证短码唯一）。
func issueCards(t *testing.T, h http.Handler, start, n int) []store.Card {
	t.Helper()
	cards := make([]store.Card, 0, n)
	for i := 0; i < n; i++ {
		body, err := json.Marshal(map[string]int{"x": start + i, "y": start + i})
		require.NoError(t, err)
		rec := postJSON(t, h, "/api/cards", string(body))
		require.Equal(t, http.StatusCreated, rec.Code)
		var card store.Card
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &card))
		cards = append(cards, card)
	}
	return cards
}

func pagePath(limit int, snapshot, cursor string) string {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if snapshot != "" {
		q.Set("snapshot", snapshot)
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	return "/api/cards?" + q.Encode()
}

func TestListCardsEmpty(t *testing.T) {
	h := newTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/cards", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"cards":[]`, "空表应返回空数组而非 null")

	_, page := getList(t, h, "/api/cards")
	assert.Empty(t, page.Cards)
	assert.False(t, page.HasMore)
	assert.Nil(t, page.NextCursor)
	assert.NotEmpty(t, page.Snapshot, "空表也应返回可用的快照标识")
}

// 多页顺序稳定：按签发时间倒序连续翻页，全程不重不漏。
func TestListCardsPaginationOrderStable(t *testing.T) {
	h := newTestRouter(t)
	issued := issueCards(t, h, 0, 7)

	status, page := getList(t, h, pagePath(3, "", ""))
	require.Equal(t, http.StatusOK, status)
	require.Len(t, page.Cards, 3)
	assert.True(t, page.HasMore)
	assert.NotEmpty(t, page.Snapshot)
	require.NotNil(t, page.NextCursor)

	all := append([]store.Card{}, page.Cards...)
	for i := 0; i < 10; i++ {
		require.NotNil(t, page.NextCursor)
		status, page = getList(t, h, pagePath(3, page.Snapshot, *page.NextCursor))
		require.Equal(t, http.StatusOK, status)
		all = append(all, page.Cards...)
		if !page.HasMore {
			break
		}
	}
	require.Len(t, all, 7, "翻页到底应恰好覆盖全部记录")
	for i, c := range all {
		assert.Equal(t, issued[len(issued)-1-i].Code, c.Code, "第 %d 条顺序不符", i)
	}
	assert.Nil(t, page.NextCursor, "最后一页不应再有翻页游标")
}

// 翻页期间新签发的卡不得插入当前浏览序列；新快照才能看到它们。
func TestListCardsSnapshotFrozenDuringPaging(t *testing.T) {
	h := newTestRouter(t)
	issued := issueCards(t, h, 0, 5)

	status, page := getList(t, h, pagePath(2, "", ""))
	require.Equal(t, http.StatusOK, status)
	require.True(t, page.HasMore)

	// 翻页期间新签发两张卡
	newer := issueCards(t, h, 100, 2)

	all := append([]store.Card{}, page.Cards...)
	for i := 0; i < 10 && page.HasMore; i++ {
		status, page = getList(t, h, pagePath(2, page.Snapshot, *page.NextCursor))
		require.Equal(t, http.StatusOK, status)
		all = append(all, page.Cards...)
	}
	require.Len(t, all, 5, "快照内应恰好是首批 5 张，新卡不得插入")
	for i, c := range all {
		assert.Equal(t, issued[len(issued)-1-i].Code, c.Code)
	}
	for _, c := range all {
		assert.NotContains(t, []string{newer[0].Code, newer[1].Code}, c.Code)
	}

	// 控制组：重新取首批（新快照）应能看到新签发的卡。
	status, fresh := getList(t, h, pagePath(1, "", ""))
	require.Equal(t, http.StatusOK, status)
	require.Len(t, fresh.Cards, 1)
	assert.Equal(t, newer[1].Code, fresh.Cards[0].Code)
}

// 伪造、篡改、缺快照、种类错用的游标一律 400，且响应不含任何记录。
func TestListCardsRejectsForgedAndMalformedCursors(t *testing.T) {
	h := newTestRouter(t)
	issueCards(t, h, 0, 3)

	_, page := getList(t, h, pagePath(2, "", ""))
	require.NotNil(t, page.NextCursor)
	snap := page.Snapshot
	cursor := *page.NextCursor

	// 篡改签名：翻转游标最后一个字符。
	last := cursor[len(cursor)-1]
	flipped := byte('A')
	if last == 'A' {
		flipped = 'B'
	}
	tampered := cursor[:len(cursor)-1] + string(flipped)

	// 伪造载荷：自行编码一个没有合法签名的游标。
	forged := base64.RawURLEncoding.EncodeToString([]byte(
		`{"k":"c","st":"2026-09-17T00:00:00Z","sd":3,"lt":"2026-09-17T00:00:00Z","ld":2,"exp":9999999999}`,
	)) + ".AAAA"

	badPaths := []string{
		pagePath(2, snap, "not-a-token"),        // 垃圾游标
		pagePath(2, snap, "abc.def"),            // 结构不符
		pagePath(2, snap, tampered),             // 签名被篡改
		pagePath(2, snap, forged),               // 伪造载荷
		pagePath(2, "", cursor),                 // 缺少快照标识
		pagePath(2, cursor, cursor),             // 游标冒充快照
		pagePath(2, snap, snap),                 // 快照冒充游标
		pagePath(2, "garbage.snapshot", cursor), // 垃圾快照
	}
	for _, p := range badPaths {
		status, m := getMap(t, h, p)
		assert.Equal(t, http.StatusBadRequest, status, "path=%s", p)
		assert.NotEmpty(t, m["error"], "path=%s", p)
		assert.NotContains(t, m, "cards", "非法游标不得泄露任何记录")
	}
}

// 游标必须与同一次加载的快照一起使用：混用其他快照的游标被拒绝。
func TestListCardsRejectsMismatchedSnapshot(t *testing.T) {
	h := newTestRouter(t)
	issueCards(t, h, 0, 3)

	_, p1 := getList(t, h, pagePath(2, "", ""))
	require.NotNil(t, p1.NextCursor)

	// 新签发改变快照边界后，再次首批查询得到不同的快照。
	issueCards(t, h, 100, 1)
	_, p2 := getList(t, h, pagePath(2, "", ""))
	require.NotEqual(t, p1.Snapshot, p2.Snapshot)

	status, m := getMap(t, h, pagePath(2, p2.Snapshot, *p1.NextCursor))
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Contains(t, m["error"], "不匹配")
	assert.NotContains(t, m, "cards")

	// 控制组：旧游标配旧快照仍可使用，且看不到新签发的卡。
	status, page := getList(t, h, pagePath(10, p1.Snapshot, *p1.NextCursor))
	require.Equal(t, http.StatusOK, status)
	require.Len(t, page.Cards, 1)
	assert.EqualValues(t, 0, page.Cards[0].X)
}

// 过期游标与过期快照一律被拒绝。
func TestListCardsRejectsExpiredCursor(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })
	h := NewRouterWithOptions(st, Options{CursorTTL: 50 * time.Millisecond})

	issueCards(t, h, 0, 3)
	_, page := getList(t, h, pagePath(2, "", ""))
	require.NotNil(t, page.NextCursor)

	time.Sleep(150 * time.Millisecond)

	status, m := getMap(t, h, pagePath(2, page.Snapshot, *page.NextCursor))
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Contains(t, m["error"], "过期")
	assert.NotContains(t, m, "cards")

	status, m = getMap(t, h, pagePath(2, page.Snapshot, ""))
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Contains(t, m["error"], "过期")
}

// 每页数量：非法值拒绝，超过上限按上限截断。
func TestListCardsLimitRules(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })
	h := NewRouterWithOptions(st, Options{PageSizeMax: 4})

	issueCards(t, h, 0, 6)

	status, page := getList(t, h, "/api/cards")
	require.Equal(t, http.StatusOK, status)
	assert.Len(t, page.Cards, 4, "默认每页数量同样不得超过上限")
	assert.True(t, page.HasMore)

	status, page = getList(t, h, "/api/cards?limit=99")
	require.Equal(t, http.StatusOK, status)
	assert.Len(t, page.Cards, 4, "超过上限应按上限截断")
	assert.True(t, page.HasMore)

	for _, bad := range []string{"0", "-1", "abc", "2.5"} {
		status, m := getMap(t, h, "/api/cards?limit="+bad)
		assert.Equal(t, http.StatusBadRequest, status, "limit=%s", bad)
		assert.NotEmpty(t, m["error"])
	}
}

// 从记录中取出的短码，经真实核验链路仍还原唯一坐标。
func TestListedCodeVerifiesThroughRealChain(t *testing.T) {
	h := newTestRouter(t)
	issued := issueCards(t, h, 0, 3)

	_, page := getList(t, h, pagePath(1, "", ""))
	require.Len(t, page.Cards, 1)
	code := page.Cards[0].Code
	assert.Equal(t, issued[2].Code, code)

	body, err := json.Marshal(map[string]string{"code": code})
	require.NoError(t, err)
	rec := postJSON(t, h, "/api/verify", string(body))
	require.Equal(t, http.StatusOK, rec.Code)
	m := decodeBody(t, rec)
	assert.Equal(t, true, m["valid"])
	assert.EqualValues(t, issued[2].X, m["x"])
	assert.EqualValues(t, issued[2].Y, m["y"])
	assert.Equal(t, true, m["issued"])
}
