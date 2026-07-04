package sitemap

import (
	"errors"
	"net/http"
	"time"
)

// ChildRef is one child sitemap referenced by an Index -- typically a leaf's
// index document (e.g. "/faq/sitemap.xml").
type ChildRef struct {
	// Loc is the child sitemap's location: a root-relative path joined to the
	// Index's BaseURL, or an already-absolute URL.
	Loc string
	// LastMod is optional; the zero value omits <lastmod>.
	LastMod time.Time
}

// IndexConfig configures the host's root sitemap index.
type IndexConfig struct {
	// BaseURL is the canonical origin used to absolutize child Locs. Required.
	BaseURL string
	// Name is the served basename; defaults to "sitemap" (served at /{Name}.xml,
	// i.e. the conventional /sitemap.xml).
	Name string
	// Children are the child sitemaps to list, supplied by the host (it knows
	// its own mount paths). The forced-inclusion gate is what keeps this list
	// honest; the Index does not discover children.
	Children []ChildRef
	// CacheTTL caches the rendered index for this long. Zero regenerates every
	// request. A lazy TTL suits the root index, which is cheap but crawler-hit.
	CacheTTL time.Duration
}

// Index serves a <sitemapindex> over a fixed list of child sitemaps.
type Index struct {
	cfg   IndexConfig
	cache *docCache
}

// NewIndex validates cfg and returns an Index.
func NewIndex(cfg IndexConfig) (*Index, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("sitemap: IndexConfig.BaseURL is required")
	}
	if cfg.Name == "" {
		cfg.Name = "sitemap"
	}
	return &Index{cfg: cfg, cache: newDocCache(cfg.CacheTTL)}, nil
}

// Register adds the index route to mux at "GET /{Name}.xml".
func (i *Index) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /"+i.cfg.Name+".xml", i.serve)
}

// Routes returns a fresh ServeMux with the index route registered.
func (i *Index) Routes() http.Handler {
	mux := http.NewServeMux()
	i.Register(mux)
	return mux
}

func (i *Index) serve(w http.ResponseWriter, r *http.Request) {
	body, err := i.cache.get("index", i.render)
	if err != nil {
		http.Error(w, "sitemap: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	// See Leaf.write: xml.Marshal output, served as XML with sniffing forbidden.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(body)
}

func (i *Index) render() ([]byte, error) {
	out := sitemapIndex{Xmlns: xmlNamespace}
	for _, c := range i.cfg.Children {
		out.Sitemaps = append(out.Sitemaps, sitemapEntry{
			Loc:     joinURL(i.cfg.BaseURL, c.Loc),
			LastMod: formatLastMod(c.LastMod),
		})
	}
	return renderXML(out)
}
