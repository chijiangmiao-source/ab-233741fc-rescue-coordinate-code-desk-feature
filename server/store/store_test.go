package store

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })
	return st
}

func TestSaveAndFindCard(t *testing.T) {
	st := openTestStore(t)

	card, err := st.SaveCard(1234, 5678, "152637483")
	require.NoError(t, err)
	assert.Equal(t, 1234, card.X)
	assert.Equal(t, 5678, card.Y)
	assert.Equal(t, "152637483", card.Code)
	assert.False(t, card.IssuedAt.IsZero(), "签发时间应被记录")

	found, ok, err := st.FindByCode("152637483")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, card, found)
}

func TestSaveCardIsIdempotentPerCode(t *testing.T) {
	st := openTestStore(t)

	first, err := st.SaveCard(3, 0, "00000030X")
	require.NoError(t, err)
	second, err := st.SaveCard(3, 0, "00000030X")
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID, "同一短码重复签发应返回同一条记录")
	assert.Equal(t, first.IssuedAt, second.IssuedAt)
}

func TestFindByCodeMiss(t *testing.T) {
	st := openTestStore(t)

	_, ok, err := st.FindByCode("999999995")
	require.NoError(t, err)
	assert.False(t, ok, "未签发过的短码不应查到记录")
}
