package paging_test

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/infodancer/web/paging"
	"github.com/stretchr/testify/require"
)

// fetcher returns a fetch closure over an in-memory slice of n sequential ints,
// honoring limit/offset exactly as a SQL LIMIT/OFFSET would. It records the
// limit it was called with so tests can assert the over-fetch.
func fetcher(n int, gotLimit *int) func(limit, offset int) ([]int, error) {
	return func(limit, offset int) ([]int, error) {
		if gotLimit != nil {
			*gotLimit = limit
		}
		var out []int
		for i := offset; i < n && len(out) < limit; i++ {
			out = append(out, i)
		}
		return out, nil
	}
}

func TestPaginate_OverFetchesByOneAndTrims(t *testing.T) {
	var limit int
	// 10 rows, page size 3, first page: over-fetch asks for 4, returns 3 + HasNext.
	page, err := paging.Paginate(paging.Params{Offset: 0, PageSize: 3}, fetcher(10, &limit))
	require.NoError(t, err)
	require.Equal(t, 4, limit, "fetch is called with PageSize+1")
	require.Equal(t, []int{0, 1, 2}, page.Items, "the sentinel row is trimmed off")
	require.False(t, page.HasPrev, "first page has no prev")
	require.True(t, page.HasNext, "the extra row signals a next page")
	require.Equal(t, 0, page.Offset)
	require.Equal(t, 3, page.PageSize)
}

func TestPaginate_LastPageHasNoNext(t *testing.T) {
	// 10 rows, page size 3, offset 9: only row 9 remains, no sentinel.
	page, err := paging.Paginate(paging.Params{Offset: 9, PageSize: 3}, fetcher(10, nil))
	require.NoError(t, err)
	require.Equal(t, []int{9}, page.Items)
	require.True(t, page.HasPrev, "offset>0 means a prev page exists")
	require.False(t, page.HasNext, "no extra row, so no next page")
}

func TestPaginate_ExactFullPageHasNoNext(t *testing.T) {
	// 6 rows, page size 3, offset 3: rows 3,4,5 with nothing after — boundary case
	// where the page is full but there is no further row.
	page, err := paging.Paginate(paging.Params{Offset: 3, PageSize: 3}, fetcher(6, nil))
	require.NoError(t, err)
	require.Equal(t, []int{3, 4, 5}, page.Items)
	require.False(t, page.HasNext, "a full final page still reports no next")
}

func TestPaginate_ClampsBadParams(t *testing.T) {
	var limit int
	page, err := paging.Paginate(paging.Params{Offset: -5, PageSize: 0}, fetcher(10, &limit))
	require.NoError(t, err)
	require.Equal(t, 2, limit, "PageSize<1 clamps to 1, so over-fetch asks for 2")
	require.Equal(t, 0, page.Offset, "negative offset clamps to 0")
	require.Equal(t, 1, page.PageSize)
	require.False(t, page.HasPrev)
}

func TestPaginate_PropagatesFetchError(t *testing.T) {
	want := errors.New("db down")
	page, err := paging.Paginate(paging.Params{PageSize: 5}, func(limit, offset int) ([]int, error) {
		return nil, want
	})
	require.ErrorIs(t, err, want)
	require.Zero(t, page, "a fetch error yields the zero Page")
}

func TestParseParams_OffsetAndSize(t *testing.T) {
	cases := []struct {
		query      string
		wantOffset int
		wantSize   int
	}{
		{"", 0, 50},                   // defaults
		{"offset=100", 100, 50},       // offset honored
		{"offset=-3", 0, 50},          // negative offset clamps to 0
		{"offset=abc", 0, 50},         // garbage offset ignored
		{"size=25", 0, 25},            // user size within bounds
		{"size=999", 0, 200},          // size clamped to maxSize
		{"size=0", 0, 50},             // non-positive size ignored -> default
		{"offset=20&size=10", 20, 10}, // both
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/list?"+tc.query, nil)
			p := paging.ParseParams(r, 50, 200)
			require.Equal(t, tc.wantOffset, p.Offset)
			require.Equal(t, tc.wantSize, p.PageSize)
		})
	}
}

func TestParseParams_NoMaxBound(t *testing.T) {
	r := httptest.NewRequest("GET", "/list?size=5000", nil)
	p := paging.ParseParams(r, 50, 0) // maxSize 0 => unbounded
	require.Equal(t, 5000, p.PageSize)
}

// Example wiring: Paginate over a typed query, ready for ui.NewPager.
func ExamplePaginate() {
	page, _ := paging.Paginate(paging.Params{Offset: 0, PageSize: 2},
		func(limit, offset int) ([]string, error) {
			all := []string{"a", "b", "c"}
			end := offset + limit
			if end > len(all) {
				end = len(all)
			}
			return all[offset:end], nil
		})
	fmt.Println(page.Items, page.HasNext)
	// Output: [a b] true
}
