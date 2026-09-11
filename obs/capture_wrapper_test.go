package obs

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fullWriter implements the optional interfaces a real ResponseWriter may
// carry, so a test can tell whether Capture's wrapping hides them.
type fullWriter struct {
	httptest.ResponseRecorder
	flushed  bool
	readFrom bool
}

func (w *fullWriter) Flush() { w.flushed = true }

func (w *fullWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, nil
}

func (w *fullWriter) ReadFrom(r io.Reader) (int64, error) {
	w.readFrom = true
	return io.Copy(io.Discard, r)
}

// TestCapturePreservesOptionalInterfaces pins the wrapping contract. A wrapper
// that embeds http.ResponseWriter and adds only WriteHeader and Write drops
// every optional interface the real writer had: the handler's
// w.(http.Flusher) check fails, so streaming responses never flush; a
// WebSocket upgrade cannot hijack; and a file copy silently leaves the
// sendfile path. None of these fail loudly, which is what makes them worth a
// test rather than a comment.
func TestCapturePreservesOptionalInterfaces(t *testing.T) {
	var sawFlusher, sawHijacker, sawReaderFrom, controllerFlushed bool

	h := Capture(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, sawFlusher = w.(http.Flusher)
		_, sawHijacker = w.(http.Hijacker)
		_, sawReaderFrom = w.(io.ReaderFrom)
		controllerFlushed = http.NewResponseController(w).Flush() == nil
	}), CaptureOptions{Component: "stream"})

	inner := &fullWriter{ResponseRecorder: *httptest.NewRecorder()}
	h.ServeHTTP(inner, httptest.NewRequest(http.MethodGet, "/events", nil))

	if !sawFlusher {
		t.Error("handler cannot see http.Flusher through Capture; SSE and MCP streaming stop flushing")
	}
	if !sawHijacker {
		t.Error("handler cannot see http.Hijacker through Capture; WebSocket upgrades break")
	}
	if !sawReaderFrom {
		t.Error("handler cannot see io.ReaderFrom through Capture; the sendfile path is lost")
	}
	if !controllerFlushed {
		t.Error("http.ResponseController cannot flush through Capture")
	}
	if !inner.flushed {
		t.Error("the flush never reached the underlying ResponseWriter")
	}
}

// TestCaptureSilentHandlerRecords200 covers the handler that returns having
// written nothing at all. net/http sends 200 in that case, so recording 0 puts
// a status no server ever sent onto the metric, and a dashboard counting
// non-2xx sees a response that did not happen.
func TestCaptureSilentHandlerRecords200(t *testing.T) {
	rep := &recordingReporter{}
	met := &recordingMetric{}

	h := Capture(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		// Writes nothing: no WriteHeader, no Write.
	}), CaptureOptions{Component: "blog", Reporter: rep, Metric: met})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/quiet", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("recorder status = %d, want 200; the test premise is wrong", rec.Code)
	}
	if got := met.statuses(); len(got) != 1 || got[0] != http.StatusOK {
		t.Errorf("metric statuses = %v, want [200]", got)
	}
	if n := rep.all(); len(n) != 0 {
		t.Errorf("a silent 200 must not be reported as a failure; got %+v", n)
	}
}

// TestCaptureReadFromReachesUnderlyingWriter checks the sendfile path end to
// end rather than by interface assertion alone: io.Copy into the wrapper must
// land on the writer's ReadFrom, and the response must still be recorded as
// 200 with a body.
func TestCaptureReadFromReachesUnderlyingWriter(t *testing.T) {
	met := &recordingMetric{}
	h := Capture(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Masked in a bare io.Reader: io.Copy prefers the source's WriteTo when
		// it has one, and strings.Reader does, which would bypass ReadFrom
		// entirely and make this test pass for the wrong reason.
		io.Copy(w, struct{ io.Reader }{strings.NewReader("file contents")})
	}), CaptureOptions{Metric: met})

	inner := &fullWriter{ResponseRecorder: *httptest.NewRecorder()}
	h.ServeHTTP(inner, httptest.NewRequest(http.MethodGet, "/static/file.txt", nil))

	if !inner.readFrom {
		t.Error("io.Copy did not reach the underlying ReadFrom; the copy fell back to the slow path")
	}
	if got := met.statuses(); len(got) != 1 || got[0] != http.StatusOK {
		t.Errorf("metric statuses = %v, want [200]", got)
	}
}
