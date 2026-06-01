package admin

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/notify"
)

// feedData is the feed view model. The full page wraps it in a [view]; the bell
// and feed-list fragments render it directly, so a poll never builds the page
// chrome.
type feedData struct {
	BasePath string

	Unread int
	Filter filterVM
	Events []eventVM

	// NextBeforeID is the keyset cursor for the next (older) page, or 0 when
	// there are no more rows. MoreQuery is the encoded query string for it.
	NextBeforeID int64
	MoreQuery    string

	// NewestCreatedAt is the RFC3339Nano timestamp of the newest event on the
	// page, posted to /seen so "mark all read" advances the cursor exactly to
	// what the admin saw.
	NewestCreatedAt string
}

// view is the full-page envelope. The embedded [ui.DocumentData] carries the
// host chrome (nav, footer, meta, CSS, analytics, theme, sidebars) that
// base.gohtml renders through ui/document; Page carries the feed data the
// content block — and the fragments it includes — reference.
type view struct {
	uiassets.DocumentData
	BasePath  string
	PageTitle string
	Page      feedData
}

type filterVM struct {
	Type     string
	Source   string
	Severity string
	Since    string
	// Scope is "all" for the full activity firehose, "" for the curated
	// admin-audience stream. Drives the feed's scope toggle.
	Scope string
}

type eventVM struct {
	ID            int64
	Type          string
	Source        string
	Title         string
	Severity      string
	SeverityClass string
	ActorLabel    string
	CreatedAt     time.Time
	RelTime       string
}

// feed serves GET / — the admin event feed. Direct navigation gets the full
// page; an htmx request gets the feed-list fragment.
func (h *Handler) feed(w http.ResponseWriter, r *http.Request) {
	// Auth gate; the feed page needs no admin identity (the bell, which does,
	// is the separate GET /bell fragment).
	if _, ok := h.resolve(w, r); !ok {
		return
	}
	ctx := r.Context()

	f, fvm := h.parseFilter(r)
	events, err := h.store.AdminFeed(ctx, f)
	if err != nil {
		h.log.Error("admin: feed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	data := h.baseFeedData()
	data.Filter = fvm
	data.Events = h.toEventVMs(events)
	if len(events) > 0 {
		data.NewestCreatedAt = events[0].CreatedAt.Format(time.RFC3339Nano)
	}
	if len(events) == f.Limit && len(events) > 0 {
		data.NextBeforeID = events[len(events)-1].ID
		data.MoreQuery = moreQuery(fvm, data.NextBeforeID)
	}

	if uiassets.IsRequest(r) && !uiassets.IsBoosted(r) {
		h.tpl.render(w, "partials/feed_list.gohtml", data)
		return
	}
	h.tpl.render(w, "pages/feed.gohtml", h.buildView(r, "Notifications", data))
}

// bell serves GET /bell — the unread-count fragment the nav polls.
func (h *Handler) bell(w http.ResponseWriter, r *http.Request) {
	id, ok := h.resolve(w, r)
	if !ok {
		return
	}
	unread, err := h.store.AdminUnreadCount(r.Context(), id)
	if err != nil {
		h.log.Error("admin: unread count", "err", err)
		unread = 0
	}
	data := h.baseFeedData()
	data.Unread = unread
	h.tpl.render(w, "partials/bell.gohtml", data)
}

// bellCount serves GET /bell/count — the bare unread number a host's nav badge
// polls. Unlike GET /bell (the whole anchor), this returns just the count text
// (empty when zero), so it drops into the innerHTML of infodancer/ui's
// .app-nav-badge span, whose ui.Badge.PollURL points here.
func (h *Handler) bellCount(w http.ResponseWriter, r *http.Request) {
	id, ok := h.resolve(w, r)
	if !ok {
		return
	}
	unread, err := h.store.AdminUnreadCount(r.Context(), id)
	if err != nil {
		h.log.Error("admin: unread count", "err", err)
		unread = 0
	}
	data := h.baseFeedData()
	data.Unread = unread
	h.tpl.render(w, "partials/bell_count.gohtml", data)
}

// seen serves POST /seen — advances the admin's read cursor to up_to (the
// newest rendered event), then returns the refreshed bell fragment.
func (h *Handler) seen(w http.ResponseWriter, r *http.Request) {
	id, ok := h.resolve(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4<<10) // the form carries one short timestamp
	var upTo time.Time
	if v := r.FormValue("up_to"); v != "" {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			upTo = t
		}
	}
	if err := h.store.AdminMarkSeen(r.Context(), id, upTo); err != nil {
		h.log.Error("admin: mark seen", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	unread, err := h.store.AdminUnreadCount(r.Context(), id)
	if err != nil {
		h.log.Error("admin: unread count", "err", err)
		unread = 0
	}
	uiassets.Trigger(w, "notify:read")
	data := h.baseFeedData()
	data.Unread = unread
	h.tpl.render(w, "partials/bell.gohtml", data)
}

// resolve returns the admin identity, or writes 403 and reports false.
func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) (notify.Identity, bool) {
	id, ok := h.resolver.Resolve(r)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return notify.Identity{}, false
	}
	return id, true
}

func (h *Handler) baseFeedData() feedData {
	return feedData{BasePath: h.basePath}
}

// buildView wraps feed data in the full-page envelope, resolving the host
// chrome (Config.Chrome) or the module default and tagging it with the page title.
func (h *Handler) buildView(r *http.Request, title string, data feedData) view {
	var doc uiassets.DocumentData
	if h.chrome != nil {
		doc = h.chrome(r)
	} else {
		doc = h.defaultChrome()
	}
	if doc.Meta.SiteName == "" {
		doc.Meta.SiteName = h.siteName
	}
	return view{
		DocumentData: doc,
		BasePath:     h.basePath,
		PageTitle:    title,
		Page:         data,
	}
}

// defaultChrome is the standalone chrome used when no host Chrome hook is set:
// the ui CSS + htmx served from the module's own /static/ui mount, and a brand
// nav/footer from SiteName/HomeURL.
func (h *Handler) defaultChrome() uiassets.DocumentData {
	assetBase := h.basePath + "/static/ui"
	return uiassets.DocumentData{
		AssetBase: assetBase,
		HeadTags:  uiassets.HeadTags(assetBase),
		Meta:      uiassets.Meta{SiteName: h.siteName},
		Nav:       uiassets.NavData{BrandText: h.siteName, BrandURL: h.homeURL},
		Footer:    uiassets.FooterData{BrandText: h.siteName, BrandURL: h.homeURL},
	}
}

func (h *Handler) parseFilter(r *http.Request) (notify.Filter, filterVM) {
	q := r.URL.Query()
	scope := q.Get("scope")
	f := notify.Filter{
		Source:       q.Get("source"),
		Type:         q.Get("type"),
		Severity:     notify.Severity(q.Get("severity")),
		Limit:        h.pageSize,
		AllAudiences: scope == "all",
	}
	if b := q.Get("before"); b != "" {
		if n, err := strconv.ParseInt(b, 10, 64); err == nil && n > 0 {
			f.BeforeID = n
		}
	}
	since := q.Get("since")
	if since != "" {
		if t, err := time.Parse("2006-01-02", since); err == nil {
			f.Since = t
		}
	}
	return f, filterVM{
		Type:     f.Type,
		Source:   f.Source,
		Severity: string(f.Severity),
		Since:    since,
		Scope:    scope,
	}
}

func (h *Handler) toEventVMs(events []notify.Event) []eventVM {
	now := h.now()
	out := make([]eventVM, 0, len(events))
	for _, ev := range events {
		vm := eventVM{
			ID:            ev.ID,
			Type:          ev.Type,
			Source:        ev.Source,
			Title:         ev.Title,
			Severity:      string(ev.Severity),
			SeverityClass: "notify-sev-" + string(ev.Severity),
			CreatedAt:     ev.CreatedAt,
			RelTime:       relTime(now, ev.CreatedAt),
		}
		if ev.Actor != nil {
			vm.ActorLabel = ev.Actor.Label
		}
		out = append(out, vm)
	}
	return out
}

// moreQuery encodes the current filters plus the keyset cursor for the
// load-older control.
func moreQuery(f filterVM, before int64) string {
	v := url.Values{}
	if f.Type != "" {
		v.Set("type", f.Type)
	}
	if f.Source != "" {
		v.Set("source", f.Source)
	}
	if f.Severity != "" {
		v.Set("severity", f.Severity)
	}
	if f.Since != "" {
		v.Set("since", f.Since)
	}
	if f.Scope != "" {
		v.Set("scope", f.Scope)
	}
	v.Set("before", strconv.FormatInt(before, 10))
	return v.Encode()
}

// relTime renders a compact "time ago" string for the feed.
func relTime(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < 0:
		return "just now"
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h ago"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d ago"
	}
}
