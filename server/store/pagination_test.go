package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertCardAt 用显式签发时间直接落库，便于确定性地构造分页序列
// （包括多条同一秒签发、需要靠 id 兜底排序的情形）。
func insertCardAt(t *testing.T, st *Store, seq int, issuedAt string) Card {
	t.Helper()
	res, err := st.db.Exec(
		`INSERT INTO cards (x, y, code, issued_at) VALUES (?, ?, ?, ?)`,
		seq, seq, fmt.Sprintf("%09d", seq), issuedAt,
	)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	card, found, err := st.CardAt(id)
	require.NoError(t, err)
	require.True(t, found)
	return card
}

// 翻完整个快照，返回按页收集到的卡序列。
func walkSnapshot(t *testing.T, st *Store, pageSize int) []Card {
	t.Helper()
	first, snap, hasMore, err := st.PageFirst(pageSize)
	require.NoError(t, err)
	got := append([]Card{}, first...)
	for hasMore {
		last := got[len(got)-1]
		page, more, err := st.PageAfter(snap.BoundaryID, last.IssuedAt.Format(time.RFC3339), last.ID, pageSize)
		require.NoError(t, err)
		require.NotEmpty(t, page, "hasMore=true 时下一页不应为空")
		got = append(got, page...)
		hasMore = more
	}
	return got
}

func TestPaginationStableOrderAcrossPages(t *testing.T) {
	st := openTestStore(t)

	// 7 张卡，签发时间严格递增；编号与时间顺序一致。
	want := make([]Card, 0, 7)
	for i := 1; i <= 7; i++ {
		c := insertCardAt(t, st, i, fmt.Sprintf("2026-09-17T08:%02d:00Z", i))
		want = append(want, c)
	}

	got := walkSnapshot(t, st, 3)
	require.Len(t, got, 7, "多页合计应覆盖快照内全部卡")

	// 期望按签发时间倒序、同秒按编号倒序。
	for i := len(want) - 1; i >= 0; i-- {
		assert.Equal(t, want[i].ID, got[len(want)-1-i].ID)
	}

	// 无重复、无遗漏。
	seen := map[int64]int{}
	for _, c := range got {
		seen[c.ID]++
	}
	assert.Len(t, seen, 7)
	for id, n := range seen {
		assert.Equal(t, 1, n, "卡 %d 重复出现", id)
	}
}

func TestPaginationSameSecondOrdersByIDDesc(t *testing.T) {
	st := openTestStore(t)
	for i := 1; i <= 5; i++ {
		insertCardAt(t, st, i, "2026-09-17T08:00:00Z")
	}
	got := walkSnapshot(t, st, 2)
	require.Len(t, got, 5)
	for i := 0; i < 4; i++ {
		assert.Greater(t, got[i].ID, got[i+1].ID, "同一秒签发应按编号倒序")
	}
}

// 首批确定快照后再签发新卡：后续翻页必须钉在旧快照上，新卡不插入、
// 不造成重复或遗漏；重新开始浏览时新卡才出现在最新位置。
func TestPaginationSnapshotExcludesNewerCards(t *testing.T) {
	st := openTestStore(t)
	for i := 1; i <= 5; i++ {
		insertCardAt(t, st, i, fmt.Sprintf("2026-09-17T08:%02d:00Z", i))
	}

	first, snap, hasMore, err := st.PageFirst(2)
	require.NoError(t, err)
	require.True(t, hasMore)
	require.Len(t, first, 2)
	snapshotIDs := []int64{5, 4, 3, 2, 1}

	// 翻页期间新签发一张更晚的卡。
	late := insertCardAt(t, st, 99, "2026-09-17T09:00:00Z")
	assert.Greater(t, late.ID, snap.BoundaryID, "新卡编号必然大于快照边界")

	got := append([]Card{}, first...)
	for hasMore {
		last := got[len(got)-1]
		page, more, err := st.PageAfter(snap.BoundaryID, last.IssuedAt.Format(time.RFC3339), last.ID, 2)
		require.NoError(t, err)
		got = append(got, page...)
		hasMore = more
	}

	require.Len(t, got, 5, "快照浏览只覆盖首批时刻已存在的 5 张卡")
	for i, c := range got {
		assert.Equal(t, snapshotIDs[i], c.ID)
	}
	for _, c := range got {
		assert.NotEqual(t, late.ID, c.ID, "浏览期间新签发的卡不得插入当前序列")
	}

	// 重新打开（新快照）：新卡应出现在最新位置，总数为 6。
	fresh := walkSnapshot(t, st, 3)
	require.Len(t, fresh, 6)
	assert.Equal(t, late.ID, fresh[0].ID, "新快照应把最新卡排在首位")
}

func TestPaginationEmptyStore(t *testing.T) {
	st := openTestStore(t)
	cards, snap, hasMore, err := st.PageFirst(20)
	require.NoError(t, err)
	assert.Empty(t, cards)
	assert.False(t, hasMore)
	assert.Equal(t, Snapshot{}, snap)
}

// 正好整除页大小：最后一页取满时 hasMore 必须为 false，不能多翻出空页。
func TestPaginationExactPageBoundary(t *testing.T) {
	st := openTestStore(t)
	for i := 1; i <= 6; i++ {
		insertCardAt(t, st, i, fmt.Sprintf("2026-09-17T08:%02d:00Z", i))
	}
	got := walkSnapshot(t, st, 3)
	assert.Len(t, got, 6)
}

// SaveCard 落库的真实时间戳（秒精度）也能稳定翻页：连续快速签发一批，
// 多条会同秒，靠 (issued_at, id) 键集保证不重不漏。
func TestPaginationWithRealSaveCards(t *testing.T) {
	st := openTestStore(t)
	const n = 25
	for i := 1; i <= n; i++ {
		_, err := st.SaveCard(i%1000, (i*7)%1000, fmt.Sprintf("%09d", i))
		require.NoError(t, err)
	}
	got := walkSnapshot(t, st, 10)
	require.Len(t, got, n)
	seen := map[string]bool{}
	for _, c := range got {
		assert.False(t, seen[c.Code], "短码 %s 重复", c.Code)
		seen[c.Code] = true
	}
}
