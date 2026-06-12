package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/infodancer/web/profile/profileuser"
	"github.com/infodancer/web/profile/store"
)

// fakeStore is an in-memory ProfileStore mirroring the Postgres semantics the
// handlers depend on: upsert-by-key, claim-once handles, unique handles.
type fakeStore struct {
	mu       sync.Mutex
	profiles map[string]*store.Profile
}

func newFakeStore() *fakeStore {
	return &fakeStore{profiles: map[string]*store.Profile{}}
}

func (f *fakeStore) Get(_ context.Context, userKey string) (*store.Profile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.profiles[userKey]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (f *fakeStore) GetByHandle(_ context.Context, handle string) (*store.Profile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.profiles {
		if p.Handle == handle && handle != "" {
			cp := *p
			return &cp, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) Upsert(_ context.Context, params store.UpsertParams) (*store.Profile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.profiles[params.UserKey]
	if !ok {
		p = &store.Profile{UserKey: params.UserKey}
		f.profiles[params.UserKey] = p
	}
	p.DisplayName = params.DisplayName
	p.BioMD = params.BioMD
	p.Links = params.Links
	p.IsPublic = params.IsPublic
	cp := *p
	return &cp, nil
}

func (f *fakeStore) ClaimHandle(_ context.Context, userKey, handle string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.profiles[userKey]
	if !ok {
		return store.ErrNotFound
	}
	if p.Handle != "" {
		return store.ErrHandleAlreadySet
	}
	return f.setHandleLocked(p, handle)
}

func (f *fakeStore) SetHandle(_ context.Context, userKey, handle string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.profiles[userKey]
	if !ok {
		return store.ErrNotFound
	}
	return f.setHandleLocked(p, handle)
}

func (f *fakeStore) setHandleLocked(p *store.Profile, handle string) error {
	for _, other := range f.profiles {
		if other.UserKey != p.UserKey && other.Handle == handle {
			return store.ErrHandleTaken
		}
	}
	p.Handle = handle
	return nil
}

func (f *fakeStore) SetAvatarURL(_ context.Context, userKey, url string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.profiles[userKey]
	if !ok {
		return store.ErrNotFound
	}
	p.AvatarURL = url
	return nil
}

// stubResolver returns a fixed user (or nil for anonymous).
type stubResolver struct{ u *profileuser.User }

func (r stubResolver) Resolve(context.Context, *http.Request) (*profileuser.User, error) {
	return r.u, nil
}

const basePath = "/profile"

// newServer builds a handler Server over the fake store for the given user.
func newServer(t *testing.T, fs *fakeStore, u *profileuser.User, opts ...func(*Deps)) http.Handler {
	t.Helper()
	d := Deps{
		Store:    fs,
		Resolver: stubResolver{u: u},
		BasePath: basePath,
	}
	for _, o := range opts {
		o(&d)
	}
	s, err := New(d)
	if err != nil {
		t.Fatalf("handler.New: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle(basePath+"/", http.StripPrefix(basePath, s.Routes()))
	return mux
}

// do issues a request with same-origin headers (so the CSRF guard passes
// unless a test overrides them) and returns the response.
func do(t *testing.T, h http.Handler, method, target string, form map[string][]string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(url.Values(form).Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if method == http.MethodPost {
		req.Header.Set("Origin", "http://"+req.Host)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
