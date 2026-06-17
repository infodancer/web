package obs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// recordingReporter captures the notices passed to Report for assertions.
type recordingReporter struct {
	mu      sync.Mutex
	notices []Notice
}

func (r *recordingReporter) Report(_ context.Context, n Notice) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notices = append(r.notices, n)
}

func (r *recordingReporter) all() []Notice {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Notice(nil), r.notices...)
}

// recordingMetric counts observations.
type recordingMetric struct {
	mu    sync.Mutex
	calls []int // statuses observed
}

func (m *recordingMetric) ObserveRequest(_, _ string, status int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, status)
}

func (m *recordingMetric) statuses() []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]int(nil), m.calls...)
}

func TestCaptureReports5xx(t *testing.T) {
	rep := &recordingReporter{}
	met := &recordingMetric{}
	h := Capture(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}), CaptureOptions{Component: "blog", Reporter: rep, Metric: met})

	req := httptest.NewRequest(http.MethodGet, "/posts", nil)
	req = req.WithContext(WithRequestID(req.Context(), "rid-1"))
	h.ServeHTTP(httptest.NewRecorder(), req)

	notices := rep.all()
	if len(notices) != 1 {
		t.Fatalf("got %d notices, want 1", len(notices))
	}
	n := notices[0]
	if n.Status != 500 || n.Component != "blog" || n.Method != http.MethodGet || n.Path != "/posts" || n.RequestID != "rid-1" {
		t.Fatalf("notice mismatch: %+v", n)
	}
	if n.Panic != nil || n.Stack != nil {
		t.Fatalf("plain 5xx should carry no panic/stack: %+v", n)
	}
	if got := met.statuses(); len(got) != 1 || got[0] != 500 {
		t.Fatalf("metric statuses = %v, want [500]", got)
	}
}

func TestCaptureIgnoresSuccess(t *testing.T) {
	rep := &recordingReporter{}
	met := &recordingMetric{}
	h := Capture(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}), CaptureOptions{Component: "blog", Reporter: rep, Metric: met})

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if n := rep.all(); len(n) != 0 {
		t.Fatalf("success should not report; got %d notices", len(n))
	}
	if got := met.statuses(); len(got) != 1 || got[0] != 200 {
		t.Fatalf("metric statuses = %v, want [200]", got)
	}
}

func TestCaptureImplicit200NotReported(t *testing.T) {
	rep := &recordingReporter{}
	met := &recordingMetric{}
	h := Capture(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("body without explicit WriteHeader"))
	}), CaptureOptions{Reporter: rep, Metric: met})

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if n := rep.all(); len(n) != 0 {
		t.Fatalf("implicit 200 should not report; got %d notices", len(n))
	}
	if got := met.statuses(); len(got) != 1 || got[0] != 200 {
		t.Fatalf("metric statuses = %v, want [200] (implicit)", got)
	}
}

func TestCaptureRecoversPanic(t *testing.T) {
	rep := &recordingReporter{}
	met := &recordingMetric{}
	h := Capture(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("kaboom")
	}), CaptureOptions{Component: "media", Reporter: rep, Metric: met})

	rec := httptest.NewRecorder()
	// Must not propagate the panic.
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/upload", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic should yield 500, got %d", rec.Code)
	}
	notices := rep.all()
	if len(notices) != 1 {
		t.Fatalf("got %d notices, want 1", len(notices))
	}
	n := notices[0]
	if n.Status != 500 || n.Component != "media" || n.Path != "/upload" {
		t.Fatalf("notice mismatch: %+v", n)
	}
	if n.Panic == nil {
		t.Fatal("recovered panic notice should carry the panic value")
	}
	if len(n.Stack) == 0 {
		t.Fatal("recovered panic notice should carry a stack")
	}
	if got := met.statuses(); len(got) != 1 || got[0] != 500 {
		t.Fatalf("metric statuses = %v, want [500]", got)
	}
}

func TestCaptureNilDefaultsAreSafe(t *testing.T) {
	// Zero-value options: no reporter, no metric. Must not panic and must
	// still recover handler panics.
	h := Capture(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("kaboom")
	}), CaptureOptions{})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("nil-options capture should still serve 500 on panic, got %d", rec.Code)
	}
}

func TestComposedWithMiddleware(t *testing.T) {
	// Middleware(Capture(...)) gives a recovered panic a request id to report.
	rep := &recordingReporter{}
	h := Middleware(Capture(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}), CaptureOptions{Component: "x", Reporter: rep}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	notices := rep.all()
	if len(notices) != 1 {
		t.Fatalf("got %d notices, want 1", len(notices))
	}
	if notices[0].RequestID == "" {
		t.Fatal("composed middleware should give the panic notice a request id")
	}
}
