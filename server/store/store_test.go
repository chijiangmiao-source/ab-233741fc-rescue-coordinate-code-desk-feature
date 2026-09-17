package store

import (
	"path/filepath"
	"testing"
	"time"

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

// insertCardAt 以指定的签发时间直接落库，构造确定性的记录序列
// （SaveCard 的时间精度为秒，快速连续签发会产生大量同秒记录，
// 这里显式覆盖同秒与跨秒两种情形）。
func insertCardAt(t *testing.T, st *Store, x, y int, code, issuedAt string) int64 {
	t.Helper()
	res, err := st.db.Exec(
		`INSERT INTO cards (x, y, code, issued_at) VALUES (?, ?, ?, ?)`,
		x, y, code, issuedAt,
	)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return id
}

func cardIDs(cards []Card) []int64 {
	ids := make([]int64, len(cards))
	for i, c := range cards {
		ids[i] = c.ID
	}
	return ids
}

func TestSnapshotBoundaryEmpty(t *testing.T) {
	st := openTestStore(t)

	snapTime, snapID, err := st.SnapshotBoundary()
	require.NoError(t, err)
	assert.Equal(t, "", snapTime)
	assert.EqualValues(t, 0, snapID)

	// 空表上的首页查询：没有记录，也不报错。
	cards, err := st.ListCards(RecordQuery{SnapTime: snapTime, SnapID: snapID, Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, cards)
}

func TestListCardsKeysetPagination(t *testing.T) {
	st := openTestStore(t)
	// 同一秒两张、跨秒再两张、再同秒一张：倒序应为 id 5,4,3,2,1。
	insertCardAt(t, st, 0, 0, "000000000", "2026-09-17T08:00:00Z")
	insertCardAt(t, st, 3, 0, "00000030X", "2026-09-17T08:00:00Z")
	insertCardAt(t, st, 42, 7, "000040272", "2026-09-17T08:00:01Z")
	insertCardAt(t, st, 500, 6000, "065000005", "2026-09-17T08:00:01Z")
	insertCardAt(t, st, 1234, 5678, "152637483", "2026-09-17T08:00:02Z")

	snapTime, snapID, err := st.SnapshotBoundary()
	require.NoError(t, err)
	assert.Equal(t, "2026-09-17T08:00:02Z", snapTime)
	assert.EqualValues(t, 5, snapID)

	// 第一页：最新两张（同秒的 4 在 3 之前由编号倒序决定）。
	page1, err := st.ListCards(RecordQuery{SnapTime: snapTime, SnapID: snapID, Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, []int64{5, 4}, cardIDs(page1))

	// 后续页用上一页最后一行的（时间, 编号）做键集，逐页不重不漏。
	last := page1[len(page1)-1]
	page2, err := st.ListCards(RecordQuery{
		SnapTime: snapTime, SnapID: snapID,
		LastTime: last.IssuedAt.UTC().Format(time.RFC3339), LastID: last.ID,
		Paged: true, Limit: 2,
	})
	require.NoError(t, err)
	assert.Equal(t, []int64{3, 2}, cardIDs(page2))

	last = page2[len(page2)-1]
	page3, err := st.ListCards(RecordQuery{
		SnapTime: snapTime, SnapID: snapID,
		LastTime: last.IssuedAt.UTC().Format(time.RFC3339), LastID: last.ID,
		Paged: true, Limit: 2,
	})
	require.NoError(t, err)
	assert.Equal(t, []int64{1}, cardIDs(page3))

	last = page3[len(page3)-1]
	page4, err := st.ListCards(RecordQuery{
		SnapTime: snapTime, SnapID: snapID,
		LastTime: last.IssuedAt.UTC().Format(time.RFC3339), LastID: last.ID,
		Paged: true, Limit: 2,
	})
	require.NoError(t, err)
	assert.Empty(t, page4)
}

func TestListCardsSnapshotExcludesNewerCards(t *testing.T) {
	st := openTestStore(t)
	insertCardAt(t, st, 0, 0, "000000000", "2026-09-17T08:00:00Z")
	insertCardAt(t, st, 3, 0, "00000030X", "2026-09-17T08:00:00Z")

	snapTime, snapID, err := st.SnapshotBoundary()
	require.NoError(t, err)

	// 快照确定之后又签发的卡（包括与快照边界同秒的卡）不得进入浏览序列。
	insertCardAt(t, st, 42, 7, "000040272", "2026-09-17T08:00:00Z")
	insertCardAt(t, st, 500, 6000, "065000005", "2026-09-17T08:00:01Z")

	cards, err := st.ListCards(RecordQuery{SnapTime: snapTime, SnapID: snapID, Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, []int64{2, 1}, cardIDs(cards), "快照边界外的记录不得出现")
}
