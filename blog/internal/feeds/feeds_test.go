package feeds

import (
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"
	"time"
)

func sampleFeed() Feed {
	pub := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	upd := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)
	return Feed{
		Title:       "Old School Gamers — Admin Notes",
		Description: "Site announcements & dev notes",
		SiteURL:     "https://example.org/blog/b/admin-notes",
		FeedURL:     "https://example.org/blog/b/admin-notes/feed.atom",
		Updated:     upd,
		Items: []Item{
			{
				Title:       "Dice & <Dragons>",
				URL:         "https://example.org/blog/p/dice-dragons",
				Summary:     "On dice, dragons & escaping.",
				ContentHTML: "<p>Hello <strong>world</strong> &amp; friends</p>",
				AuthorName:  "Matthew",
				AuthorURL:   "https://example.org/blog/authors/matthew",
				Tags:        []string{"announcements", "osr"},
				Published:   pub,
				Updated:     upd,
			},
			{
				Title:     "Second post",
				URL:       "https://example.org/blog/p/second-post",
				Published: pub.Add(-24 * time.Hour),
				Updated:   pub.Add(-24 * time.Hour),
			},
		},
	}
}

func TestRSSRoundTrip(t *testing.T) {
	out, err := RSS(sampleFeed())
	if err != nil {
		t.Fatalf("RSS: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		`<rss version="2.0"`,
		"<title>Old School Gamers — Admin Notes</title>",
		"Dice &amp; &lt;Dragons&gt;", // titles escaped by the encoder
		`<guid isPermaLink="true">https://example.org/blog/p/dice-dragons</guid>`,
		"<dc:creator>Matthew</dc:creator>",
		"<category>announcements</category>",
		"<![CDATA[<p>Hello <strong>world</strong> &amp; friends</p>]]>",
		`rel="self"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("RSS output missing %q\n%s", want, s)
		}
	}

	// The document must re-parse as XML.
	var parsed struct {
		Channel struct {
			Title string `xml:"title"`
			Items []struct {
				Title   string `xml:"title"`
				PubDate string `xml:"pubDate"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("re-parse RSS: %v", err)
	}
	if len(parsed.Channel.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(parsed.Channel.Items))
	}
	if parsed.Channel.Items[0].Title != "Dice & <Dragons>" {
		t.Errorf("title round-trip = %q", parsed.Channel.Items[0].Title)
	}
	if got := parsed.Channel.Items[0].PubDate; !strings.Contains(got, "Jun 2026") {
		t.Errorf("pubDate = %q, want RFC1123Z in June 2026", got)
	}
}

func TestAtomRoundTrip(t *testing.T) {
	out, err := Atom(sampleFeed())
	if err != nil {
		t.Fatalf("Atom: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		`<feed xmlns="http://www.w3.org/2005/Atom">`,
		"<updated>2026-06-02T09:30:00Z</updated>",
		`<category term="osr">`,
		`<content type="html">`,
		"<uri>https://example.org/blog/authors/matthew</uri>",
		`rel="self"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("Atom output missing %q\n%s", want, s)
		}
	}

	var parsed struct {
		Entries []struct {
			ID      string `xml:"id"`
			Content string `xml:"content"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("re-parse Atom: %v", err)
	}
	if len(parsed.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(parsed.Entries))
	}
	// The escaped HTML must round-trip back to the sanitized original.
	if got := parsed.Entries[0].Content; got != "<p>Hello <strong>world</strong> &amp; friends</p>" {
		t.Errorf("content round-trip = %q", got)
	}
}

func TestJSONFeed(t *testing.T) {
	out, err := JSON(sampleFeed())
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var parsed struct {
		Version string `json:"version"`
		Title   string `json:"title"`
		FeedURL string `json:"feed_url"`
		Items   []struct {
			ID          string   `json:"id"`
			ContentHTML string   `json:"content_html"`
			Date        string   `json:"date_published"`
			Tags        []string `json:"tags"`
			Authors     []struct {
				Name string `json:"name"`
			} `json:"authors"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("re-parse JSON Feed: %v", err)
	}
	if parsed.Version != "https://jsonfeed.org/version/1.1" {
		t.Errorf("version = %q", parsed.Version)
	}
	if len(parsed.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(parsed.Items))
	}
	it := parsed.Items[0]
	if it.ContentHTML != "<p>Hello <strong>world</strong> &amp; friends</p>" {
		t.Errorf("content_html = %q", it.ContentHTML)
	}
	if it.Date != "2026-06-01T12:00:00Z" {
		t.Errorf("date_published = %q", it.Date)
	}
	if len(it.Authors) != 1 || it.Authors[0].Name != "Matthew" {
		t.Errorf("authors = %+v", it.Authors)
	}
	if len(it.Tags) != 2 {
		t.Errorf("tags = %v", it.Tags)
	}
}

func TestEmptyFeed(t *testing.T) {
	f := Feed{Title: "Empty", SiteURL: "https://example.org/blog", FeedURL: "https://example.org/blog/feed.xml"}
	for name, fn := range map[string]func(Feed) ([]byte, error){"rss": RSS, "atom": Atom, "json": JSON} {
		out, err := fn(f)
		if err != nil {
			t.Fatalf("%s on empty feed: %v", name, err)
		}
		if len(out) == 0 {
			t.Fatalf("%s produced no output", name)
		}
	}
}
