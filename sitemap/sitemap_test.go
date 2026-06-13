package sitemap_test

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/infodancer/web/sitemap"
)

// sliceSource is a plain Source backed by a slice; it records how many times it
// was streamed so tests can assert caching.
type sliceSource struct {
	entries []sitemap.Entry
	streams int
}

func (s *sliceSource) SitemapEntries(_ context.Context, yield func(sitemap.Entry) bool) error {
	s.streams++
	for _, e := range s.entries {
		if !yield(e) {
			return nil
		}
	}
	return nil
}

// pagedSource adds the PagedSource optimization over a slice.
type pagedSource struct {
	entries []sitemap.Entry
	counts  int
	pages   int
}

func (s *pagedSource) SitemapEntries(_ context.Context, yield func(sitemap.Entry) bool) error {
	for _, e := range s.entries {
		if !yield(e) {
			return nil
		}
	}
	return nil
}

func (s *pagedSource) SitemapCount(context.Context) (int, error) {
	s.counts++
	return len(s.entries), nil
}

func (s *pagedSource) SitemapPage(_ context.Context, offset, limit int, yield func(sitemap.Entry) bool) error {
	s.pages++
	end := min(offset+limit, len(s.entries))
	for i := offset; i < end && i >= 0; i++ {
		if !yield(s.entries[i]) {
			return nil
		}
	}
	return nil
}

func makeEntries(n int) []sitemap.Entry {
	out := make([]sitemap.Entry, n)
	for i := range out {
		out[i] = sitemap.Entry{Loc: fmt.Sprintf("/book/%d", i)}
	}
	return out
}

func get(t *testing.T, h http.Handler, target string) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest("GET", target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	res := rec.Result()
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	return res, string(body)
}

func TestLeafSingleURLSet(t *testing.T) {
	src := &sliceSource{entries: []sitemap.Entry{
		{Loc: "/book/a", LastMod: time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)},
		{Loc: "/book/b"},
		{Loc: "https://elsewhere.example/x"},
	}}
	leaf, err := sitemap.NewLeaf(sitemap.LeafConfig{BaseURL: "https://sf.org", Source: src})
	if err != nil {
		t.Fatal(err)
	}
	res, body := get(t, leaf.Routes(), "/sitemap.xml")
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/xml") {
		t.Errorf("content-type %q", ct)
	}
	if !strings.Contains(body, "<urlset") {
		t.Errorf("expected <urlset>, got:\n%s", body)
	}
	// Root-relative joined to BaseURL; absolute passed through; lastmod present
	// only where set.
	for _, want := range []string{
		"<loc>https://sf.org/book/a</loc>",
		"<lastmod>2026-06-13</lastmod>",
		"<loc>https://sf.org/book/b</loc>",
		"<loc>https://elsewhere.example/x</loc>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
	// /book/b has no lastmod -- exactly one <lastmod> in the doc.
	if n := strings.Count(body, "<lastmod>"); n != 1 {
		t.Errorf("lastmod count = %d, want 1", n)
	}
	// Well-formed XML.
	if err := xml.Unmarshal([]byte(body), new(struct{})); err != nil {
		t.Errorf("malformed xml: %v", err)
	}
}

func TestLeafOverflowToSubIndex(t *testing.T) {
	src := &sliceSource{entries: makeEntries(25)}
	leaf, err := sitemap.NewLeaf(sitemap.LeafConfig{
		BaseURL:      "https://sf.org",
		PublicPrefix: "/faq",
		Source:       src,
		MaxPerPage:   10,
	})
	if err != nil {
		t.Fatal(err)
	}
	h := leaf.Routes()

	// Root promotes to a sitemapindex over 3 parts, addressed at the leaf's own
	// public stem (/faq/sitemap/N.xml).
	res, body := get(t, h, "/sitemap.xml")
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	if !strings.Contains(body, "<sitemapindex") {
		t.Fatalf("expected sitemapindex, got:\n%s", body)
	}
	for i := 1; i <= 3; i++ {
		want := fmt.Sprintf("<loc>https://sf.org/faq/sitemap/%d.xml</loc>", i)
		if !strings.Contains(body, want) {
			t.Errorf("index missing %q:\n%s", want, body)
		}
	}

	// Part 2 is the middle window of 10.
	res, body = get(t, h, "/sitemap/2")
	if res.StatusCode != 200 {
		t.Fatalf("part 2 status %d", res.StatusCode)
	}
	if !strings.Contains(body, "<loc>https://sf.org/book/10</loc>") ||
		!strings.Contains(body, "<loc>https://sf.org/book/19</loc>") {
		t.Errorf("part 2 wrong window:\n%s", body)
	}
	if strings.Contains(body, "/book/9<") || strings.Contains(body, "/book/20<") {
		t.Errorf("part 2 leaked neighbors:\n%s", body)
	}

	// Last part has the remainder (5 entries).
	res, body = get(t, h, "/sitemap/3.xml")
	if res.StatusCode != 200 {
		t.Fatalf("part 3 status %d", res.StatusCode)
	}
	if n := strings.Count(body, "<url>"); n != 5 {
		t.Errorf("part 3 url count = %d, want 5", n)
	}

	// Out-of-range and bad parts 404.
	if res, _ := get(t, h, "/sitemap/4"); res.StatusCode != 404 {
		t.Errorf("part 4 status %d, want 404", res.StatusCode)
	}
	if res, _ := get(t, h, "/sitemap/0"); res.StatusCode != 404 {
		t.Errorf("part 0 status %d, want 404", res.StatusCode)
	}
	if res, _ := get(t, h, "/sitemap/x"); res.StatusCode != 404 {
		t.Errorf("part x status %d, want 404", res.StatusCode)
	}
}

func TestLeafPagedSourcePreferred(t *testing.T) {
	src := &pagedSource{entries: makeEntries(25)}
	leaf, err := sitemap.NewLeaf(sitemap.LeafConfig{BaseURL: "https://sf.org", Source: src, MaxPerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	h := leaf.Routes()
	if res, _ := get(t, h, "/sitemap.xml"); res.StatusCode != 200 {
		t.Fatalf("root status %d", res.StatusCode)
	}
	if res, body := get(t, h, "/sitemap/1"); res.StatusCode != 200 || !strings.Contains(body, "/book/0<") {
		t.Fatalf("part 1 wrong: %d\n%s", res.StatusCode, body)
	}
	if src.counts == 0 {
		t.Errorf("paged source SitemapCount never called")
	}
	if src.pages == 0 {
		t.Errorf("paged source SitemapPage never called")
	}
}

func TestLeafCacheTTL(t *testing.T) {
	src := &sliceSource{entries: makeEntries(3)}
	leaf, err := sitemap.NewLeaf(sitemap.LeafConfig{BaseURL: "https://sf.org", Source: src, CacheTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	h := leaf.Routes()
	for range 3 {
		if res, _ := get(t, h, "/sitemap.xml"); res.StatusCode != 200 {
			t.Fatalf("status %d", res.StatusCode)
		}
	}
	// One materialization across the three cached requests.
	if src.streams != 1 {
		t.Errorf("source streamed %d times, want 1 (cached)", src.streams)
	}
}

func TestLeafValidation(t *testing.T) {
	if _, err := sitemap.NewLeaf(sitemap.LeafConfig{Source: &sliceSource{}}); err == nil {
		t.Error("expected error for missing BaseURL")
	}
	if _, err := sitemap.NewLeaf(sitemap.LeafConfig{BaseURL: "https://x"}); err == nil {
		t.Error("expected error for missing Source")
	}
}

func TestIndex(t *testing.T) {
	idx, err := sitemap.NewIndex(sitemap.IndexConfig{
		BaseURL: "https://sf.org",
		Children: []sitemap.ChildRef{
			{Loc: "/sitemap-catalog.xml", LastMod: time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)},
			{Loc: "/faq/sitemap.xml"},
			{Loc: "https://sf.org/blog/sitemap.xml"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, body := get(t, idx.Routes(), "/sitemap.xml")
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	if !strings.Contains(body, "<sitemapindex") {
		t.Fatalf("expected sitemapindex:\n%s", body)
	}
	for _, want := range []string{
		"<loc>https://sf.org/sitemap-catalog.xml</loc>",
		"<lastmod>2026-06-13</lastmod>",
		"<loc>https://sf.org/faq/sitemap.xml</loc>",
		"<loc>https://sf.org/blog/sitemap.xml</loc>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index missing %q:\n%s", want, body)
		}
	}
	if n := strings.Count(body, "<lastmod>"); n != 1 {
		t.Errorf("lastmod count = %d, want 1", n)
	}
}
