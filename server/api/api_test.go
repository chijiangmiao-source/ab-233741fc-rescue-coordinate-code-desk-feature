package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

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
