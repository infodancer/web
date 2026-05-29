package admin

import (
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"time"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/notify"
)

// pageData is the view model shared by the feed page and its fragments.
type pageData struct {
	BasePath    string
	SiteName    string
	HomeURL     string
	PageTitle   string
	HTMXHead    template.HTML
	PollSeconds int

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

type filterVM struct {
	Type     string
	Source   string
	Severity string
	Since    string
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
	id, ok := h.resolve(w, r)
	if !ok {
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

	unread, err := h.store.AdminUnreadCount(ctx, id)
	if err != nil {
		h.log.Error("admin: unread count", "err", err)
		unread = 0
	}

	data := h.basePageData()
	data.Unread = unread
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
	data.PageTitle = "Notifications"
	h.tpl.render(w, "pages/feed.gohtml", data)
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
	data := h.basePageData()
	data.Unread = unread
	h.tpl.render(w, "partials/bell.gohtml", data)
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
	data := h.basePageData()
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

func (h *Handler) basePageData() pageData {
	return pageData{
		BasePath:    h.basePath,
		SiteName:    h.siteName,
		HomeURL:     h.homeURL,
		HTMXHead:    template.HTML(h.htmxHead), //nolint:gosec // produced by ui.HeadTags
		PollSeconds: h.pollSeconds,
	}
}

func (h *Handler) parseFilter(r *http.Request) (notify.Filter, filterVM) {
	q := r.URL.Query()
	f := notify.Filter{
		Source:   q.Get("source"),
		Type:     q.Get("type"),
		Severity: notify.Severity(q.Get("severity")),
		Limit:    h.pageSize,
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
