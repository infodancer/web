// Package feeds renders the blog's syndication documents: RSS 2.0,
// Atom 1.0 (RFC 4287), and JSON Feed 1.1. One [Feed] value describes a
// scope (combined, per-author, per-named-blog); the three builders render
// it byte-identically across scopes.
//
// Everything goes through encoding/xml and encoding/json — no string
// assembly — so item titles and bodies are escaped by the encoder, and the
// already-sanitized HTML content survives intact inside CDATA/encoded
// fields.
package feeds

import (
	"encoding/json"
	"encoding/xml"
	"time"
)

// Item is one post as a feed entry. URL must be absolute; ContentHTML must
// already be sanitized by the markdown pipeline.
type Item struct {
	Title       string
	URL         string
	Summary     string // plain-text excerpt
	ContentHTML string
	AuthorName  string
	AuthorURL   string // absolute author-page URL; "" omits
	Tags        []string
	Published   time.Time
	Updated     time.Time
}

// Feed describes one scope's feed.
type Feed struct {
	Title       string
	Description string
	SiteURL     string // the scope's HTML page (absolute)
	FeedURL     string // this feed document's own URL (absolute)
	Updated     time.Time
	Items       []Item
}

// --- RSS 2.0 ---

type rssRoot struct {
	XMLName   xml.Name   `xml:"rss"`
	Version   string     `xml:"version,attr"`
	NSContent string     `xml:"xmlns:content,attr"`
	NSAtom    string     `xml:"xmlns:atom,attr"`
	NSDC      string     `xml:"xmlns:dc,attr"`
	Channel   rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	LastBuildDate string    `xml:"lastBuildDate,omitempty"`
	AtomLink      rssSelf   `xml:"atom:link"`
	Items         []rssItem `xml:"item"`
}

type rssSelf struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

type rssItem struct {
	Title       string      `xml:"title"`
	Link        string      `xml:"link"`
	GUID        rssGUID     `xml:"guid"`
	PubDate     string      `xml:"pubDate,omitempty"`
	Creator     string      `xml:"dc:creator,omitempty"`
	Categories  []string    `xml:"category,omitempty"`
	Description string      `xml:"description,omitempty"`
	Content     *rssContent `xml:"content:encoded,omitempty"`
}

type rssGUID struct {
	IsPermaLink string `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

type rssContent struct {
	Value string `xml:",cdata"`
}

// RSS renders f as an RSS 2.0 document.
func RSS(f Feed) ([]byte, error) {
	ch := rssChannel{
		Title:       f.Title,
		Link:        f.SiteURL,
		Description: f.Description,
		AtomLink:    rssSelf{Href: f.FeedURL, Rel: "self", Type: "application/rss+xml"},
	}
	if !f.Updated.IsZero() {
		ch.LastBuildDate = f.Updated.UTC().Format(time.RFC1123Z)
	}
	for _, it := range f.Items {
		ri := rssItem{
			Title:       it.Title,
			Link:        it.URL,
			GUID:        rssGUID{IsPermaLink: "true", Value: it.URL},
			Creator:     it.AuthorName,
			Categories:  it.Tags,
			Description: it.Summary,
		}
		if !it.Published.IsZero() {
			ri.PubDate = it.Published.UTC().Format(time.RFC1123Z)
		}
		if it.ContentHTML != "" {
			ri.Content = &rssContent{Value: it.ContentHTML}
		}
		ch.Items = append(ch.Items, ri)
	}
	return marshalXML(rssRoot{
		Version:   "2.0",
		NSContent: "http://purl.org/rss/1.0/modules/content/",
		NSAtom:    "http://www.w3.org/2005/Atom",
		NSDC:      "http://purl.org/dc/elements/1.1/",
		Channel:   ch,
	})
}

// --- Atom 1.0 ---

type atomRoot struct {
	XMLName  xml.Name    `xml:"feed"`
	NS       string      `xml:"xmlns,attr"`
	ID       string      `xml:"id"`
	Title    string      `xml:"title"`
	Subtitle string      `xml:"subtitle,omitempty"`
	Updated  string      `xml:"updated"`
	Links    []atomLink  `xml:"link"`
	Entries  []atomEntry `xml:"entry"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr,omitempty"`
	Type string `xml:"type,attr,omitempty"`
}

type atomEntry struct {
	ID         string       `xml:"id"`
	Title      string       `xml:"title"`
	Link       atomLink     `xml:"link"`
	Published  string       `xml:"published,omitempty"`
	Updated    string       `xml:"updated"`
	Author     *atomAuthor  `xml:"author,omitempty"`
	Categories []atomCat    `xml:"category,omitempty"`
	Summary    string       `xml:"summary,omitempty"`
	Content    *atomContent `xml:"content,omitempty"`
}

type atomAuthor struct {
	Name string `xml:"name"`
	URI  string `xml:"uri,omitempty"`
}

type atomCat struct {
	Term string `xml:"term,attr"`
}

type atomContent struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

// Atom renders f as an Atom 1.0 document. Feed and entry ids are the
// permalink URLs, which are stable and unique within a site.
func Atom(f Feed) ([]byte, error) {
	updated := f.Updated
	if updated.IsZero() {
		updated = time.Unix(0, 0)
	}
	root := atomRoot{
		NS:       "http://www.w3.org/2005/Atom",
		ID:       f.FeedURL,
		Title:    f.Title,
		Subtitle: f.Description,
		Updated:  updated.UTC().Format(time.RFC3339),
		Links: []atomLink{
			{Href: f.SiteURL, Rel: "alternate", Type: "text/html"},
			{Href: f.FeedURL, Rel: "self", Type: "application/atom+xml"},
		},
	}
	for _, it := range f.Items {
		e := atomEntry{
			ID:      it.URL,
			Title:   it.Title,
			Link:    atomLink{Href: it.URL, Rel: "alternate", Type: "text/html"},
			Updated: it.Updated.UTC().Format(time.RFC3339),
			Summary: it.Summary,
		}
		if !it.Published.IsZero() {
			e.Published = it.Published.UTC().Format(time.RFC3339)
		}
		if it.AuthorName != "" {
			e.Author = &atomAuthor{Name: it.AuthorName, URI: it.AuthorURL}
		}
		for _, t := range it.Tags {
			e.Categories = append(e.Categories, atomCat{Term: t})
		}
		if it.ContentHTML != "" {
			e.Content = &atomContent{Type: "html", Value: it.ContentHTML}
		}
		root.Entries = append(root.Entries, e)
	}
	return marshalXML(root)
}

// --- JSON Feed 1.1 ---

type jsonFeed struct {
	Version     string     `json:"version"`
	Title       string     `json:"title"`
	HomePageURL string     `json:"home_page_url,omitempty"`
	FeedURL     string     `json:"feed_url,omitempty"`
	Description string     `json:"description,omitempty"`
	Items       []jsonItem `json:"items"`
}

type jsonItem struct {
	ID            string       `json:"id"`
	URL           string       `json:"url,omitempty"`
	Title         string       `json:"title,omitempty"`
	ContentHTML   string       `json:"content_html,omitempty"`
	Summary       string       `json:"summary,omitempty"`
	DatePublished string       `json:"date_published,omitempty"`
	DateModified  string       `json:"date_modified,omitempty"`
	Authors       []jsonAuthor `json:"authors,omitempty"`
	Tags          []string     `json:"tags,omitempty"`
}

type jsonAuthor struct {
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

// JSON renders f as a JSON Feed 1.1 document.
func JSON(f Feed) ([]byte, error) {
	out := jsonFeed{
		Version:     "https://jsonfeed.org/version/1.1",
		Title:       f.Title,
		HomePageURL: f.SiteURL,
		FeedURL:     f.FeedURL,
		Description: f.Description,
		Items:       []jsonItem{},
	}
	for _, it := range f.Items {
		ji := jsonItem{
			ID:          it.URL,
			URL:         it.URL,
			Title:       it.Title,
			ContentHTML: it.ContentHTML,
			Summary:     it.Summary,
			Tags:        it.Tags,
		}
		if !it.Published.IsZero() {
			ji.DatePublished = it.Published.UTC().Format(time.RFC3339)
		}
		if !it.Updated.IsZero() {
			ji.DateModified = it.Updated.UTC().Format(time.RFC3339)
		}
		if it.AuthorName != "" {
			ji.Authors = []jsonAuthor{{Name: it.AuthorName, URL: it.AuthorURL}}
		}
		out.Items = append(out.Items, ji)
	}
	return json.MarshalIndent(out, "", "  ")
}

func marshalXML(v any) ([]byte, error) {
	b, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), b...), nil
}
