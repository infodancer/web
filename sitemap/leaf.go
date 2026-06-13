package sitemap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LeafConfig configures a single-source sitemap leaf.
type LeafConfig struct {
	// BaseURL is the canonical origin, e.g. "https://speculativefiction.org".
	// Root-relative Entry Locs are joined to it; the leaf's own index/part URLs
	// are built from it too. Required.
	BaseURL string
	// PublicPrefix is the path prefix where this leaf is publicly mounted, used
	// only to build the leaf's own URLs (index and parts). A module mounted at
	// /faq via StripPrefix sets "/faq"; a host-root leaf leaves it "". This is
	// independent of the routing patterns, which are always relative so the leaf
	// works whether or not the host strips a prefix.
	PublicPrefix string
	// Name is the leaf's basename, letting several leaves coexist at one mount
	// level. Defaults to "sitemap". The index is served at {Name}.xml and parts
	// at {Name}/{n}.xml; public URLs are {BaseURL}{PublicPrefix}/{Name}.xml etc.
	Name string
	// Source supplies the URLs. If it also implements PagedSource, the leaf
	// paginates through it; otherwise the source is materialized (and cached).
	Source Source
	// CacheTTL caches rendered documents (and, for plain sources, the
	// materialized entries) for this long. Zero regenerates every request. Each
	// leaf chooses its own policy.
	CacheTTL time.Duration
	// MaxPerPage caps URLs per part. Defaults to MaxURLsPerFile (50000); a
	// smaller value is mainly useful in tests.
	MaxPerPage int
}

// Leaf serves one Source as a sitemap: a <urlset> when it fits in one page,
// auto-promoting to a <sitemapindex> over paginated parts past MaxPerPage.
type Leaf struct {
	cfg   LeafConfig
	cache *docCache

	mu         sync.Mutex
	matEntries []Entry
	matAt      time.Time
	matErr     error
	matValid   bool
}

// NewLeaf validates cfg and returns a Leaf.
func NewLeaf(cfg LeafConfig) (*Leaf, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("sitemap: LeafConfig.BaseURL is required")
	}
	if cfg.Source == nil {
		return nil, errors.New("sitemap: LeafConfig.Source is required")
	}
	if cfg.Name == "" {
		cfg.Name = "sitemap"
	}
	if cfg.MaxPerPage <= 0 {
		cfg.MaxPerPage = MaxURLsPerFile
	}
	return &Leaf{cfg: cfg, cache: newDocCache(cfg.CacheTTL)}, nil
}

// Register adds the leaf's two routes to mux: the index/urlset at
// "GET /{Name}.xml" and parts at "GET /{Name}/{n}". Patterns are relative, so
// a module that mounts mux under a stripped prefix and a host that mounts at
// the root both work -- public URL text comes from PublicPrefix, not the route.
func (l *Leaf) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /"+l.cfg.Name+".xml", l.serveRoot)
	mux.HandleFunc("GET /"+l.cfg.Name+"/{n}", l.servePart)
}

// Routes returns a fresh ServeMux with the leaf's routes registered, for
// standalone mounting or testing. Modules with their own mux call Register.
func (l *Leaf) Routes() http.Handler {
	mux := http.NewServeMux()
	l.Register(mux)
	return mux
}

// serveRoot handles GET /{Name}.xml: a single <urlset> when the source fits in
// one page, else a <sitemapindex> over the parts.
func (l *Leaf) serveRoot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	total, err := l.count(ctx)
	if err != nil {
		l.fail(w, err)
		return
	}
	if total <= l.cfg.MaxPerPage {
		body, err := l.cache.get("root", func() ([]byte, error) {
			return l.renderURLSet(ctx, 0, l.cfg.MaxPerPage)
		})
		l.write(w, body, err)
		return
	}
	parts := partCount(total, l.cfg.MaxPerPage)
	body, err := l.cache.get("index", func() ([]byte, error) { return l.renderSubIndex(parts) })
	l.write(w, body, err)
}

// servePart handles GET /{Name}/{n}: the <urlset> for part n (1-based). Accepts
// both "{n}" and "{n}.xml".
func (l *Leaf) servePart(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSuffix(r.PathValue("n"), ".xml")
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	total, err := l.count(ctx)
	if err != nil {
		l.fail(w, err)
		return
	}
	if n > partCount(total, l.cfg.MaxPerPage) {
		http.NotFound(w, r)
		return
	}
	offset := (n - 1) * l.cfg.MaxPerPage
	body, err := l.cache.get("part:"+strconv.Itoa(n), func() ([]byte, error) {
		return l.renderURLSet(ctx, offset, l.cfg.MaxPerPage)
	})
	l.write(w, body, err)
}

// count returns the source's total entry count, using PagedSource when
// available and otherwise materializing.
func (l *Leaf) count(ctx context.Context) (int, error) {
	if p, ok := l.cfg.Source.(PagedSource); ok {
		return p.SitemapCount(ctx)
	}
	entries, err := l.materialize(ctx)
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}

// renderURLSet renders a <urlset> for entries [offset, offset+limit).
func (l *Leaf) renderURLSet(ctx context.Context, offset, limit int) ([]byte, error) {
	out := urlSet{Xmlns: xmlNamespace}
	collect := func(e Entry) bool {
		out.URLs = append(out.URLs, urlEntry{
			Loc:     joinURL(l.cfg.BaseURL, e.Loc),
			LastMod: formatLastMod(e.LastMod),
		})
		return true
	}
	if p, ok := l.cfg.Source.(PagedSource); ok {
		if err := p.SitemapPage(ctx, offset, limit, collect); err != nil {
			return nil, err
		}
	} else {
		entries, err := l.materialize(ctx)
		if err != nil {
			return nil, err
		}
		end := min(offset+limit, len(entries))
		for i := offset; i < end; i++ {
			collect(entries[i])
		}
	}
	return renderXML(out)
}

// renderSubIndex renders the <sitemapindex> a leaf serves when its source
// overflows one page, referencing its own parts.
func (l *Leaf) renderSubIndex(parts int) ([]byte, error) {
	stem := selfStem(l.cfg.BaseURL, l.cfg.PublicPrefix, l.cfg.Name)
	out := sitemapIndex{Xmlns: xmlNamespace}
	for i := 1; i <= parts; i++ {
		out.Sitemaps = append(out.Sitemaps, sitemapEntry{
			Loc: fmt.Sprintf("%s/%d.xml", stem, i),
		})
	}
	return renderXML(out)
}

// materialize streams a plain (non-paged) source into a slice once per cache
// window, so count and the page renders within a request share one pass. With
// CacheTTL zero it streams on each call.
func (l *Leaf) materialize(ctx context.Context) ([]Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.matValid && l.cfg.CacheTTL > 0 && time.Since(l.matAt) < l.cfg.CacheTTL {
		return l.matEntries, l.matErr
	}
	var entries []Entry
	err := l.cfg.Source.SitemapEntries(ctx, func(e Entry) bool {
		entries = append(entries, e)
		return true
	})
	l.matEntries, l.matErr, l.matAt, l.matValid = entries, err, time.Now(), true
	return entries, err
}

func (l *Leaf) write(w http.ResponseWriter, body []byte, err error) {
	if err != nil {
		l.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write(body)
}

func (l *Leaf) fail(w http.ResponseWriter, err error) {
	http.Error(w, "sitemap: "+err.Error(), http.StatusInternalServerError)
}
