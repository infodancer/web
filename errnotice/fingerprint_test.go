package errnotice

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/infodancer/web/obs"
)

func plain(component, method, path string, status int) obs.Notice {
	return obs.Notice{Component: component, Method: method, Path: path, Status: status}
}

func TestFingerprint_StableAndGrouping(t *testing.T) {
	tests := []struct {
		name string
		a    obs.Notice
		b    obs.Notice
		same bool
	}{
		{
			name: "identical notices fingerprint the same",
			a:    plain("blog", "GET", "/posts/123", 500),
			b:    plain("blog", "GET", "/posts/123", 500),
			same: true,
		},
		{
			name: "numeric id segments group",
			a:    plain("blog", "GET", "/posts/123", 500),
			b:    plain("blog", "GET", "/posts/456", 500),
			same: true,
		},
		{
			name: "uuid id segments group",
			a:    plain("blog", "GET", "/users/3f2504e0-4f89-41d3-9a0c-0305e82c3301", 500),
			b:    plain("blog", "GET", "/users/123e4567-e89b-12d3-a456-426614174000", 500),
			same: true,
		},
		{
			name: "long hex id segments group",
			a:    plain("media", "GET", "/media/a1b2c3d4e5f60718", 500),
			b:    plain("media", "GET", "/media/ffeeddccbbaa9988", 500),
			same: true,
		},
		{
			name: "different status splits",
			a:    plain("blog", "GET", "/posts/123", 500),
			b:    plain("blog", "GET", "/posts/123", 503),
			same: false,
		},
		{
			name: "different component splits",
			a:    plain("blog", "GET", "/posts/123", 500),
			b:    plain("profile", "GET", "/posts/123", 500),
			same: false,
		},
		{
			name: "different method splits",
			a:    plain("blog", "GET", "/posts/123", 500),
			b:    plain("blog", "POST", "/posts/123", 500),
			same: false,
		},
		{
			name: "different non-id route splits",
			a:    plain("blog", "GET", "/posts/123", 500),
			b:    plain("blog", "GET", "/comments/123", 500),
			same: false,
		},
		{
			name: "short word segment is not an id, so it splits",
			a:    plain("api", "GET", "/v1/health", 500),
			b:    plain("api", "GET", "/v2/health", 500),
			same: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fa := fingerprint(tt.a)
			fb := fingerprint(tt.b)
			if len(fa) != 64 {
				t.Fatalf("fingerprint is not a sha256 hex digest: %q (len %d)", fa, len(fa))
			}
			if (fa == fb) != tt.same {
				t.Errorf("fingerprint(a)==fingerprint(b) = %v, want %v\n a=%q -> %s\n b=%q -> %s",
					fa == fb, tt.same, tt.a.Path, fa, tt.b.Path, fb)
			}
		})
	}
}

func TestFingerprint_PanicVsPlainDiffer(t *testing.T) {
	base := plain("blog", "GET", "/posts/123", 500)

	panicked := base
	panicked.Panic = "boom"

	if fingerprint(base) == fingerprint(panicked) {
		t.Fatal("a recovered panic must not fingerprint the same as a plain 5xx")
	}
}

func TestFingerprint_DistinctPanicsDiffer(t *testing.T) {
	base := plain("blog", "GET", "/posts/123", 500)

	stringPanic := base
	stringPanic.Panic = "boom"

	errPanic := base
	errPanic.Panic = errString("boom") // same text, different type

	if fingerprint(stringPanic) == fingerprint(errPanic) {
		t.Error("panics of different Go types must not merge")
	}
}

func TestFingerprint_SamePanicTypeAndFrameGroup(t *testing.T) {
	stack := []byte("goroutine 1 [running]:\nmain.handler(...)\n\t/app/handler.go:42 +0x1d\n")
	a := plain("blog", "GET", "/posts/1", 500)
	a.Panic = "boom"
	a.Stack = stack
	b := plain("blog", "GET", "/posts/2", 500) // different id, same everything else
	b.Panic = "boom"
	b.Stack = stack

	if fingerprint(a) != fingerprint(b) {
		t.Error("same panic type and top frame on the same normalized route should group")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"/", "/"},
		{"/posts/123", "/posts/:id"},
		{"/posts/123/comments/456", "/posts/:id/comments/:id"},
		{"/users/3f2504e0-4f89-41d3-9a0c-0305e82c3301", "/users/:id"},
		{"/media/a1b2c3d4e5f60718", "/media/:id"}, // 16 hex chars
		{"/media/abc", "/media/abc"},              // short, not an id
		{"/v2/health", "/v2/health"},              // mixed alnum, not an id
		{"/posts/latest", "/posts/latest"},        // word, not an id
		{"/a1b2", "/a1b2"},                        // short hex, not an id
	}
	for _, tt := range tests {
		if got := normalizePath(tt.in); got != tt.want {
			t.Errorf("normalizePath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	t.Run("under cap is unchanged", func(t *testing.T) {
		s := "short stack"
		if got := truncate(s, maxStackLen); got != s {
			t.Errorf("truncate left a short string changed: %q", got)
		}
	})

	t.Run("oversized is capped and marked", func(t *testing.T) {
		big := strings.Repeat("x", maxStackLen*2)
		got := truncate(big, maxStackLen)
		if len(got) > maxStackLen {
			t.Errorf("truncated length %d exceeds cap %d", len(got), maxStackLen)
		}
		if !strings.HasSuffix(got, "...[truncated]") {
			t.Errorf("truncated output missing marker: %q", got[len(got)-20:])
		}
	})

	t.Run("panic cap is independent and smaller", func(t *testing.T) {
		big := strings.Repeat("y", maxPanicLen*2)
		got := truncate(big, maxPanicLen)
		if len(got) > maxPanicLen {
			t.Errorf("truncated panic length %d exceeds cap %d", len(got), maxPanicLen)
		}
	})

	t.Run("does not split a multibyte rune", func(t *testing.T) {
		// Fill with a 3-byte rune so a naive byte cut would land mid-rune.
		s := strings.Repeat("世", 100) // 300 bytes
		got := truncate(s, 50)
		if !utf8.ValidString(got) {
			t.Errorf("truncate produced invalid UTF-8")
		}
	})
}
